package guard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
)

// approvalTitle names a loosening for the approver (improve-approval-wording,
// design D2): one kind of change gets its own title, several kinds the general
// one. The request key, not the title, is what code should match.
func approvalTitle(payer string, version int64, loosening []config.Change) string {
	kinds := map[string]bool{}
	targets := map[config.Target]bool{}
	for _, c := range loosening {
		kinds[looseningKind(c)] = true
		if c.Target.Kind != "" {
			targets[c.Target] = true
		}
	}
	what := "Loosen spend protection"
	if len(kinds) == 1 {
		many := len(targets) > 1
		for k := range kinds {
			switch k {
			case "cap":
				what = pick(many, "Raise spend caps", "Raise a spend cap")
			case "remove":
				what = pick(many, "Stop protecting scopes", "Stop protecting a scope")
			case "dry-run":
				what = pick(many, "Switch scopes to dry-run", "Switch a scope to dry-run")
			case "recipients":
				what = "Change alert recipients"
			case "ai":
				what = "Loosen the AI guard"
			}
		}
	}
	return fmt.Sprintf("%s for payer %s (config version %d)", what, payer, version)
}

func looseningKind(c config.Change) string {
	switch {
	case c.Field == "ceiling_usd" || c.Field == "threshold_pct":
		return "cap"
	case c.Field == "scope":
		return "remove"
	case c.Field == "mode" && c.To == config.ModeDryRun:
		return "dry-run"
	case c.Field == config.FieldAlertEmail:
		return "recipients"
	case strings.HasPrefix(c.Field, "ai."):
		return "ai"
	}
	return "other"
}

func pick(many bool, plural, one string) string {
	if many {
		return plural
	}
	return one
}

// freezeLines says, for each frozen scope a loosening touches, its spend and
// whether approving would lift the freeze, by the rules the guard lifts by
// (design D3). A spend read that fails says so and gives no verdict; it never
// stops the approval being raised.
func (g *Guard) freezeLines(ctx context.Context, loosening []config.Change, effective, desired *config.Resolved) []string {
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil || len(frozen) == 0 {
		return nil
	}
	isFrozen := map[config.Target]bool{}
	for _, f := range frozen {
		isFrozen[f.Target] = true
	}
	var order []config.Target
	byTarget := map[config.Target][]config.Change{}
	for _, c := range loosening {
		if c.Target.Kind == "" || strings.HasPrefix(c.Field, "ai.") || !isFrozen[c.Target] {
			continue
		}
		if _, seen := byTarget[c.Target]; !seen {
			order = append(order, c.Target)
		}
		byTarget[c.Target] = append(byTarget[c.Target], c)
	}
	var out []string
	for _, t := range order {
		cs := byTarget[t]
		scope, inDesired := desired.Scope(t)
		if !inDesired && effective != nil {
			scope, _ = effective.Scope(t)
		}
		subject := targetName(t) + " is frozen"
		specs, err := g.Specs(ctx, &config.Resolved{Scopes: []config.ResolvedScope{scope}})
		if err != nil || len(specs) != 1 {
			out = append(out, subject+". Its spend couldn't be read, so this approval can't say whether the change would lift the freeze.")
			continue
		}
		spend, ok := g.spendFor(ctx, specs[0], "approval")
		if !ok {
			out = append(out, subject+". Its spend couldn't be read from Cost Explorer, so this approval can't say whether the change would lift the freeze.")
			continue
		}
		line := fmt.Sprintf("%s, and its spend %s is %.2f USD", subject, periodText(scope.Period), spend)
		switch {
		case !inDesired:
			line += ", so removing its scope would lift the freeze."
		case raisesCap(cs):
			threshold := scope.CeilingUSD * float64(scope.ThresholdPct) / 100
			if spend < threshold {
				line += ", so this change would lift the freeze."
			} else {
				line += fmt.Sprintf(", so this change would not lift the freeze: its spend is still at or over the new threshold of %.2f USD.", threshold)
			}
		default:
			line += ". This change doesn't lift the freeze by itself."
		}
		out = append(out, line)
	}
	return out
}

func raisesCap(cs []config.Change) bool {
	for _, c := range cs {
		if c.Field == "ceiling_usd" || c.Field == "threshold_pct" {
			return true
		}
	}
	return false
}

func periodText(period string) string {
	switch period {
	case config.PeriodQuarterly:
		return "this quarter"
	case config.PeriodAnnually:
		return "this year"
	}
	return "this month"
}

// durationWords writes a delay for people: "30 minutes", "1 hour", "1 hour 30
// minutes". A delay with seconds falls back to Go's form.
func durationWords(d time.Duration) string {
	if d%time.Minute != 0 {
		return d.String()
	}
	h, m := int(d/time.Hour), int((d%time.Hour)/time.Minute)
	unit := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	switch {
	case h == 0:
		return unit(m, "minute")
	case m == 0:
		return unit(h, "hour")
	}
	return unit(h, "hour") + " " + unit(m, "minute")
}

// utcText writes a time as "13:25 UTC on 2026-11-12".
func utcText(t time.Time) string {
	return t.UTC().Format("15:04 UTC on 2006-01-02")
}
