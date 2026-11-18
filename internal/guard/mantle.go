package guard

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// mantleCoverage reports, once each, active accounts that don't share their
// mantle usage with the payer account in a watched region: not linked to its
// observability sink there (add-mantle-usage D6). Without a sink in every
// watched region, nothing is reported or cleared; reading that region's
// metrics fails, which the fast path alerts once.
func (g *Guard) mantleCoverage(ctx context.Context, eff *config.Resolved, version int64) error {
	if g.Fast == nil || len(g.Fast.MantleRegions) == 0 || g.Fast.MantleLinks == nil || eff.AI == nil {
		return nil
	}
	accounts, err := g.Org.ListAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	names := map[string]string{}
	var errs []error
	var current []string
	complete := true
	for _, region := range g.Fast.MantleRegions {
		sink, linked, err := g.Fast.MantleLinks(ctx, region)
		if err != nil {
			errs = append(errs, fmt.Errorf("mantle links in %s: %w", region, err))
			continue
		}
		if sink == "" {
			complete = false // the fast path alerts that it can't read this region
			continue
		}
		for _, a := range accounts {
			if a.State != "ACTIVE" || eff.Excluded(a.ID) || linked[a.ID] {
				continue
			}
			names[a.ID] = a.Name
			current = append(current, a.ID+"@"+region)
		}
	}
	if len(errs) > 0 || !complete {
		// Without every region's links, a lapsed report can't be told from a
		// failed read or a missing sink; keep what was reported.
		return errors.Join(errs...)
	}
	sort.Strings(current)
	fresh, err := g.reportOnceScoped(ctx, schema.KindMantleUnlinked, current, version,
		func(id string) string { return "account:" + strings.SplitN(id, "@", 2)[0] },
		func(id string) map[string]any {
			acct, region, _ := strings.Cut(id, "@")
			return map[string]any{"account_id": acct, "name": names[acct], "region": region}
		})
	if len(fresh) > 0 {
		var lines []string
		for _, d := range fresh {
			lines = append(lines, fmt.Sprintf("- %s (%s) in %s", d["account_id"], d["name"], d["region"]))
		}
		g.alert(ctx, fmt.Sprintf("guardrail: %d account(s) not sharing mantle usage", len(fresh)),
			"The AI guard can't count bedrock-mantle spend in these accounts, because they aren't linked to the payer account's CloudWatch observability sink in that region. Link them, preferably with the organization-wide link template so new accounts are linked too (see docs/ai-guard.md), or exclude them in the config if leaving them out is intended:\n"+strings.Join(lines, "\n"))
	}
	return err
}
