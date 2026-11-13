package guard

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

var tX = config.Target{Kind: "account", ID: "111122223333"}

func enforceDoc(ceiling, monthEnd string) string {
	return `{"payer_account_id":"123456789012","defaults":{"mode":"enforce","threshold_pct":90,"month_end":"` + monthEnd + `"},
		"scopes":[{"account_id":"111122223333","ceiling_usd":` + ceiling + `}]}`
}

func TestReconcileTwiceIsNoOp(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	puts := h.budgets.puts
	h.pull()
	if h.budgets.puts != puts {
		t.Fatalf("second reconcile changed budgets: %d -> %d", puts, h.budgets.puts)
	}
}

func TestCustomerBudgetIsLeftAlone(t *testing.T) {
	h := newHarness(t)
	h.budgets.all["team-x-monthly"] = BudgetStatus{Spec: BudgetSpec{Name: "team-x-monthly", LimitUSD: 1}}
	h.publish(doc("50000", "50000"))
	h.pull()
	if b, ok := h.budgets.all["team-x-monthly"]; !ok || b.Spec.LimitUSD != 1 {
		t.Fatal("customer budget changed")
	}
}

func TestBudgetCarriesScopeSettings(t *testing.T) {
	h := newHarness(t)
	h.publish(enforceDoc("50000", "stay_frozen"))
	h.pull()
	b := h.budgets.all[bX].Spec
	if b.LimitUSD != 50000 || b.ThresholdPct != 90 || !b.Enforce || b.PolicyID != "p-styleB" || b.Accounts[0] != tX.ID {
		t.Fatalf("spec %+v", b)
	}
}

func TestDryRunScopeGetsNoAction(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	if s := h.budgets.all[bX].Spec; s.Enforce || s.PolicyID != "" {
		t.Fatalf("a dry-run scope must have no action: %+v", s)
	}
}

func TestEnforceAddsActionAndDryRunRemovesIt(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(enforceDoc("50000", "stay_frozen")) // tightening: applies at once
	h.pull()
	if s := h.budgets.all[bX].Spec; !s.Enforce || s.PolicyID != "p-styleB" {
		t.Fatalf("enforce should give the scope its action: %+v", s)
	}
	h.publish(`{"payer_account_id":"123456789012","defaults":{"threshold_pct":90,"month_end":"stay_frozen"},
		"scopes":[{"account_id":"111122223333","ceiling_usd":50000}]}`) // back to dry-run: loosening
	h.pull()
	if !h.budgets.all[bX].Spec.Enforce {
		t.Fatal("going back to dry-run must wait for approval")
	}
	approveAndWait(h, "3", time.Hour)
	h.pull()
	if s := h.budgets.all[bX].Spec; s.Enforce || s.PolicyID != "" {
		t.Fatalf("approved dry-run should drop the action: %+v", s)
	}
}

func TestOUScopeFollowsMembership(t *testing.T) {
	h := newHarness(t)
	h.org.ous["ou-ab12-cd34ef56"] = []string{"222233334444"}
	h.publish(`{"payer_account_id":"123456789012","scopes":[{"ou_id":"ou-ab12-cd34ef56","ceiling_usd":1000}]}`)
	h.pull()
	name := "guardrail-ou-ou-ab12-cd34ef56"
	if got := h.budgets.all[name].Spec.Accounts; len(got) != 1 {
		t.Fatalf("accounts %v", got)
	}
	h.org.ous["ou-ab12-cd34ef56"] = []string{"222233334444", "555566667777"}
	if err := h.g.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.budgets.all[name].Spec.Accounts; len(got) != 2 {
		t.Fatalf("account joining the OU not picked up: %v", got)
	}
	if h.budgets.all[name].Spec.Target.Kind != "ou" {
		t.Fatal("the freeze target should be the OU")
	}
}

func TestThresholdInDryRunRecordsWouldFreeze(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.budgets.setActual(bX, 50001)
	if err := h.g.OnThreshold(context.Background(), bX); err != nil {
		t.Fatal(err)
	}
	if h.events.count(schema.KindWouldFreeze) != 1 || h.events.count(schema.KindFreeze) != 0 || len(h.state.frozen) != 0 {
		t.Fatalf("events %v frozen %v", h.events.kinds(), h.state.frozen)
	}
}

func TestThresholdInEnforceRecordsFreezeAndAlerts(t *testing.T) {
	h := newHarness(t)
	h.publish(enforceDoc("50000", "stay_frozen"))
	h.pull()
	h.budgets.setActual(bX, 46000)
	if err := h.g.OnThreshold(context.Background(), bX); err != nil {
		t.Fatal(err)
	}
	if h.events.count(schema.KindFreeze) != 1 || len(h.state.frozen) != 1 {
		t.Fatalf("events %v", h.events.kinds())
	}
	if !h.alerts.has("111122223333 is frozen") || !h.alerts.has("nothing running was stopped") {
		t.Fatalf("alert: %v", h.alerts.list)
	}
	var e jennah.Event // the freeze, followed by the heartbeat it changed
	for _, x := range h.events.list {
		if x.Kind == schema.KindFreeze {
			e = x
		}
	}
	if e.Scope != tX.String() || e.ConfigVersion != 1 {
		t.Fatalf("freeze event %+v", e)
	}
}

func freezeX(h *harness, actual float64) {
	h.t.Helper()
	h.publish(enforceDoc("50000", "stay_frozen"))
	h.pull()
	h.budgets.setActual(bX, actual)
	h.spend.byAccount[tX.ID] = actual // Cost Explorer agrees
	if err := h.g.OnThreshold(context.Background(), bX); err != nil {
		h.t.Fatal(err)
	}
	h.org.attached["p-styleB"] = []config.Target{tX}
}

func TestApprovedRaiseLiftsFreeze(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.publish(enforceDoc("60000", "stay_frozen")) // threshold 54000 > 46000
	h.pull()
	if len(h.budgets.releases) != 0 {
		t.Fatal("lifted before approval")
	}
	approveAndWait(h, "2", time.Hour)
	h.pull()
	if len(h.budgets.releases) != 1 || len(h.state.frozen) != 0 || h.events.count(schema.KindLift) != 1 {
		t.Fatalf("releases %v events %v", h.budgets.releases, h.events.kinds())
	}
}

func TestRaiseStillTooLowKeepsFreeze(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.publish(enforceDoc("51000", "stay_frozen")) // threshold 45900 < 46000
	h.pull()
	approveAndWait(h, "2", time.Hour)
	h.pull()
	if len(h.budgets.releases) != 0 || len(h.state.frozen) != 1 {
		t.Fatal("freeze lifted although spend is still above the new threshold")
	}
}

func TestPeriodStartAutoLift(t *testing.T) {
	h := newHarness(t)
	h.publish(enforceDoc("50000", "auto_lift"))
	h.pull()
	h.budgets.setActual(bX, 46000)
	_ = h.g.OnThreshold(context.Background(), bX)
	h.org.attached["p-styleB"] = []config.Target{tX}
	h.now = time.Date(2026, 12, 1, 0, 30, 0, 0, time.UTC)
	if err := h.g.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.budgets.releases) != 1 || h.events.count(schema.KindLift) != 1 {
		t.Fatalf("auto_lift did not lift: %v", h.events.kinds())
	}
}

func TestPeriodStartStayFrozen(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.now = time.Date(2026, 12, 1, 0, 30, 0, 0, time.UTC)
	h.budgets.setActual(bX, 0) // Budgets resets actual spend at the new period
	if err := h.g.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.pull()
	if len(h.budgets.releases) != 0 || len(h.state.frozen) != 1 {
		t.Fatal("stay_frozen lifted at period start")
	}
}

func TestSamePeriodNoAutoLift(t *testing.T) {
	h := newHarness(t)
	h.publish(enforceDoc("50000", "auto_lift"))
	h.pull()
	_ = h.g.OnThreshold(context.Background(), bX)
	h.org.attached["p-styleB"] = []config.Target{tX}
	h.now = h.now.Add(24 * time.Hour)
	_ = h.g.Daily(context.Background())
	if len(h.budgets.releases) != 0 {
		t.Fatal("lifted within the same period")
	}
}

func TestHandDetachedFreezeIsDriftNotReattached(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.org.attached["p-styleB"] = nil // detached by hand
	if err := h.g.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = h.g.Daily(context.Background())
	if h.events.count(schema.KindDrift) != 1 {
		t.Fatalf("want one drift event, got %v", h.events.kinds())
	}
	if !h.alerts.has("does not re-attach") {
		t.Fatalf("alerts %v", h.alerts.list)
	}
	if len(h.budgets.releases) != 0 {
		t.Fatal("drift should not touch the policy")
	}
}

func TestUnrecordedAttachmentIsDrift(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.org.attached["p-styleA"] = []config.Target{{Kind: "account", ID: "999988887777"}}
	_ = h.g.Daily(context.Background())
	if h.events.count(schema.KindDrift) != 1 {
		t.Fatalf("events %v", h.events.kinds())
	}
}

func TestHeartbeatDaily(t *testing.T) {
	h := newHarness(t)
	_ = h.g.Daily(context.Background())
	_ = h.g.Daily(context.Background())
	if h.events.count(schema.KindHeartbeat) != 1 {
		t.Fatal("one heartbeat per day")
	}
	h.now = h.now.Add(24 * time.Hour)
	_ = h.g.Daily(context.Background())
	if h.events.count(schema.KindHeartbeat) != 2 {
		t.Fatal("next day's heartbeat missing")
	}
}

func TestPeriodStart(t *testing.T) {
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	cases := map[string]string{
		config.PeriodMonthly:   "2026-11-01",
		config.PeriodQuarterly: "2026-10-01",
		config.PeriodAnnually:  "2026-01-01",
	}
	for p, want := range cases {
		if got := PeriodStart(p, at).Format("2006-01-02"); got != want {
			t.Errorf("%s: got %s want %s", p, got, want)
		}
	}
}

func TestTeardownLiftsAndDeletesOnlyOwned(t *testing.T) {
	h := newHarness(t)
	h.budgets.all["team-x-monthly"] = BudgetStatus{Spec: BudgetSpec{Name: "team-x-monthly"}}
	freezeX(h, 46000)
	if err := h.g.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.budgets.releases) != 1 || len(h.state.frozen) != 0 {
		t.Fatalf("releases %v", h.budgets.releases)
	}
	if len(h.budgets.all) != 1 {
		t.Fatalf("budgets left: %v", h.budgets.all)
	}
}

func TestHeartbeatAndConfigAppliedReportCodeHash(t *testing.T) {
	h := newHarness(t)
	h.g.SetCodeHash(context.Background(), "q1w2e3=", nil)
	h.publish(doc("50000", "50000"))
	h.pull()
	_ = h.g.Daily(context.Background())
	for _, e := range h.events.list {
		if e.Kind != schema.KindHeartbeat && e.Kind != schema.KindConfigApplied {
			continue
		}
		var d map[string]any
		_ = json.Unmarshal(e.Detail, &d)
		if d["code_sha256"] != "q1w2e3=" || d["guard_version"] != "v0.0.0-test" {
			t.Fatalf("%s detail %v", e.Kind, d)
		}
	}
}

func TestUnknownCodeHashAlerts(t *testing.T) {
	h := newHarness(t)
	h.g.SetCodeHash(context.Background(), "", errors.New("AccessDenied"))
	if h.g.S.CodeSHA256 != "unknown" || !h.alerts.has("code hash unknown") {
		t.Fatalf("hash %q alerts %v", h.g.S.CodeSHA256, h.alerts.list)
	}
	_ = h.g.Daily(context.Background())
	var d map[string]any
	_ = json.Unmarshal(h.events.list[len(h.events.list)-1].Detail, &d)
	if d["code_sha256"] != "unknown" {
		t.Fatalf("heartbeat should say unknown: %v", d)
	}
}
