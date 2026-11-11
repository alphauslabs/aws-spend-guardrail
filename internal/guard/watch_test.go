package guard

import (
	"context"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

func watching(h *harness) map[string]time.Time {
	w, _ := h.state.Reported(context.Background(), reportedSpendWatch)
	return w
}

// Lowering a ceiling below what is already spent freezes on the same run,
// although the updated budget reports zero.
func TestLowerCeilingBelowSpendFreezesAtOnce(t *testing.T) {
	h := newHarness(t)
	h.publish(enforceDoc("100", "stay_frozen"))
	h.pull()
	h.spend.byAccount[tX.ID] = 5.24
	h.now = h.now.Add(5 * time.Minute)
	h.publish(enforceDoc("4", "stay_frozen")) // a tightening: applies at once
	h.pull()
	if len(h.budgets.freezes) != 1 || len(h.state.frozen) != 1 {
		t.Fatalf("not frozen: freezes %v events %v", h.budgets.freezes, h.events.kinds())
	}
	_, d := lastEvent(h, schema.KindFreeze)
	if d["how"] != HowGuardAttach || d["spend_source"] != SpendSourceCostExplorer || d["actual_usd"] != 5.24 {
		t.Fatalf("freeze detail %v", d)
	}
	if alertCount(h, "is frozen") != 1 {
		t.Fatalf("alerts %v", h.alerts.list)
	}
	// Budgets catches up later and fires its action: not a second freeze.
	h.budgets.setActual(bX, 5.24)
	if err := h.g.OnThreshold(context.Background(), bX); err != nil {
		t.Fatal(err)
	}
	if h.events.count(schema.KindFreeze) != 1 || alertCount(h, "is frozen") != 1 {
		t.Fatalf("frozen twice: %v", h.events.kinds())
	}
}

func TestSpendCrossingLaterFreezesOnTheHourlyCheck(t *testing.T) {
	h := newHarness(t)
	h.publish(enforceDoc("100", "stay_frozen")) // threshold 90
	h.pull()
	if h.spend.calls != 1 {
		t.Fatalf("first check on the same run: %d calls", h.spend.calls)
	}
	h.spend.byAccount[tX.ID] = 95
	for i := 0; i < 11; i++ { // 55 minutes of pulls
		h.now = h.now.Add(5 * time.Minute)
		h.pull()
	}
	if h.spend.calls != 1 || len(h.budgets.freezes) != 0 {
		t.Fatalf("checked more than hourly: %d calls, freezes %v", h.spend.calls, h.budgets.freezes)
	}
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	if h.spend.calls != 2 || len(h.budgets.freezes) != 1 {
		t.Fatalf("hourly check: %d calls, freezes %v", h.spend.calls, h.budgets.freezes)
	}
}

func TestSpendWatchEnds(t *testing.T) {
	start := func(t *testing.T) *harness {
		h := newHarness(t)
		h.publish(enforceDoc("100", "stay_frozen"))
		h.pull()
		if len(watching(h)) != 1 {
			t.Fatal("no watch started")
		}
		return h
	}
	t.Run("budgets reports spend", func(t *testing.T) {
		h := start(t)
		h.budgets.setActual(bX, 10)
		h.now = h.now.Add(time.Hour)
		h.pull()
		if len(watching(h)) != 0 || h.spend.calls != 1 {
			t.Fatalf("watch %v, %d calls", watching(h), h.spend.calls)
		}
	})
	t.Run("after 48 hours", func(t *testing.T) {
		h := start(t)
		h.now = h.now.Add(SpendWatchFor)
		h.pull()
		if len(watching(h)) != 0 {
			t.Fatal("still watching")
		}
	})
	t.Run("at a new period", func(t *testing.T) {
		h := start(t)
		h.now = time.Date(2026, 12, 1, 0, 5, 0, 0, time.UTC)
		h.pull()
		if len(watching(h)) != 0 {
			t.Fatal("still watching")
		}
	})
	t.Run("an unchanged budget starts no watch", func(t *testing.T) {
		h := start(t)
		h.budgets.setActual(bX, 10)
		h.now = h.now.Add(time.Hour)
		h.pull()
		h.now = h.now.Add(time.Hour)
		h.pull()
		if len(watching(h)) != 0 || h.spend.calls != 1 {
			t.Fatalf("watch %v, %d calls", watching(h), h.spend.calls)
		}
	})
}

func TestFrozenScopeIsNotFrozenAgain(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.publish(enforceDoc("51000", "stay_frozen")) // a raise, still too low
	h.pull()
	approveAndWait(h, "2", time.Hour)
	h.pull()
	h.now = h.now.Add(2 * time.Hour)
	h.pull()
	if len(h.budgets.freezes) != 0 || h.events.count(schema.KindFreeze) != 1 {
		t.Fatalf("freezes %v events %v", h.budgets.freezes, h.events.kinds())
	}
}

func TestDryRunWatchRecordsWouldFreezeOnce(t *testing.T) {
	h := newHarness(t)
	h.spend.byAccount[tX.ID] = 60000
	h.publish(doc("50000", "50000")) // dry-run
	h.pull()
	for i := 0; i < 3; i++ {
		h.now = h.now.Add(time.Hour)
		h.pull()
	}
	if len(h.budgets.freezes) != 0 || len(h.state.frozen) != 0 {
		t.Fatal("a dry-run scope was frozen")
	}
	if h.events.count(schema.KindWouldFreeze) != 1 {
		t.Fatalf("events %v", h.events.kinds())
	}
	_, d := lastEvent(h, schema.KindWouldFreeze)
	if d["spend_source"] != SpendSourceCostExplorer {
		t.Fatalf("detail %v", d)
	}
}
