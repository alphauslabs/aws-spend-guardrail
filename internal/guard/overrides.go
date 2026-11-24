package guard

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// reportedPriceOverrideLow keys the reported records of config prices found
// below the known price (add-price-override-check D3).
const reportedPriceOverrideLow = "price_override_low"

// checkPriceOverrides reports, once per entry and known price, each ai.prices
// entry below the model's price from the release and shared tables. Pricing
// doesn't change: the entry still wins until the org's config changes it.
func (g *Guard) checkPriceOverrides(ctx context.Context, eff *config.Resolved, version int64) error {
	if g.Fast == nil || eff == nil || eff.AI == nil {
		return nil
	}
	reported, err := g.State.Reported(ctx, reportedPriceOverrideLow)
	if err != nil {
		return fmt.Errorf("read reported low prices: %w", err)
	}
	shared, sharedTiers, _ := g.sharedAIPrices(ctx)
	pack := ai.NewTiered(g.Fast.Prices, shared, sharedTiers, eff.AI)
	models := make([]string, 0, len(eff.AI.Prices))
	for m := range eff.AI.Prices {
		models = append(models, m)
	}
	sort.Strings(models)
	low := map[string]bool{}
	for _, m := range models {
		entry := eff.AI.Prices[m]
		known, ok := pack.KnownPrice(ai.ModelKey(m))
		if !ok || (entry.InputPer1K >= known.InputPer1K && entry.OutputPer1K >= known.OutputPer1K) {
			continue
		}
		key := m + "|" + priceWords(entry) + "|" + priceWords(known)
		low[key] = true
		if _, done := reported[key]; done {
			continue
		}
		g.emit(ctx, schema.KindPriceOverrideLow, "price_override_low:"+key+":"+g.now().Format("2006-01-02"), "", version, map[string]any{
			"model": m,
			"entry": map[string]float64{"input_per_1k": entry.InputPer1K, "output_per_1k": entry.OutputPer1K},
			"known": map[string]float64{"input_per_1k": known.InputPer1K, "output_per_1k": known.OutputPer1K},
		})
		g.alert(ctx, "guardrail: config price below the known price", fmt.Sprintf(
			"This payer org's config prices %s at %s in ai.prices, below what the guard otherwise knows for it: %s, the lowest across the regions its release and shared prices list it in. Calls to it are still priced at the config's price, so its spend may be counted low and caps may trip late. To fix it, raise the entry to the known price, which applies at once, or remove it, which needs approval.",
			m, priceWords(entry), priceWords(known)))
		_ = g.State.MarkReported(ctx, reportedPriceOverrideLow, key, g.now())
	}
	// Forget entries that are no longer low, so a later return is reported.
	for key := range reported {
		if !low[key] {
			_ = g.State.ClearReported(ctx, reportedPriceOverrideLow, key)
		}
	}
	return nil
}

// priceWords is a price as alerts give it: USD per 1,000 input and output
// tokens, as plain decimals.
func priceWords(p config.ModelPrice) string {
	return strings.Join([]string{config.Decimal(p.InputPer1K), "USD input and", config.Decimal(p.OutputPer1K), "USD output per 1,000 tokens"}, " ")
}
