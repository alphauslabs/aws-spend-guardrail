package guard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
	approvalv1 "github.com/alphauslabs/jennah-sdk-go/jennah/approval/v1"
)

func TestApprovalTitles(t *testing.T) {
	a := config.Target{Kind: "account", ID: "891376926337"}
	b := config.Target{Kind: "ou", ID: "ou-ab12-cdef3456"}
	for _, tc := range []struct {
		changes []config.Change
		want    string
	}{
		{[]config.Change{{Target: a, Field: "ceiling_usd", From: "5.30", To: "7.00"}}, "Raise a spend cap for payer 339712710400 (config version 8)"},
		{[]config.Change{{Target: a, Field: "ceiling_usd"}, {Target: a, Field: "threshold_pct"}}, "Raise a spend cap for payer 339712710400 (config version 8)"},
		{[]config.Change{{Target: a, Field: "ceiling_usd"}, {Target: b, Field: "ceiling_usd"}}, "Raise spend caps for payer 339712710400 (config version 8)"},
		{[]config.Change{{Target: b, Field: "scope", From: "present"}}, "Stop protecting a scope for payer 339712710400 (config version 8)"},
		{[]config.Change{{Target: a, Field: "mode", To: config.ModeDryRun}}, "Switch a scope to dry-run for payer 339712710400 (config version 8)"},
		{[]config.Change{{Field: config.FieldAlertEmail, To: "x@example.com"}}, "Change alert recipients for payer 339712710400 (config version 8)"},
		{[]config.Change{{Field: "ai.models", To: "any"}, {Target: a, Field: "ai.hourly_usd"}}, "Loosen the AI guard for payer 339712710400 (config version 8)"},
		{[]config.Change{{Target: a, Field: "ceiling_usd"}, {Target: b, Field: "scope", From: "present"}}, "Loosen spend protection for payer 339712710400 (config version 8)"},
		{[]config.Change{{Target: a, Field: "period", To: config.PeriodQuarterly}}, "Loosen spend protection for payer 339712710400 (config version 8)"},
	} {
		if got := approvalTitle("339712710400", 8, tc.changes); got != tc.want {
			t.Errorf("%v:\n got %q\nwant %q", tc.changes, got, tc.want)
		}
	}
}

// raiseApproval applies a first version with account 111122223333 enforcing at
// a 5.30 USD ceiling, optionally records it frozen, then publishes a raise to
// newCeiling and returns the approval the guard raised for it.
func raiseApproval(t *testing.T, h *harness, frozen bool, newCeiling string) *approvalv1.Approval {
	t.Helper()
	enforce := func(c string) string {
		return `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":` + c + `,"mode":"enforce"}]}`
	}
	h.publish(enforce("5.30"))
	h.pull()
	if frozen {
		if err := h.state.MarkFrozen(context.Background(), Frozen{Target: config.Target{Kind: "account", ID: "111122223333"}, Since: h.now, ThresholdUSD: 5.30}); err != nil {
			t.Fatal(err)
		}
	}
	h.publish(enforce(newCeiling))
	h.pull()
	a := h.jennah.Approval("guardrail:123456789012:msp_a:v2")
	if a == nil {
		t.Fatal("no approval raised")
	}
	if err := alerttext.Check(a.GetSummary()); err != nil {
		t.Errorf("summary breaks the wording rules: %v\n%s", err, a.GetSummary())
	}
	return a
}

func TestApprovalSaysTheRaiseLiftsTheFreeze(t *testing.T) {
	h := newHarness(t)
	h.spend.byAccount["111122223333"] = 6.16
	a := raiseApproval(t, h, true, "7")
	s := a.GetSummary()
	for _, want := range []string{
		"Config version 2 for payer account 123456789012 (MSP msp_a) loosens protection. If approved, it makes this change:",
		"- Raise the ceiling of account 111122223333 from 5.30 USD to 7.00 USD.",
		"Account 111122223333 is frozen, and its spend this month is 6.16 USD, so this change would lift the freeze.",
		"If you approve, the change applies 30 minutes after the guard first saw this version, at 09:30 UTC on 2026-11-10 at the earliest.",
		"If you reject it, or nobody answers by 11:00 UTC on 2026-11-10, the stricter config stays in force.",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	if a.GetTitle() != "Raise a spend cap for payer 123456789012 (config version 2)" {
		t.Errorf("title = %q", a.GetTitle())
	}
	p := a.GetPayload().AsMap()
	if p["earliest_apply_at"] != "2026-11-10T09:30:00Z" || p["expires_at"] != "2026-11-10T11:00:00Z" || p["loosening"] == nil || p["config_version"] != float64(2) {
		t.Errorf("payload = %v", p)
	}
	if strings.Contains(s, "30m0s") || strings.Contains(s, "->") || strings.Contains(s, "ceiling_usd") {
		t.Errorf("summary still has diff or Go formatting:\n%s", s)
	}
}

func TestApprovalSaysARaiseBelowSpendKeepsTheFreeze(t *testing.T) {
	h := newHarness(t)
	h.spend.byAccount["111122223333"] = 6.16
	s := raiseApproval(t, h, true, "6").GetSummary()
	if !strings.Contains(s, "so this change would not lift the freeze: its spend is still at or over the new threshold of 6.00 USD.") {
		t.Fatalf("summary:\n%s", s)
	}
}

func TestApprovalWithoutSpendStillRaised(t *testing.T) {
	h := newHarness(t)
	h.spend.err = errors.New("ce unavailable")
	s := raiseApproval(t, h, true, "7").GetSummary()
	if !strings.Contains(s, "Account 111122223333 is frozen. Its spend couldn't be read from Cost Explorer, so this approval can't say whether the change would lift the freeze.") {
		t.Fatalf("summary:\n%s", s)
	}
}

func TestApprovalForAnUnfrozenScopeHasNoFreezeLine(t *testing.T) {
	h := newHarness(t)
	h.spend.byAccount["111122223333"] = 1
	s := raiseApproval(t, h, false, "7").GetSummary()
	if strings.Contains(s, "frozen") {
		t.Fatalf("an unfrozen scope got a freeze line:\n%s", s)
	}
}

func TestDurationWords(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Minute: "30 minutes", time.Minute: "1 minute", time.Hour: "1 hour",
		90 * time.Minute: "1 hour 30 minutes", 2 * time.Hour: "2 hours", 90 * time.Second: "1m30s",
	} {
		if got := durationWords(d); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
}
