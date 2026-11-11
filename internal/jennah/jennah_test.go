package jennah_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	approvalv1 "github.com/alphauslabs/jennah-sdk-go/jennah/approval/v1"
)

const cfgDS = "guardrail.msp_a.123456789012.config"
const inboxDS = "guardrail.msp_a.123456789012.inbox"

func TestVersionsReadAndWrite(t *testing.T) {
	ctx := context.Background()
	_, c := jennahtest.Start(t)

	v, err := c.LatestVersion(ctx, cfgDS)
	if err != nil || v != nil {
		t.Fatalf("empty dataset: %v %v", v, err)
	}
	for n := int64(1); n <= 3; n++ {
		doc := json.RawMessage(`{"n":` + string(rune('0'+n)) + `}`)
		if err := c.InsertVersion(ctx, cfgDS, jennah.Version{Number: n, Document: doc, PublishedAt: time.Now()}, "k"+string(rune('0'+n))); err != nil {
			t.Fatal(err)
		}
	}
	v, err = c.LatestVersion(ctx, cfgDS)
	if err != nil || v.Number != 3 || string(v.Document) != `{"n":3}` {
		t.Fatalf("latest: %+v %v", v, err)
	}
	v, err = c.GetVersion(ctx, cfgDS, 2)
	if err != nil || v.Number != 2 {
		t.Fatalf("get 2: %+v %v", v, err)
	}
	v, err = c.GetVersion(ctx, cfgDS, 9)
	if err != nil || v != nil {
		t.Fatalf("get missing: %+v %v", v, err)
	}
}

func TestConcurrentPublishersConflict(t *testing.T) {
	ctx := context.Background()
	_, c := jennahtest.Start(t)
	v := jennah.Version{Number: 8, Document: json.RawMessage(`{}`), PublishedAt: time.Now()}
	if err := c.InsertVersion(ctx, cfgDS, v, "a"); err != nil {
		t.Fatal(err)
	}
	err := c.InsertVersion(ctx, cfgDS, v, "b")
	if !errors.Is(err, jennah.ErrVersionExists) {
		t.Fatalf("second publisher: want ErrVersionExists, got %v", err)
	}
}

func TestRetriedPublishWritesOneVersion(t *testing.T) {
	ctx := context.Background()
	f, c := jennahtest.Start(t)
	f.LoseCommitResponse = 1
	v := jennah.Version{Number: 1, Document: json.RawMessage(`{}`), PublishedAt: time.Now()}
	if err := c.InsertVersion(ctx, cfgDS, v, "same"); err == nil {
		t.Fatal("expected the lost response to surface as an error")
	}
	if err := c.InsertVersion(ctx, cfgDS, v, "same"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := len(f.Rows(cfgDS, schema.ConfigTable)); n != 1 {
		t.Fatalf("want 1 version, got %d", n)
	}
}

func TestEventsAreWrittenOnce(t *testing.T) {
	ctx := context.Background()
	f, c := jennahtest.Start(t)
	f.LoseCommitResponse = 1
	e := jennah.Event{ID: "freeze:acct:2026-11", OccurredAt: time.Now(), Kind: schema.KindFreeze, GuardVersion: "v0"}
	if err := c.CommitEvent(ctx, inboxDS, e); err == nil {
		t.Fatal("expected the lost response to surface")
	}
	if err := c.CommitEvent(ctx, inboxDS, e); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := c.CommitEvent(ctx, inboxDS, e); err != nil {
		t.Fatalf("third write: %v", err)
	}
	if n := len(f.Rows(inboxDS, schema.EventsTable)); n != 1 {
		t.Fatalf("want 1 event, got %d", n)
	}
}

func TestLatestEventByKind(t *testing.T) {
	ctx := context.Background()
	_, c := jennahtest.Start(t)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i, k := range []string{schema.KindConfigApplied, schema.KindHeartbeat, schema.KindConfigApplied} {
		e := jennah.Event{ID: string(rune('a' + i)), OccurredAt: base.Add(time.Duration(i) * time.Hour), Kind: k, ConfigVersion: int64(i + 1), GuardVersion: "v0"}
		if err := c.CommitEvent(ctx, inboxDS, e); err != nil {
			t.Fatal(err)
		}
	}
	e, err := c.LatestEvent(ctx, inboxDS, schema.KindConfigApplied)
	if err != nil || e == nil || e.ConfigVersion != 3 {
		t.Fatalf("got %+v %v", e, err)
	}
}

func TestApprovalsAreIdempotentByRequestKey(t *testing.T) {
	ctx := context.Background()
	f, c := jennahtest.Start(t)
	req := jennah.ApprovalRequest{
		RequestKey: "guardrail:123456789012:v6",
		Title:      "Loosen guard config v6",
		Summary:    "raise",
		Payload:    map[string]any{"payer_account_id": "123456789012", "config_version": 6},
		Approvers:  []string{"a@example.com"},
		Quorum:     jennah.QuorumAnyOne,
		TTL:        time.Hour,
	}
	a1, err := c.RaiseApproval(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := c.RaiseApproval(ctx, req)
	if err != nil || a2.ID != a1.ID || f.CreatedApprovals != 1 {
		t.Fatalf("replay created another: %+v %+v %d %v", a1, a2, f.CreatedApprovals, err)
	}
	f.Decide(a1.ID, approvalv1.ApprovalStatus_APPROVAL_STATUS_APPROVED)
	got, err := c.GetApproval(ctx, a1.ID)
	if err != nil || got.Status != jennah.ApprovalApproved {
		t.Fatalf("status: %+v %v", got, err)
	}
	if err := c.CancelApproval(ctx, a1.ID); err != nil {
		t.Fatalf("cancelling a decided approval should be a no-op: %v", err)
	}
}

func TestApprovalsCarryAQuorumRuleJennahAccepts(t *testing.T) {
	ctx := context.Background()
	f, c := jennahtest.Start(t)
	approvers := []string{"a@example.com", "b@example.com", "c@example.com"}
	for _, tc := range []struct {
		q        jennah.Quorum
		want     approvalv1.Quorum
		required int32
	}{
		{jennah.QuorumAnyOne, approvalv1.Quorum_QUORUM_ANY_ONE, 1},
		{jennah.QuorumAll, approvalv1.Quorum_QUORUM_ALL, 3},
	} {
		key := "guardrail:123456789012:" + string(tc.q)
		_, err := c.RaiseApproval(ctx, jennah.ApprovalRequest{RequestKey: key, Title: "t", Approvers: approvers, Quorum: tc.q, TTL: time.Hour})
		if err != nil {
			t.Fatalf("%s: %v", tc.q, err)
		}
		a := f.Approval(key)
		if a.GetQuorum() != tc.want || a.GetRequiredApprovals() != tc.required {
			t.Fatalf("%s: got %v with %d required", tc.q, a.GetQuorum(), a.GetRequiredApprovals())
		}
	}
	// No rule is refused before anything is sent.
	if _, err := c.RaiseApproval(ctx, jennah.ApprovalRequest{RequestKey: "k", Title: "t", Approvers: approvers, TTL: time.Hour}); err == nil {
		t.Fatal("an approval without a quorum rule was raised")
	}
	if f.CreatedApprovals != 2 {
		t.Fatalf("created %d approvals, want 2", f.CreatedApprovals)
	}
}
