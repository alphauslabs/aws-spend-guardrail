package guard

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// ultrafastUnseen checks yesterday's ultrafast use, which the guard prices
// only from what the trail records. For each watched region, it reads the
// ultrafast tokens per account and model from the shared metrics, and
// reports, once each, an account and model with ultrafast tokens that the
// trail showed no ultrafast call for that day: a sign AWS records the tier
// where the guard doesn't look, so those calls were priced as standard
// (add-tier-pricing D10).
func (g *Guard) ultrafastUnseen(ctx context.Context, eff *config.Resolved, version int64) error {
	if g.Fast == nil || g.Fast.Ultrafast == nil || len(g.Fast.MantleRegions) == 0 || eff.AI == nil {
		return nil
	}
	day := g.now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	seenRaw, err := activity.UltrafastSeenOn(ctx, g.Fast.Engine.Store, day)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for k := range seenRaw {
		acct, model, _ := strings.Cut(k, "|")
		seen[acct+"|"+ai.ModelKey(model)] = true
	}
	tokens := map[string][2]int64{}
	var current []string
	var errs []error
	for _, region := range g.Fast.MantleRegions {
		used, err := g.Fast.Ultrafast(ctx, region, day)
		if err != nil {
			errs = append(errs, fmt.Errorf("ultrafast metrics in %s: %w", region, err))
			continue
		}
		for _, u := range used {
			if eff.Excluded(u.Account) {
				continue
			}
			k := u.Account + "|" + ai.ModelKey(u.Model)
			if seen[k] {
				continue
			}
			if _, dup := tokens[k]; !dup {
				current = append(current, k)
			}
			t := tokens[k]
			tokens[k] = [2]int64{t[0] + u.In, t[1] + u.Out}
		}
	}
	if len(errs) > 0 {
		// Without every region, a lapsed report can't be told from a failed
		// read; keep what was reported.
		return errors.Join(errs...)
	}
	sort.Strings(current)
	fresh, err := g.reportOnceScoped(ctx, schema.KindUltrafastUnseen, current, version,
		func(id string) string { return "account:" + strings.SplitN(id, "|", 2)[0] },
		func(id string) map[string]any {
			acct, model, _ := strings.Cut(id, "|")
			return map[string]any{"account_id": acct, "model": model, "day": day.Format("2006-01-02"),
				"input_tokens": tokens[id][0], "output_tokens": tokens[id][1]}
		})
	if len(fresh) > 0 {
		var lines []string
		for _, d := range fresh {
			lines = append(lines, fmt.Sprintf("- %s calling %s: %d input and %d output tokens", d["account_id"], d["model"], d["input_tokens"], d["output_tokens"]))
		}
		g.alert(ctx, fmt.Sprintf("guardrail: ultrafast use the trail didn't show (%d)", len(fresh)),
			"CloudWatch shows ultrafast-tier Bedrock tokens on "+day.Format("2006-01-02")+" that the CloudTrail records the guard reads didn't mark as ultrafast, so the guard priced those calls at the standard rate, a sixth of their cost or less. AWS may record the tier somewhere the guard doesn't read yet; check docs/aws-facts.md and report it:\n"+strings.Join(lines, "\n"))
	}
	return err
}
