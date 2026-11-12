package config

import (
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
)

var (
	sAcct = Target{Kind: "account", ID: "111122223333"}
	sOU   = Target{Kind: "ou", ID: "ou-ab12-cdef3456"}
	sGone = Target{Kind: "account", ID: "444455556666"}
)

// diffBothWays returns every change Diff produces between a and b, in both
// directions, so each kind is seen loosening and tightening.
func diffBothWays(a, b *Resolved) []Change {
	return append(Diff(a, b), Diff(b, a)...)
}

func TestEveryChangeKindHasASentence(t *testing.T) {
	a := &Resolved{
		PayerAccountID: "123456789012",
		Scopes: []ResolvedScope{
			{Target: sAcct, CeilingUSD: 5.30, Period: PeriodMonthly, ThresholdPct: 90, FreezeStyle: StyleB, Mode: ModeEnforce, MonthEnd: MonthEndStayFrozen},
			{Target: sGone, CeilingUSD: 10, Period: PeriodMonthly, ThresholdPct: 100, FreezeStyle: StyleB, Mode: ModeDryRun, MonthEnd: MonthEndStayFrozen},
		},
		ExcludedAccounts: []string{"999988887777"},
		AlertEmails:      []string{"old@example.com"},
		AI: &ResolvedAI{
			Mode:          ModeEnforce,
			Scopes:        []ResolvedAIScope{{Target: sAcct, HourlyUSD: 5, DailyUSD: 0, MonthlyUSD: 100, Mode: ModeEnforce}, {Target: sGone, HourlyUSD: 1, Mode: ModeDryRun}},
			Models:        []string{"anthropic.claude-sonnet-5-5", "anthropic.claude-opus-5-5"},
			Regions:       nil,
			APIKeyCallers: []string{"arn:aws:iam::111122223333:user/old"},
			Prices:        map[string]ModelPrice{"m.old": {InputPer1K: 0.003, OutputPer1K: 0.015}, "m.both": {InputPer1K: 0.003, OutputPer1K: 0.015}},
			Lifts:         []string{"ai:111122223333:20261111T0930"},
		},
	}
	b := &Resolved{
		PayerAccountID: "123456789012",
		Scopes: []ResolvedScope{
			{Target: sAcct, CeilingUSD: 7.00, Period: PeriodQuarterly, ThresholdPct: 100, FreezeStyle: StyleA, Mode: ModeDryRun, MonthEnd: MonthEndAutoLift},
			{Target: sOU, CeilingUSD: 50, Period: PeriodMonthly, ThresholdPct: 100, FreezeStyle: StyleB, Mode: ModeEnforce, MonthEnd: MonthEndStayFrozen},
		},
		ExcludedAccounts: []string{"777766665555"},
		AlertEmails:      []string{"new@example.com"},
		AI: &ResolvedAI{
			Mode:          ModeDryRun,
			Scopes:        []ResolvedAIScope{{Target: sAcct, HourlyUSD: 10, DailyUSD: 20, MonthlyUSD: 0, Mode: ModeDryRun}, {Target: sOU, HourlyUSD: 2, Mode: ModeEnforce}},
			Models:        []string{"anthropic.claude-sonnet-5-5"},
			Regions:       []string{"us-east-1"},
			APIKeyCallers: []string{"arn:aws:iam::111122223333:user/new"},
			Prices:        map[string]ModelPrice{"m.new": {InputPer1K: 0.001, OutputPer1K: 0.002}, "m.both": {InputPer1K: 0.001, OutputPer1K: 0.005}},
			Lifts:         []string{"ai:111122223333:20261112T1000"},
		},
	}
	changes := diffBothWays(a, b)
	// Models change between a list and one fewer entry, and to and from "any".
	changes = append(changes, diffBothWays(&Resolved{AI: &ResolvedAI{Models: []string{"x"}}}, &Resolved{AI: &ResolvedAI{}})...)

	fields := map[string]bool{}
	for _, c := range changes {
		fields[c.Field] = true
		s := c.Sentence()
		if err := alerttext.Check(s); err != nil {
			t.Errorf("%s: %v: %q", c, err, s)
		}
		for _, bad := range []string{"->", "_usd", "_pct", "(loosening)", "(tightening)", "account:", "ou:", c.Field + ":"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s: sentence contains %q: %q", c, bad, s)
			}
		}
		if strings.HasPrefix(s, "Change "+c.Field+" ") {
			t.Errorf("%s fell through to the generic sentence: %q", c, s)
		}
	}
	for _, f := range []string{"scope", "ceiling_usd", "threshold_pct", "mode", "freeze_style", "month_end", "period", "excluded",
		FieldAlertEmail, FieldAIScope, "ai.hourly_usd", "ai.daily_usd", "ai.monthly_usd", "ai.mode", FieldAIModels, FieldAIRegions,
		FieldAIAPIKey, FieldAIPrice, FieldAILift} {
		if !fields[f] {
			t.Errorf("no change of kind %s was produced, so its sentence isn't tested", f)
		}
	}
}

func TestSentenceWording(t *testing.T) {
	for _, tc := range []struct {
		c    Change
		want string
	}{
		{Change{Target: Target{Kind: "account", ID: "891376926337"}, Field: "ceiling_usd", From: "5.30", To: "7.00", Direction: Loosening},
			"Raise the ceiling of account 891376926337 from 5.30 USD to 7.00 USD."},
		{Change{Target: sOU, Field: "scope", From: "present", Direction: Loosening}, "Stop protecting OU ou-ab12-cdef3456."},
		{Change{Target: sAcct, Field: "threshold_pct", From: "90", To: "100", Direction: Loosening},
			"Raise the freeze threshold of account 111122223333 from 90% to 100% of its ceiling."},
		{Change{Target: sAcct, Field: "ai.daily_usd", From: "none", To: "20.00", Direction: Tightening},
			"Set a daily AI spend cap of 20.00 USD on account 111122223333."},
		{Change{Field: FieldAIModels, From: "[a b]", To: allowlistAnyText, Direction: Loosening},
			"Allow any model, where only a, b were allowed."},
		{Change{Field: FieldAIPrice, From: "m.x in 0.003/out 0.015 per 1k", To: "m.x in 0.001/out 0.005 per 1k", Direction: Loosening},
			"Change the price override for m.x from 0.003 USD input and 0.015 USD output per 1,000 tokens to 0.001 USD input and 0.005 USD output per 1,000 tokens."},
	} {
		if got := tc.c.Sentence(); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.c, got, tc.want)
		}
	}
}
