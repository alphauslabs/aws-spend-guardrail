package guard

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/internal/prices"
)

// PricePublisher builds the AI price table from the Price List and publishes
// it to the enterprise's shared prices, with the price publisher key, as
// `guardrail prices publish` does with no manual changes
// (add-scheduled-price-publish D3, D4).
type PricePublisher func(ctx context.Context) (*prices.PublishResult, error)

// metaPricePublish names the scheduled publish's alert state.
const metaPricePublish = "prices.publish"

// pricePublishState is what the scheduled publish remembers between runs, so
// each condition is alerted once (design D5, D9).
type pricePublishState struct {
	Failing  string                       `json:"failing,omitempty"`
	Unpriced []string                     `json:"unpriced,omitempty"`
	Stale    map[string]prices.StalePrice `json:"stale,omitempty"`
}

// PublishSharedPrices is the publish_prices task: publish the day's shared
// prices and alert on a failure (once per failing stretch), its recovery, a
// version written, models newly without a price, and manual prices newly
// above the Price List. A failure is alerted, not returned, so the schedule
// doesn't retry it all day; the next day's run tries again.
func (g *Guard) PublishSharedPrices(ctx context.Context) error {
	if g.PublishPrices == nil {
		return nil
	}
	var st pricePublishState
	if _, err := g.State.Meta(ctx, metaPricePublish, &st); err != nil {
		return fmt.Errorf("read price publish state: %w", err)
	}
	r, err := g.PublishPrices(ctx)
	if err != nil {
		if st.Failing == "" {
			g.alert(ctx, "guardrail: shared prices not published", withError(
				"The scheduled publish of shared AI prices failed, so they weren't updated. Guards keep pricing with the prices they have. The guard tries again on its next daily run.", err))
		}
		st.Failing = err.Error()
		return g.State.SetMeta(ctx, metaPricePublish, st)
	}
	if st.Failing != "" {
		st.Failing = ""
		g.alert(ctx, "guardrail: shared prices published again", "The scheduled publish of shared AI prices works again.")
	}
	if r.Written() {
		g.alert(ctx, fmt.Sprintf("guardrail: shared prices version %d published", r.Version), publishedText(r))
	}

	// Models the build can't price, alerted when new (D5).
	var unpriced, fresh []string
	known := map[string]bool{}
	for _, id := range st.Unpriced {
		known[id] = true
	}
	for _, u := range r.Unmatched {
		unpriced = append(unpriced, u.ID)
		if !known[u.ID] {
			fresh = append(fresh, fmt.Sprintf("- %s (%s)", u.ID, u.Name))
		}
	}
	sort.Strings(unpriced)
	if len(fresh) > 0 {
		g.alert(ctx, "guardrail: models with no price", "The AWS Price List has no price the guard can use for these Bedrock models. Guards treat calls to them as unknown models. To price one for every guard, publish a manual price with guardrail prices publish --overrides FILE --note TEXT (see docs/operating.md):\n"+strings.Join(fresh, "\n"))
	}
	st.Unpriced = unpriced

	// Manual prices above the Price List, alerted when new or changed (D9).
	stale := map[string]prices.StalePrice{}
	var changed []string
	for _, s := range r.Stale {
		stale[s.Model] = s
		if prev, ok := st.Stale[s.Model]; !ok || prev != s {
			changed = append(changed, fmt.Sprintf("- %s: manual %s, Price List %s", s.Model, s.Manual, s.List))
		}
	}
	if len(changed) > 0 {
		g.alert(ctx, "guardrail: manual prices above the Price List", "These manual shared prices are above what the AWS Price List now charges on input or output, so guards keep charging the higher manual price. If the Price List is right, drop each one with guardrail prices publish --drop-override MODEL --note TEXT:\n"+strings.Join(changed, "\n"))
	}
	st.Stale = stale
	return g.State.SetMeta(ctx, metaPricePublish, st)
}

// publishedText describes a version the scheduled publish wrote.
func publishedText(r *prices.PublishResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The scheduled publish wrote shared AI prices version %d. Guards apply added and raised prices on their next pull; lowered or removed prices take effect only through a release.", r.Version)
	var lines []string
	for _, ch := range r.Changes {
		switch ch.Kind {
		case "added":
			lines = append(lines, fmt.Sprintf("- %s added: %s", ch.Model, ch.To))
		case "removed":
			lines = append(lines, fmt.Sprintf("- %s removed: was %s", ch.Model, ch.From))
		default:
			lines = append(lines, fmt.Sprintf("- %s %s: %s, was %s", ch.Model, ch.Kind, ch.To, ch.From))
		}
	}
	for _, id := range r.AutoDropped {
		lines = append(lines, fmt.Sprintf("- %s: manual price dropped, since the Price List now prices it at or above it", id))
	}
	if len(lines) > 0 {
		b.WriteString(" Changes:\n" + strings.Join(lines, "\n"))
	}
	return b.String()
}
