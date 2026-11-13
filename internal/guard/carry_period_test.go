package guard

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// periodReset is AWS resetting X's executed action at the start of a period, as
// it does on a budget update: the policy is detached, and the action reads as
// resetting for `reads` status reads before standby.
func periodReset(h *harness, reads int) {
	b := h.budgets.all[bX]
	b.ActionExecuted = false
	h.budgets.all[bX] = b
	h.org.detach(b.Spec.PolicyID, tX)
	if reads > 0 {
		h.budgets.resetting[bX] = reads
	}
}

func nextMonth(h *harness) {
	t := h.now.UTC()
	h.now = time.Date(t.Year(), t.Month()+1, 1, 0, 30, 0, 0, time.UTC)
}

func periodCarries(h *harness) int {
	n := 0
	for _, e := range h.events.list {
		var d map[string]any
		_ = json.Unmarshal(e.Detail, &d)
		if e.Kind == schema.KindFreezeCarried && d["reason"] == "period_reset" {
			n++
		}
	}
	return n
}

func TestPeriodResetOfAStayFrozenFreezeIsCarried(t *testing.T) {
	h := actionFrozenX(t)
	since := h.state.frozen[tX].Since
	nextMonth(h)
	periodReset(h, 0)
	h.pull()
	if !h.org.isAttached("p-styleB", tX) || periodCarries(h) != 1 {
		t.Fatalf("not carried into the new period: attached %v, events %v", h.org.isAttached("p-styleB", tX), h.events.kinds())
	}
	if f, ok := h.state.frozen[tX]; !ok || !f.Since.Equal(since) {
		t.Fatalf("the freeze record should keep its start: %+v", f)
	}
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	if periodCarries(h) != 1 {
		t.Fatal("a carried freeze is carried once")
	}
}

func TestPeriodResetStillRunningFinishesOnALaterRun(t *testing.T) {
	h := actionFrozenX(t)
	nextMonth(h)
	periodReset(h, 2)
	h.pull()
	if h.org.isAttached("p-styleB", tX) {
		t.Fatal("attached before AWS's reset finished")
	}
	for i := 0; i < 3 && !h.org.isAttached("p-styleB", tX); i++ {
		h.now = h.now.Add(5 * time.Minute)
		h.pull()
	}
	if !h.org.isAttached("p-styleB", tX) || h.events.count(schema.KindFreezeCarried) != 1 {
		t.Fatalf("the pending carry didn't finish: events %v", h.events.kinds())
	}
	if h.events.count(schema.KindDrift) != 0 {
		t.Fatal("a pending carry is not drift")
	}
}

func TestDailyOnTheFirstCarriesBeforeDrift(t *testing.T) {
	h := actionFrozenX(t)
	nextMonth(h)
	periodReset(h, 0)
	if err := h.g.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.org.isAttached("p-styleB", tX) || periodCarries(h) != 1 || h.events.count(schema.KindDrift) != 0 || len(h.state.frozen) != 1 {
		t.Fatalf("attached %v, events %v, frozen %v", h.org.isAttached("p-styleB", tX), h.events.kinds(), h.state.frozen)
	}
}

func TestADetachWithTheActionStillExecutedIsDriftInANewPeriod(t *testing.T) {
	h := actionFrozenX(t)
	nextMonth(h)
	h.org.detach("p-styleB", tX) // by hand: the action still reads as executed
	h.pull()
	if h.org.isAttached("p-styleB", tX) || periodCarries(h) != 0 {
		t.Fatal("a freeze a person detached was attached again")
	}
	if err := h.g.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.events.count(schema.KindDrift) != 1 || len(h.state.frozen) != 0 {
		t.Fatalf("want drift and the record cleared: events %v", h.events.kinds())
	}
}

func TestAutoLiftIsNotCarried(t *testing.T) {
	h := newHarness(t)
	h.budgets.org = h.org
	h.g.Sleep = func(d time.Duration) { h.now = h.now.Add(d) }
	h.publish(enforceDoc("50000", "auto_lift"))
	h.pull()
	h.spend.byAccount[tX.ID] = 46000
	h.budgets.setActual(bX, 46000)
	h.budgets.fire(bX)
	if err := h.g.OnThreshold(context.Background(), bX); err != nil {
		t.Fatal(err)
	}
	nextMonth(h)
	periodReset(h, 0)
	h.pull()
	if err := h.g.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.org.isAttached("p-styleB", tX) || periodCarries(h) != 0 {
		t.Fatalf("an auto_lift freeze was carried into the new period: %v", h.events.kinds())
	}
}

func TestANewPeriodWithThePolicyStillAttachedChangesNothing(t *testing.T) {
	h := actionFrozenX(t)
	nextMonth(h)
	reads := h.budgets.statusReads
	h.pull()
	if periodCarries(h) != 0 || len(h.budgets.freezes) != 0 {
		t.Fatalf("events %v freezes %v", h.events.kinds(), h.budgets.freezes)
	}
	if h.budgets.statusReads != reads {
		t.Errorf("read the action status %d times for a freeze still attached", h.budgets.statusReads-reads)
	}
}
