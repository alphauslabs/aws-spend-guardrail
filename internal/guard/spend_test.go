package guard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

func lastEvent(h *harness, kind string) (jennah.Event, map[string]any) {
	h.t.Helper()
	for i := len(h.events.list) - 1; i >= 0; i-- {
		if e := h.events.list[i]; e.Kind == kind {
			var d map[string]any
			_ = json.Unmarshal(e.Detail, &d)
			return e, d
		}
	}
	h.t.Fatalf("no %s event in %v", kind, h.events.kinds())
	return jennah.Event{}, nil
}

func alertCount(h *harness, substr string) int {
	n := 0
	for _, a := range h.alerts.list {
		if strings.Contains(a, substr) {
			n++
		}
	}
	return n
}

// A raise that is still too low must not lift on a later run, when updating
// the budget has made it report zero spend.
func TestRaiseStillTooLowKeepsFreezeWhenBudgetReadsZero(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.publish(enforceDoc("51000", "stay_frozen")) // threshold 45900 < 46000
	h.pull()
	approveAndWait(h, "2", time.Hour)
	h.pull()
	h.budgets.setActual(bX, 0) // AWS zeroed it on the update
	for i := 0; i < 3; i++ {
		h.now = h.now.Add(5 * time.Minute)
		h.pull()
	}
	if len(h.budgets.releases) != 0 || len(h.state.frozen) != 1 {
		t.Fatalf("lifted on a zeroed budget: releases %v", h.budgets.releases)
	}
}

func TestRaiseAboveSpendLiftsWithCostExplorerFigure(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.budgets.setActual(bX, 0)
	h.publish(enforceDoc("60000", "stay_frozen")) // threshold 54000
	h.pull()
	approveAndWait(h, "2", time.Hour)
	h.pull()
	if len(h.budgets.releases) != 1 {
		t.Fatalf("not lifted: %v", h.events.kinds())
	}
	_, d := lastEvent(h, schema.KindLift)
	if d["actual_usd"] != 46000.0 || d["spend_source"] != SpendSourceCostExplorer {
		t.Fatalf("lift detail %v", d)
	}
}

func TestCostExplorerErrorKeepsFreezeAndAlertsOnce(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.spend.byAccount[tX.ID] = 1000
	h.spend.err = errors.New("throttled")
	h.publish(enforceDoc("60000", "stay_frozen"))
	h.pull()
	approveAndWait(h, "2", time.Hour)
	for i := 0; i < 3; i++ {
		h.pull()
		h.now = h.now.Add(5 * time.Minute)
	}
	if len(h.budgets.releases) != 0 {
		t.Fatal("lifted without a spend reading")
	}
	if n := alertCount(h, "spend check failing"); n != 1 {
		t.Fatalf("want one alert, got %d: %v", n, h.alerts.list)
	}
	h.spend.err = nil
	h.pull()
	if len(h.budgets.releases) != 1 {
		t.Fatal("not lifted once Cost Explorer answered")
	}
	if r, _ := h.state.Reported(context.Background(), reportedSpendError); len(r) != 0 {
		t.Fatalf("failure not cleared: %v", r)
	}
}
