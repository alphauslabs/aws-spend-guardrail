package guard

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

func TestFrozenLiftedAndFrozenAgainInOnePeriod(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	h.publish(enforceDoc("60000", "stay_frozen")) // threshold 54000
	h.pull()
	approveAndWait(h, "2", time.Hour)
	h.pull()
	if h.events.count(schema.KindLift) != 1 {
		t.Fatalf("not lifted: %v", h.events.kinds())
	}
	h.now = h.now.Add(time.Hour)
	h.budgets.setActual(bX, 55000)
	if err := h.g.OnThreshold(context.Background(), bX); err != nil {
		t.Fatal(err)
	}
	if n := h.events.count(schema.KindFreeze); n != 2 {
		t.Fatalf("want two freeze events, got %d: %v", n, h.events.kinds())
	}
	last := h.events.list[len(h.events.list)-1]
	if last.Kind != schema.KindFreeze || alertCount(h, "is frozen") != 2 {
		t.Fatalf("last %s, alerts %v", last.Kind, h.alerts.list)
	}
	want := fmt.Sprintf("freeze:%s:2026-11-01:%d", tX, h.now.Unix())
	if last.ID != want {
		t.Fatalf("id %s, want %s", last.ID, want)
	}
}

func TestRepeatedNotificationWritesALostFreezeOnce(t *testing.T) {
	h := newHarness(t)
	h.publish(enforceDoc("50000", "stay_frozen"))
	h.pull()
	// The freeze was recorded, but its event never reached the outbox.
	since := h.now
	if err := h.state.MarkFrozen(context.Background(), Frozen{
		Target: tX, Since: since, PeriodStart: PeriodStart(config.PeriodMonthly, since), PolicyID: "p-styleB", ThresholdUSD: 45000,
	}); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(20 * time.Minute)
	h.budgets.setActual(bX, 46000)
	for i := 0; i < 2; i++ {
		if err := h.g.OnThreshold(context.Background(), bX); err != nil {
			t.Fatal(err)
		}
	}
	if h.events.count(schema.KindFreeze) != 1 || alertCount(h, "is frozen") != 0 {
		t.Fatalf("events %v alerts %v", h.events.kinds(), h.alerts.list)
	}
	e, _ := lastEvent(h, schema.KindFreeze)
	if !e.OccurredAt.Equal(since) || e.ID != fmt.Sprintf("freeze:%s:2026-11-01:%d", tX, since.Unix()) {
		t.Fatalf("event %s at %s, want the original freeze time %s", e.ID, e.OccurredAt, since)
	}
}

func TestAlertHelpers(t *testing.T) {
	ou := config.Target{Kind: "ou", ID: "ou-ab12-cdef3456"}
	if targetName(tX) != "Account 111122223333" || targetRef(tX) != "account 111122223333" || targetRef(ou) != "OU ou-ab12-cdef3456" {
		t.Fatalf("%q %q %q", targetName(tX), targetRef(tX), targetRef(ou))
	}
	for _, code := range []string{"budget_raised", "scope_removed", "period_start", "stack_deleted", "something_new"} {
		if r := liftReason(code); r == "" || r[len(r)-1] != '.' {
			t.Fatalf("%s: %q", code, r)
		}
	}
}

// Alerts the other tests don't reach: the harness's alert fake checks the
// wording of each.
func TestCodeHashUnknownAlertReadsWell(t *testing.T) {
	h := newHarness(t)
	h.g.SetCodeHash(context.Background(), "", errors.New("lambda: access denied"))
	h.g.SetCodeHash(context.Background(), "", nil)
	if alertCount(h, "code hash") != 2 {
		t.Fatalf("alerts %v", h.alerts.list)
	}
}
