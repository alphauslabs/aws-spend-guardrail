package guard

import (
	"context"
	"fmt"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
)

// reportedActionLimit marks a budget whose action AWS refused because the payer
// account is at its budget action limit, so the refusal is alerted once
// (add-enforcing-scope-limit).
const reportedActionLimit = "action_limit"

type limitedAction struct {
	spec BudgetSpec
	err  error
}

// reportActionLimit alerts once for scopes newly left without a budget action at
// AWS's limit, and clears scopes that got their action, left the config, or left
// enforce, so a later refusal alerts again. A budget whose put failed for
// another reason keeps its mark: this run says nothing about its action.
func (g *Guard) reportActionLimit(ctx context.Context, limited []limitedAction, failed map[string]bool) error {
	reported, err := g.State.Reported(ctx, reportedActionLimit)
	if err != nil {
		return fmt.Errorf("read action limit reports: %w", err)
	}
	now := map[string]bool{}
	var fresh []limitedAction
	for _, l := range limited {
		now[l.spec.Name] = true
		if _, ok := reported[l.spec.Name]; !ok {
			fresh = append(fresh, l)
		}
	}
	for name := range reported {
		if !now[name] && !failed[name] {
			_ = g.State.ClearReported(ctx, reportedActionLimit, name)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	for _, l := range fresh {
		_ = g.State.MarkReported(ctx, reportedActionLimit, l.spec.Name, g.now())
	}
	g.alert(ctx, "guardrail: scopes can't freeze, budget action limit reached", withError(actionLimitBody(fresh), fresh[0].err))
	return nil
}

func actionLimitBody(ls []limitedAction) string {
	var who string
	if len(ls) == 1 {
		who = targetName(ls[0].spec.Target) + " has no budget action, so it can't freeze until it has one."
	} else {
		refs := make([]string, len(ls))
		for i, l := range ls {
			refs[i] = targetRef(l.spec.Target)
		}
		who = "These scopes have no budget action, so they can't freeze until they have one: " + strings.Join(refs, ", ") + "."
	}
	return who + fmt.Sprintf(" AWS refused the action because the payer account is at its limit of %d budget actions. ", config.MaxEnforcingScopes) +
		"Enforce fewer scopes, by grouping accounts into OU scopes or setting some to dry-run, or remove budget actions outside the guard. " +
		"Their budgets exist, and the guard tries the actions again on each run."
}
