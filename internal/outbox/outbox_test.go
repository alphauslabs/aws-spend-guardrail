package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
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
