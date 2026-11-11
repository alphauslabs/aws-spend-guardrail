package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/prices"
)

// AIChunk is the pack and region of the AI price chunk.
const (
	AIPricePack   = "ai"
	AIPriceRegion = "us-east-1"
)

// PublishPrices shows how a built table differs from the shared one in the
// enterprise, and publishes it as a new version unless dryRun is set.
func (c *CLI) PublishPrices(ctx context.Context, enterprise string, t *prices.Table, unmatched []prices.Unmatched, note string, dryRun bool) error {
	fmt.Fprintf(c.Out, "enterprise %s, dataset guardrail.prices\n", enterprise)
	var current *prices.Table
	latest, err := c.J.LatestPrices(ctx)
	if err != nil {
		return err
	}
	if latest != nil {
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
	changes := prices.Diff(prices.Of(current), prices.Of(t))
	for _, ch := range changes {
		switch ch.Kind {
		case "added":
			fmt.Fprintf(c.Out, "  added    %s: %s\n", ch.Model, ch.To)
		case "removed":
			fmt.Fprintf(c.Out, "  removed  %s: was %s\n", ch.Model, ch.From)
		default:
			fmt.Fprintf(c.Out, "  %-8s %s: %s -> %s\n", ch.Kind, ch.Model, ch.From, ch.To)
		}
	}
	for _, u := range unmatched {
		fmt.Fprintf(c.Status, "no price found for %s (%s); it trips as unknown unless allowlisted and priced in config\n", u.ID, u.Name)
	}
	if len(changes) == 0 {
		fmt.Fprintln(c.Out, "no changes")
		return nil
	}
	fmt.Fprintf(c.Out, "%d change(s). Guards apply raised and added prices; lowered prices take effect only through a release.\n", len(changes))
	if dryRun {
		fmt.Fprintln(c.Out, "dry run: nothing published")
		return nil
	}
	doc, err := json.Marshal(t)
	if err != nil {
		return err
	}
	v, err := c.J.PublishPrices(ctx, []jennah.PriceChunk{{Pack: AIPricePack, Region: AIPriceRegion, Document: doc, Models: len(t.Models)}}, note, c.now())
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
