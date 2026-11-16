package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
	"github.com/alphauslabs/aws-spend-guardrail/internal/prices"
)

// AIChunk is the pack and region of the AI price chunk.
const (
	AIPricePack   = "ai"
	AIPriceRegion = "us-east-1"
)

// PublishOptions are a price publish's choices beyond the built table.
type PublishOptions struct {
	// Manual are prices set by hand (--overrides), already validated by
	// prices.ReadManual. They add to or replace the latest version's.
	Manual map[string]config.ModelPrice
	// Drop removes manual prices (--drop-override).
	Drop   []string
	Note   string
	DryRun bool
	// Release is the price table guards of this release use, for the floor
	// warning; nil skips it.
	Release *ai.Table
}

// ErrNoteNeeded is returned when manual prices change without a note
// (add-manual-price-overrides D4).
var ErrNoteNeeded = errors.New("manual prices changed; give --note with the reason")

// PublishPrices shows how a built table, with the manual prices carried
// forward and changed as asked, differs from the shared one in the
// enterprise, and publishes it as a new version unless o.DryRun is set.
func (c *CLI) PublishPrices(ctx context.Context, enterprise string, built *prices.Table, unmatched []prices.Unmatched, o PublishOptions) error {
	fmt.Fprintf(c.Out, "enterprise %s, dataset guardrail.prices\n", enterprise)
	var current *prices.Table
	latest, err := c.J.LatestPrices(ctx)
	if err != nil {
		return err
	}
	next := int64(1)
	if latest != nil {
		next = latest.Number + 1
		fmt.Fprintf(c.Out, "latest version %d (%s)\n", latest.Number, latest.PublishedAt.Format("2006-01-02 15:04 MST"))
		doc, err := c.J.PriceChunkDoc(ctx, latest.Number, AIPricePack, AIPriceRegion)
		if err != nil {
			return err
		}
		if doc != nil {
			current = &prices.Table{}
			if err := json.Unmarshal(doc, current); err != nil {
				return fmt.Errorf("latest AI chunk: %w", err)
			}
		}
	} else {
		fmt.Fprintln(c.Out, "no version published yet")
	}

	// Manual prices: the latest version's, then the file's, then the drops.
	var prev map[string]prices.ManualPrice
	if current != nil {
		prev = current.Manual
	}
	manual := map[string]prices.ManualPrice{}
	for id, m := range prev {
		manual[id] = m
	}
	for id, p := range o.Manual {
		if m, ok := manual[id]; ok && m.Price() == p {
			continue // unchanged: keeps the version that set it
		}
		manual[id] = prices.ManualPrice{InputPer1K: p.InputPer1K, OutputPer1K: p.OutputPer1K, SetInVersion: next}
	}
	var dropped []string
	for _, d := range o.Drop {
		id := ai.ModelKey(d)
		if _, given := o.Manual[id]; given {
			return fmt.Errorf("%s is both priced in the manual price file and dropped", id)
		}
		if _, ok := manual[id]; !ok {
			return fmt.Errorf("no manual price for %s to drop", id)
		}
		delete(manual, id)
		dropped = append(dropped, id)
	}
	manualChanged := !sameManual(prev, manual)
	if manualChanged && o.Note == "" && !o.DryRun {
		return ErrNoteNeeded
	}

	t := prices.MergeManual(built, manual)
	changes := prices.Diff(prices.Of(current), prices.Of(t))
	for _, ch := range changes {
		mark := ""
		if _, ok := manual[ch.Model]; ok {
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
	sort.Strings(dropped)
	for _, id := range dropped {
		fmt.Fprintf(c.Out, "  manual price dropped for %s\n", id)
	}
	c.manualNotes(built, manual, o)
	for _, u := range unmatched {
		if _, ok := manual[ai.ModelKey(u.ID)]; ok {
			continue
		}
		fmt.Fprintf(c.Status, "no price found for %s (%s); it trips as unknown unless allowlisted and priced in config, or priced with --overrides\n", u.ID, u.Name)
	}
	if len(changes) == 0 && !manualChanged {
		fmt.Fprintln(c.Out, "no changes")
		return nil
	}
	if len(changes) > 0 {
		fmt.Fprintf(c.Out, "%d change(s). Guards apply raised and added prices; lowered prices take effect only through a release.\n", len(changes))
	}
	if manualChanged {
		fmt.Fprintln(c.Out, "manual prices changed")
	}
	if o.DryRun {
		if manualChanged && o.Note == "" {
			fmt.Fprintln(c.Out, "publishing this needs --note, since manual prices change")
		}
		fmt.Fprintln(c.Out, "dry run: nothing published")
		return nil
	}
	doc, err := json.Marshal(t)
	if err != nil {
		return err
	}
	v, err := c.J.PublishPrices(ctx, []jennah.PriceChunk{{Pack: AIPricePack, Region: AIPriceRegion, Document: doc, Models: len(t.Models)}}, o.Note, c.now())
	if err != nil {
		return err
	}
	if v == 0 {
		fmt.Fprintln(c.Out, "the latest version already holds these prices; nothing published")
		return nil
	}
	fmt.Fprintf(c.Out, "published price version %d\n", v)
	return nil
}

// manualNotes explains each manual price the published table doesn't use as
// given (design D5): where the Price List is higher, where the Price List now
// prices a model carried forward by hand, and where guards will charge their
// floor for a model the release doesn't list.
func (c *CLI) manualNotes(built *prices.Table, manual map[string]prices.ManualPrice, o PublishOptions) {
	ids := make([]string, 0, len(manual))
	for id := range manual {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := manual[id]
		if b, listed := built.Models[id]; listed {
			if side := higherSides(b, m.Price()); side != "" {
				fmt.Fprintf(c.Out, "  note     %s: the Price List price is higher on %s (%s), so it is published there\n", id, side, prices.PriceOf{In: b.InputPer1K, Out: b.OutputPer1K})
			}
			if _, given := o.Manual[id]; !given {
				fmt.Fprintf(c.Out, "  note     %s: the Price List now prices it; drop the manual price with --drop-override %s if it is no longer needed\n", id, id)
			}
			continue
		}
		if o.Release == nil {
			continue
		}
		if _, inRelease := o.Release.Models[id]; inRelease {
			continue
		}
		f := o.Release.Floor(id)
		if side := higherSides(f, m.Price()); side != "" {
			fmt.Fprintf(c.Status, "warning: guards will price %s at no less than %s (its provider's highest price in this release), above its manual price on %s\n", id, prices.PriceOf{In: f.InputPer1K, Out: f.OutputPer1K}, side)
		}
	}
}

// higherSides names the sides on which a is above b: "input", "output",
// "input and output", or "".
func higherSides(a, b config.ModelPrice) string {
	in, out := a.InputPer1K > b.InputPer1K, a.OutputPer1K > b.OutputPer1K
	switch {
	case in && out:
		return "input and output"
	case in:
		return "input"
	case out:
		return "output"
	}
	return ""
}

func sameManual(a, b map[string]prices.ManualPrice) bool {
	if len(a) != len(b) {
		return false
	}
	for id, m := range a {
		if n, ok := b[id]; !ok || n.Price() != m.Price() {
			return false
		}
	}
	return true
}
