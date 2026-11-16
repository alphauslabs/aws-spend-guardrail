package cli

import (
	"context"
	"fmt"

	"github.com/alphauslabs/aws-spend-guardrail/internal/prices"
)

// AIChunk is the pack and region of the AI price chunk.
const (
	AIPricePack   = prices.AIPack
	AIPriceRegion = prices.AIRegion
)

// PublishOptions are a price publish's choices beyond the built table.
type PublishOptions = prices.PublishOptions

// ErrNoteNeeded is returned when manual prices change without a note
// (add-manual-price-overrides D4).
var ErrNoteNeeded = prices.ErrNoteNeeded

// PublishPrices shows how a built table, with the manual prices carried
// forward and changed as asked, differs from the shared one in the
// enterprise, and publishes it as a new version unless o.DryRun is set. The
// work is prices.Publish, shared with a publishing guard; this prints it.
func (c *CLI) PublishPrices(ctx context.Context, enterprise string, built *prices.Table, unmatched []prices.Unmatched, o PublishOptions) error {
	fmt.Fprintf(c.Out, "enterprise %s, dataset guardrail.prices\n", enterprise)
	if o.Now.IsZero() {
		o.Now = c.now()
	}
	r, err := prices.Publish(ctx, c.J, built, unmatched, o)
	if r.Read {
		if r.Latest != nil {
			fmt.Fprintf(c.Out, "latest version %d (%s)\n", r.Latest.Number, r.Latest.PublishedAt.Format("2006-01-02 15:04 MST"))
		} else {
			fmt.Fprintln(c.Out, "no version published yet")
		}
	}
	if err != nil && r.Changes == nil {
		return err // failed before comparing: nothing to show
	}
	for _, ch := range r.Changes {
		mark := ""
		if _, ok := r.Manual[ch.Model]; ok {
			mark = " (manual)"
		}
		switch ch.Kind {
		case "added":
			fmt.Fprintf(c.Out, "  added    %s: %s%s\n", ch.Model, ch.To, mark)
		case "removed":
			fmt.Fprintf(c.Out, "  removed  %s: was %s\n", ch.Model, ch.From)
		default:
			fmt.Fprintf(c.Out, "  %-8s %s: %s -> %s%s\n", ch.Kind, ch.Model, ch.From, ch.To, mark)
		}
	}
	for _, id := range r.Dropped {
		fmt.Fprintf(c.Out, "  manual price dropped for %s\n", id)
	}
	for _, id := range r.AutoDropped {
		fmt.Fprintf(c.Out, "  manual price dropped for %s: the Price List now prices it at or above it\n", id)
	}
	for _, n := range r.ListHigher {
		fmt.Fprintf(c.Out, "  note     %s: the Price List price is higher on %s (%s), so it is published there\n", n.Model, n.Sides, n.Price)
	}
	for _, id := range r.CarriedStale {
		fmt.Fprintf(c.Out, "  note     %s: the Price List now prices it; drop the manual price with --drop-override %s if it is no longer needed\n", id, id)
	}
	for _, n := range r.BelowFloor {
		fmt.Fprintf(c.Status, "warning: guards will price %s at no less than %s (its provider's highest price in this release), above its manual price on %s\n", n.Model, n.Price, n.Sides)
	}
	for _, u := range r.Unmatched {
		fmt.Fprintf(c.Status, "no price found for %s (%s); it trips as unknown unless allowlisted and priced in config, or priced with --overrides\n", u.ID, u.Name)
	}
	if err != nil {
		return err
	}
	if len(r.Changes) == 0 && !r.ManualChanged {
		fmt.Fprintln(c.Out, "no changes")
		return nil
	}
	if len(r.Changes) > 0 {
		fmt.Fprintf(c.Out, "%d change(s). Guards apply raised and added prices; lowered prices take effect only through a release.\n", len(r.Changes))
	}
	if r.ManualChanged {
		fmt.Fprintln(c.Out, "manual prices changed")
	}
	if o.DryRun {
		if r.ManualChanged && o.Note == "" && len(r.AutoDropped) == 0 {
			fmt.Fprintln(c.Out, "publishing this needs --note, since manual prices change")
		}
		fmt.Fprintln(c.Out, "dry run: nothing published")
		return nil
	}
	if r.Version == 0 {
		fmt.Fprintln(c.Out, "the latest version already holds these prices; nothing published")
		return nil
	}
	fmt.Fprintf(c.Out, "published price version %d\n", r.Version)
	return nil
}
