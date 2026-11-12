package guard

import (
	"context"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// actionFrozenX freezes X the way AWS Budgets does: its action fires and
// attaches the policy, then the guard is notified.
func actionFrozenX(t *testing.T) *harness {
	h := newHarness(t)
	h.budgets.org = h.org
	var slept time.Duration
	h.g.Sleep = func(d time.Duration) { slept += d; h.now = h.now.Add(d) }
	t.Cleanup(func() {
		if slept > CarryWait {
			t.Errorf("waited %s, more than %s", slept, CarryWait)
		}
	})
	h.publish(enforceDoc("50000", "stay_frozen")) // threshold 45000
	h.pull()
	h.spend.byAccount[tX.ID] = 46000
	h.budgets.setActual(bX, 46000)
	h.budgets.fire(bX)
	if err := h.g.OnThreshold(context.Background(), bX); err != nil {
		t.Fatal(err)
	}
	if !h.org.isAttached("p-styleB", tX) || len(h.state.frozen) != 1 {
		t.Fatal("setup: X not frozen")
	}
	return h
}

func TestTighteningKeepsAnActionFreeze(t *testing.T) {
	h := actionFrozenX(t)
	h.publish(enforceDoc("45000", "stay_frozen")) // a tightening: AWS resets the action
	h.pull()
	if !h.org.isAttached("p-styleB", tX) {
		t.Fatal("unfrozen by the budget update")
	}
	if h.events.count(schema.KindFreeze) != 1 || alertCount(h, "is frozen") != 1 || h.events.count(schema.KindFreezeCarried) != 1 {
		t.Fatalf("events %v alerts %v", h.events.kinds(), h.alerts.list)
	}
	e, d := lastEvent(h, schema.KindFreezeCarried)
	if e.ConfigVersion != 2 || d["frozen_since"] == nil {
		t.Fatalf("freeze_carried %+v %v", e, d)
	}
}

func TestRaiseStillTooLowKeepsAnActionFreeze(t *testing.T) {
	h := actionFrozenX(t)
	h.publish(enforceDoc("51000", "stay_frozen")) // threshold 45900, below 46000
	h.pull()
	approveAndWait(h, "2", time.Hour)
	h.pull()
	if !h.org.isAttached("p-styleB", tX) || len(h.budgets.releases) != 0 || h.events.count(schema.KindFreezeCarried) != 1 {
		t.Fatalf("attached %v releases %v events %v", h.org.isAttached("p-styleB", tX), h.budgets.releases, h.events.kinds())
	}
}

func TestRaiseAboveSpendLiftsAfterTheReset(t *testing.T) {
	h := actionFrozenX(t)
	h.publish(enforceDoc("60000", "stay_frozen")) // threshold 54000, above 46000
	h.pull()
	approveAndWait(h, "2", time.Hour)
	h.pull()
	if h.org.isAttached("p-styleB", tX) || h.events.count(schema.KindFreezeCarried) != 0 {
		t.Fatalf("re-attached after a lift: events %v", h.events.kinds())
	}
	_, d := lastEvent(h, schema.KindLift)
	if d["how"] != "already_detached" {
		t.Fatalf("lift detail %v", d)
	}
}

func TestSlowResetIsCarriedOnTheNextRun(t *testing.T) {
	h := actionFrozenX(t)
	h.budgets.resetReads = 1000 // longer than one run waits
	h.publish(enforceDoc("45000", "stay_frozen"))
	h.pull()
	if h.org.isAttached("p-styleB", tX) || h.events.count(schema.KindFreezeCarried) != 0 {
		t.Fatal("attached before the reset finished")
	}
	if r, _ := h.state.Reported(context.Background(), reportedCarry); len(r) != 1 {
		t.Fatalf("not left for the next run: %v", r)
	}
	h.budgets.resetting[bX] = 0 // the reset finishes
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	if !h.org.isAttached("p-styleB", tX) || h.events.count(schema.KindFreezeCarried) != 1 {
		t.Fatalf("not carried: events %v", h.events.kinds())
	}
	if r, _ := h.state.Reported(context.Background(), reportedCarry); len(r) != 0 {
		t.Fatalf("carry record left: %v", r)
	}
}

func TestGuardAttachedFreezeNeedsNoCarry(t *testing.T) {
	h := newHarness(t)
	h.budgets.org = h.org
	h.g.Sleep = func(time.Duration) { t.Fatal("waited for a reset that can't happen") }
	h.publish(enforceDoc("100", "stay_frozen"))
	h.pull()
	h.spend.byAccount[tX.ID] = 95
	h.publish(enforceDoc("50", "stay_frozen")) // the spend watch freezes X itself
	h.pull()
	if !h.org.isAttached("p-styleB", tX) {
		t.Fatal("setup: not frozen by the guard")
	}
	reads := h.budgets.statusReads
	h.publish(enforceDoc("40", "stay_frozen"))
	h.pull()
	if !h.org.isAttached("p-styleB", tX) || h.events.count(schema.KindFreezeCarried) != 0 || h.budgets.statusReads != reads {
		t.Fatalf("events %v, %d status reads", h.events.kinds(), h.budgets.statusReads-reads)
	}
}
