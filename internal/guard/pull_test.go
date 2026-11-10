package guard

import (
	"context"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
	approvalv1 "github.com/alphauslabs/jennah-sdk-go/jennah/approval/v1"
)

const (
	bX = "guardrail-account-111122223333"
	bY = "guardrail-account-444455556666"
)

func doc(xCeiling, yCeiling string) string {
	return `{"payer_account_id":"123456789012","scopes":[
		{"account_id":"111122223333","ceiling_usd":` + xCeiling + `},
		{"account_id":"444455556666","ceiling_usd":` + yCeiling + `}]}`
}

func TestNoConfigNothingHappens(t *testing.T) {
	h := newHarness(t)
	h.pull()
	if h.budgets.puts != 0 || len(h.events.list) != 0 {
		t.Fatal("nothing should happen without a config")
	}
}

func TestFirstVersionAppliesAtOnce(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	if h.st().AppliedVersion != 1 || h.ceiling(bX) != 50000 || h.events.count(schema.KindConfigApplied) != 1 {
		t.Fatalf("state %+v events %v", h.st(), h.events.kinds())
	}
	if h.jennah.CreatedApprovals != 0 {
		t.Fatal("no approval for a first config")
	}
}

func TestTighteningAppliesOnNextPull(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("40000", "50000"))
	h.pull()
	if h.ceiling(bX) != 40000 || h.st().AppliedVersion != 2 || h.jennah.CreatedApprovals != 0 {
		t.Fatalf("ceiling %v state %+v", h.ceiling(bX), h.st())
	}
}

func TestInvalidVersionKeepsLastValid(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(`{"payer_account_id":"123456789012","scopes":[{"account_id":"123456789012","ceiling_usd":1}]}`)
	h.pull()
	h.pull()
	if h.st().AppliedVersion != 1 || h.ceiling(bX) != 50000 {
		t.Fatalf("invalid version changed config: %+v", h.st())
	}
	if n := h.events.count(schema.KindConfigRejected); n != 1 {
		t.Fatalf("want one config_rejected, got %d", n)
	}
	if !h.alerts.has("version 2 is invalid") {
		t.Fatalf("no local alert: %v", h.alerts.list)
	}
}

func TestWrongPayerIsRejected(t *testing.T) {
	h := newHarness(t)
	h.publish(`{"payer_account_id":"999999999999","scopes":[]}`)
	h.pull()
	if h.st().AppliedVersion != 0 || h.events.count(schema.KindConfigRejected) != 1 {
		t.Fatalf("state %+v", h.st())
	}
}

func TestJennahUnreachableKeepsReconcilingLastConfig(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	delete(h.budgets.all, bX) // someone deleted the budget
	h.jennah.FailReads = true
	h.pull()
	if _, ok := h.budgets.all[bX]; !ok {
		t.Fatal("the guard should restore budgets from the last valid config while Jennah is down")
	}
	if !h.alerts.has("cannot read config from Jennah") {
		t.Fatalf("no alert: %v", h.alerts.list)
	}
}

func TestRestartReconcilesFromStoredState(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.jennah.FailReads = true
	delete(h.budgets.all, bY)
	h.g = h.newGuard() // a fresh process with the same state store
	h.pull()
	if _, ok := h.budgets.all[bY]; !ok {
		t.Fatal("restart should reconcile from stored config")
	}
}

func TestMixedVersionTightensNowAndRaisesOneApproval(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("40000", "60000"))
	h.pull()
	h.pull()
	if h.ceiling(bX) != 40000 {
		t.Fatalf("X should tighten at once, got %v", h.ceiling(bX))
	}
	if h.ceiling(bY) != 50000 {
		t.Fatalf("Y should wait for approval, got %v", h.ceiling(bY))
	}
	if h.jennah.CreatedApprovals != 1 {
		t.Fatalf("want one approval, got %d", h.jennah.CreatedApprovals)
	}
	a := h.jennah.Approval("guardrail:123456789012:msp_a:v2")
	if a == nil || a.GetPayload().GetFields()["config_version"].GetNumberValue() != 2 {
		t.Fatalf("approval payload: %v", a)
	}
	if h.events.count(schema.KindConfigPartial) != 1 || h.events.count(schema.KindLooseningRaised) != 1 {
		t.Fatalf("events: %v", h.events.kinds())
	}
}

func approveAndWait(h *harness, version string, wait time.Duration) {
	a := h.jennah.Approval("guardrail:123456789012:msp_a:v" + version)
	h.jennah.Decide(a.GetApprovalId(), approvalv1.ApprovalStatus_APPROVAL_STATUS_APPROVED)
	h.now = h.now.Add(wait)
}

func TestApprovedLooseningWaitsForTheDelay(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()

	approveAndWait(h, "2", 10*time.Minute)
	h.pull()
	if h.ceiling(bY) != 50000 {
		t.Fatal("applied before the delay passed")
	}
	h.now = h.now.Add(20 * time.Minute) // 30 minutes after first seen
	h.pull()
	if h.ceiling(bY) != 60000 || h.st().AppliedVersion != 2 || h.st().Pending != nil {
		t.Fatalf("not applied after approval and delay: %+v", h.st())
	}
	if h.events.count(schema.KindLooseningOK) != 1 || h.events.count(schema.KindConfigApplied) != 2 {
		t.Fatalf("events: %v", h.events.kinds())
	}
}

func TestRejectedOrExpiredKeepsStricter(t *testing.T) {
	for _, tc := range []struct {
		status approvalv1.ApprovalStatus
		kind   string
	}{
		{approvalv1.ApprovalStatus_APPROVAL_STATUS_REJECTED, schema.KindLooseningDenied},
		{approvalv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED, schema.KindLooseningExpired},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			h := newHarness(t)
			h.publish(doc("50000", "50000"))
			h.pull()
			h.publish(doc("50000", "60000"))
			h.pull()
			a := h.jennah.Approval("guardrail:123456789012:msp_a:v2")
			h.jennah.Decide(a.GetApprovalId(), tc.status)
			h.now = h.now.Add(3 * time.Hour)
			h.pull()
			h.pull()
			if h.ceiling(bY) != 50000 || h.st().AppliedVersion != 1 {
				t.Fatalf("loosening applied: %+v", h.st())
			}
			if h.events.count(tc.kind) != 1 || h.jennah.CreatedApprovals != 1 {
				t.Fatalf("events %v approvals %d", h.events.kinds(), h.jennah.CreatedApprovals)
			}
		})
	}
}

func TestNewerVersionSupersedesPendingApproval(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()
	h.publish(doc("45000", "50000")) // v3, pure tightening relative to what is in force
	h.pull()
	a := h.jennah.Approval("guardrail:123456789012:msp_a:v2")
	if a.GetStatus() != approvalv1.ApprovalStatus_APPROVAL_STATUS_CANCELLED {
		t.Fatalf("v2 approval should be cancelled, is %v", a.GetStatus())
	}
	if h.st().AppliedVersion != 3 || h.ceiling(bX) != 45000 || h.ceiling(bY) != 50000 {
		t.Fatalf("state %+v", h.st())
	}
}

func TestRemovedScopeNeedsApproval(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(`{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":50000}]}`)
	h.pull()
	if _, ok := h.budgets.all[bY]; !ok {
		t.Fatal("a removed scope must stay until approved")
	}
	approveAndWait(h, "2", time.Hour)
	h.pull()
	if _, ok := h.budgets.all[bY]; ok {
		t.Fatal("approved removal should delete the budget")
	}
}

func TestPullErrorDoesNotLoseState(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.jennah.FailReads = true
	_ = h.g.Pull(context.Background())
	if h.st().AppliedVersion != 1 {
		t.Fatal("state lost")
	}
}
