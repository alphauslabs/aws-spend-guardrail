package guard

import (
	"context"
	"strings"
	"testing"
	"time"
)

func enforcing(accounts ...string) string {
	var scopes []string
	for _, a := range accounts {
		scopes = append(scopes, `{"account_id":"`+a+`","ceiling_usd":100,"mode":"enforce"}`)
	}
	return `{"payer_account_id":"123456789012","scopes":[` + strings.Join(scopes, ",") + `]}`
}

func (a *alerts) count(substr string) int {
	n := 0
	for _, x := range a.list {
		if strings.Contains(x, substr) {
			n++
		}
	}
	return n
}

const limitSubject = "budget action limit reached"

// A scope past AWS's action limit gets its budget and one alert, not one per
// run, and its action once there is room.
func TestActionLimitAlertsOnceAndRecovers(t *testing.T) {
	h := newHarness(t)
	h.budgets.actionCap = 2
	h.publish(enforcing("111122223333", "444455556666", "777788889999"))
	for i := 0; i < 3; i++ {
		h.pull()
		h.now = h.now.Add(5 * time.Minute)
	}
	if n := h.alerts.count(limitSubject); n != 1 {
		t.Fatalf("limit alerts = %d, want 1: %v", n, h.alerts.list)
	}
	if !h.alerts.has("Account 777788889999 has no budget action, so it can't freeze until it has one.") ||
		!h.alerts.has("limit of 100 budget actions") || !h.alerts.has("OU scopes") {
		t.Fatalf("alert wording: %v", h.alerts.list)
	}
	if h.alerts.has("reconcile incomplete") {
		t.Fatalf("the limit should not also raise the generic alert: %v", h.alerts.list)
	}
	b, ok := h.budgets.all["guardrail-account-777788889999"]
	if !ok || b.Spec.Enforce {
		t.Fatalf("budget should exist without its action: %+v %v", b, ok)
	}

	h.budgets.actionCap = 3 // room again
	h.pull()
	if !h.budgets.all["guardrail-account-777788889999"].Spec.Enforce {
		t.Fatal("the action should be created once there is room")
	}
	if r, _ := h.state.Reported(context.Background(), reportedActionLimit); len(r) != 0 {
		t.Fatalf("report not cleared: %v", r)
	}

	h.budgets.actionCap = 2 // a later refusal alerts again
	delete(h.budgets.all, "guardrail-account-777788889999")
	h.pull()
	if n := h.alerts.count(limitSubject); n != 2 {
		t.Fatalf("a new refusal should alert again: %d", n)
	}
}

// While a version that swaps enforcing scopes waits for approval, the stricter
// config holds both sets; the new scopes are reported, then created after the
// approved version removes the old ones.
func TestActionLimitDuringPendingSwap(t *testing.T) {
	h := newHarness(t)
	h.budgets.actionCap = 2
	h.publish(enforcing("111122223333", "444455556666"))
	h.pull()
	h.publish(enforcing("200000000001", "200000000002"))
	h.pull()
	if n := h.alerts.count(limitSubject); n != 1 || !h.alerts.has("account 200000000001, account 200000000002") {
		t.Fatalf("want one alert naming both new scopes: %v", h.alerts.list)
	}
	approveAndWait(h, "2", 31*time.Minute)
	h.pull() // applies v2: old budgets go after this run's puts
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	for _, a := range []string{"200000000001", "200000000002"} {
		if !h.budgets.all["guardrail-account-"+a].Spec.Enforce {
			t.Fatalf("%s should have its action after the swap: %+v", a, h.budgets.all)
		}
	}
	if _, ok := h.budgets.all["guardrail-account-111122223333"]; ok {
		t.Fatal("the removed scope's budget should be gone")
	}
	if n := h.alerts.count(limitSubject); n != 1 {
		t.Fatalf("no new limit alert expected: %d", n)
	}
}
