package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ds = "guardrail.msp_a.123456789012.inbox"

type fakeQueue struct{ bodies []string }

func (q *fakeQueue) SendMessage(_ context.Context, in *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	q.bodies = append(q.bodies, aws.ToString(in.MessageBody))
	return &sqs.SendMessageOutput{}, nil
}

func (q *fakeQueue) batch() events.SQSEvent {
	var ev events.SQSEvent
	for i, b := range q.bodies {
		ev.Records = append(ev.Records, events.SQSMessage{MessageId: string(rune('a' + i)), Body: b})
	}
	return ev
}

func TestTimedOutCommitRetriedOnce(t *testing.T) {
	ctx := context.Background()
	f, c := jennahtest.Start(t)
	q := &fakeQueue{}
	p := &Producer{SQS: q, QueueURL: "q"}
	if err := p.Emit(ctx, jennah.Event{ID: "freeze:account:111122223333:2026-11-01", OccurredAt: time.Now(), Kind: schema.KindFreeze, GuardVersion: "v0"}); err != nil {
		t.Fatal(err)
	}
	cons := &Consumer{Jennah: c, DatasetID: ds}

	f.LoseCommitResponse = 1
	resp, _ := cons.Handle(ctx, q.batch())
	if len(resp.BatchItemFailures) != 1 {
		t.Fatal("a lost response should be retried")
	}
	resp, _ = cons.Handle(ctx, q.batch()) // SQS redelivers
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("retry failed: %+v", resp)
	}
	if n := len(f.Rows(ds, schema.EventsTable)); n != 1 {
		t.Fatalf("want 1 event in the inbox, got %d", n)
	}
}

func TestEventsQueuedDuringOutageKeepTheirTime(t *testing.T) {
	ctx := context.Background()
	f, c := jennahtest.Start(t)
	q := &fakeQueue{}
	p := &Producer{SQS: q, QueueURL: "q"}
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	_ = p.Emit(ctx, jennah.Event{ID: "freeze:x", OccurredAt: at, Kind: schema.KindFreeze, GuardVersion: "v0"})
	cons := &Consumer{Jennah: c, DatasetID: ds}

	f.FailCommits = true
	for i := 0; i < 3; i++ {
		resp, _ := cons.Handle(ctx, q.batch())
		if len(resp.BatchItemFailures) != 1 {
			t.Fatal("expected failure while Jennah is down")
		}
	}
	f.FailCommits = false
	resp, _ := cons.Handle(ctx, q.batch())
	if len(resp.BatchItemFailures) != 0 {
		t.Fatal("delivery after outage failed")
	}
	rows := f.Rows(ds, schema.EventsTable)
	if len(rows) != 1 || !rows[0].GetColumns()[schema.ColOccurredAt].GetTimestampValue().AsTime().Equal(at) {
		t.Fatalf("event time not preserved: %v", rows)
	}
}

func TestGarbageMessageGoesToDLQ(t *testing.T) {
	_, c := jennahtest.Start(t)
	cons := &Consumer{Jennah: c, DatasetID: ds}
	resp, _ := cons.Handle(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "m", Body: "{not json"}}})
	if len(resp.BatchItemFailures) != 1 {
		t.Fatal("garbage should fail")
	}
}

// stubJennah fails every write with err and answers lookups with found or
// findErr, counting lookups.
type stubJennah struct {
	err     error
	found   bool
	findErr error
	finds   int
}

func (s *stubJennah) CommitEvent(context.Context, string, jennah.Event) error { return s.err }
func (s *stubJennah) HasEvent(context.Context, string, string) (bool, error) {
	s.finds++
	return s.found, s.findErr
}

type logs struct{ lines []string }

func (l *logs) logf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func queued(t *testing.T, detail string) (*fakeQueue, jennah.Event) {
	t.Helper()
	q := &fakeQueue{}
	e := jennah.Event{ID: "heartbeat:2026-11-18", OccurredAt: time.Now(), Kind: schema.KindHeartbeat, GuardVersion: "v0", Detail: json.RawMessage(detail)}
	if err := (&Producer{SQS: q, QueueURL: "q"}).Emit(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return q, e
}

// A second event under an id the inbox holds, with other content, is refused
// for good; it counts as delivered and the first stands (fix-outbox-delivery).
func TestRepeatedIDCountsAsDelivered(t *testing.T) {
	ctx := context.Background()
	f, c := jennahtest.Start(t)
	l := &logs{}
	cons := &Consumer{Jennah: c, Inbox: c, DatasetID: ds, Logf: l.logf}
	first, _ := queued(t, `{"secret_note":"first"}`)
	if resp, _ := cons.Handle(ctx, first.batch()); len(resp.BatchItemFailures) != 0 {
		t.Fatalf("first: %+v", resp)
	}
	second, _ := queued(t, `{"secret_note":"second"}`)
	resp, _ := cons.Handle(ctx, second.batch())
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("a repeated id must leave the queue: %+v, logs %v", resp, l.lines)
	}
	rows := f.Rows(ds, schema.EventsTable)
	if len(rows) != 1 || !strings.Contains(rows[0].GetColumns()[schema.ColDetail].GetJsonValue(), "first") {
		t.Fatalf("the first event must stand: %v", rows)
	}
	if len(l.lines) != 1 || !strings.Contains(l.lines[0], "heartbeat:2026-11-18 (heartbeat) already in the inbox") || strings.Contains(l.lines[0], "secret_note") {
		t.Fatalf("logs %v", l.lines)
	}
}

func TestFailedWritesRetriedAndLogged(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		stub      *stubJennah
		wantFinds int
		wantInLog string
	}{
		{"invalid, not in the inbox", &stubJennah{err: status.Error(codes.InvalidArgument, "bad row")}, 1, "bad row"},
		{"invalid, lookup fails", &stubJennah{err: status.Error(codes.InvalidArgument, "bad row"), findErr: errors.New("reader down")}, 1, "reader down"},
		{"unavailable: no lookup", &stubJennah{err: status.Error(codes.Unavailable, "jennah down"), found: true}, 0, "jennah down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &logs{}
			cons := &Consumer{Jennah: tc.stub, Inbox: tc.stub, DatasetID: ds, Logf: l.logf}
			q, e := queued(t, `{"secret_note":"x"}`)
			resp, _ := cons.Handle(ctx, q.batch())
			if len(resp.BatchItemFailures) != 1 || tc.stub.finds != tc.wantFinds {
				t.Fatalf("failures %+v, lookups %d", resp, tc.stub.finds)
			}
			if len(l.lines) != 1 || !strings.Contains(l.lines[0], "write "+e.ID+" (heartbeat) failed") || !strings.Contains(l.lines[0], tc.wantInLog) || strings.Contains(l.lines[0], "secret_note") {
				t.Fatalf("logs %v", l.lines)
			}
		})
	}
}
