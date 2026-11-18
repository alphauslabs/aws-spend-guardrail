package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
	"github.com/alphauslabs/aws-spend-guardrail/internal/prices"
)

func table(models map[string]config.ModelPrice) *prices.Built {
	regions := map[string]map[string]config.ModelPrice{prices.LegacyRegion: models}
	return &prices.Built{Table: &prices.Table{Source: prices.Source, Basis: prices.Basis, Published: "2026-11-11T00:00:00Z", Models: prices.Legacy(regions), Regions: regions}}
}

func TestPublishPricesShowsChangesAndHonoursDryRun(t *testing.T) {
	ctx := context.Background()
	c, f, out, status := newCLI(t)
	if err := c.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	t1 := table(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}, "a.m2": {InputPer1K: 1, OutputPer1K: 1}})
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", t1, []prices.Unmatched{{ID: "x.new", Name: "New"}}, PublishOptions{Note: "first"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "enterprise ent-1") || !strings.Contains(got, "added    a.m1") || !strings.Contains(got, "published price version 1") {
		t.Fatalf("first publish:\n%s", got)
	}
	if !strings.Contains(status.String(), "no price found for x.new") {
		t.Fatalf("unmatched not reported: %s", status)
	}

	t2 := table(map[string]config.ModelPrice{"a.m1": {InputPer1K: 2, OutputPer1K: 2}, "a.m3": {InputPer1K: 1, OutputPer1K: 1}})
	commits := f.Commits
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", t2, nil, PublishOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"raised   a.m1", "added    a.m3", "removed  a.m2", "dry run: nothing published"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if f.Commits != commits {
		t.Fatal("a dry run wrote something")
	}
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", t1, nil, PublishOptions{}); err != nil || !strings.Contains(out.String(), "no changes") {
		t.Fatalf("same prices: %v\n%s", err, out)
	}
}

// manualSetup publishes version 1 from a build that prices a.m1 and a.m2 only.
func manualSetup(t *testing.T) (*CLI, *jennahtest.Server, *bytes.Buffer, *bytes.Buffer, *prices.Built) {
	t.Helper()
	c, f, out, status := newCLI(t)
	if err := c.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	built := table(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}, "a.m2": {InputPer1K: 1, OutputPer1K: 1}})
	if err := c.PublishPrices(context.Background(), "ent-1", built, nil, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	status.Reset()
	return c, f, out, status, built
}

func latestTable(t *testing.T, c *CLI) (*prices.Table, int64) {
	t.Helper()
	v, err := c.J.LatestPrices(context.Background())
	if err != nil || v == nil {
		t.Fatalf("latest: %v %v", v, err)
	}
	doc, err := c.J.PriceChunkDoc(context.Background(), v.Number, AIPricePack, AIPriceRegion)
	if err != nil {
		t.Fatal(err)
	}
	var tb prices.Table
	if err := json.Unmarshal(doc, &tb); err != nil {
		t.Fatal(err)
	}
	return &tb, v.Number
}

func TestManualPricesAddCarryForwardAndDrop(t *testing.T) {
	ctx := context.Background()
	c, f, out, _, built := manualSetup(t)
	grok := map[string]config.ModelPrice{"x.grok": {InputPer1K: 3, OutputPer1K: 15}}

	// A new manual price without a note is refused, and nothing is written.
	commits := f.Commits
	if err := c.PublishPrices(ctx, "ent-1", built, []prices.Unmatched{{ID: "x.grok", Name: "Grok"}}, PublishOptions{Manual: grok}); !errors.Is(err, ErrNoteNeeded) || f.Commits != commits {
		t.Fatalf("err %v, commits %d", err, f.Commits-commits)
	}
	// With a note, it is published and marked.
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", built, nil, PublishOptions{Manual: grok, Note: "price grok by hand"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "added    x.grok: in 3 / out 15 per 1k (manual)") || !strings.Contains(got, "published price version 2") {
		t.Fatalf("output:\n%s", got)
	}
	tb, v := latestTable(t, c)
	if v != 2 || tb.Models["x.grok"].OutputPer1K != 15 || tb.Manual["x.grok"].SetInVersion != 2 {
		t.Fatalf("version %d: %+v %+v", v, tb.Models, tb.Manual)
	}

	// A routine publish (no file, no note) carries it forward and writes nothing.
	out.Reset()
	commits = f.Commits
	if err := c.PublishPrices(ctx, "ent-1", built, []prices.Unmatched{{ID: "x.grok", Name: "Grok"}}, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no changes") || f.Commits != commits {
		t.Fatalf("routine publish wrote %d commits:\n%s", f.Commits-commits, out)
	}
	// The same price given again keeps the version that set it.
	if err := c.PublishPrices(ctx, "ent-1", built, nil, PublishOptions{Manual: grok}); err != nil || f.Commits != commits {
		t.Fatalf("same manual price: %v, %d commits", err, f.Commits-commits)
	}

	// A routine publish with a changed build still keeps the manual price.
	built2 := table(map[string]config.ModelPrice{"a.m1": {InputPer1K: 2, OutputPer1K: 2}, "a.m2": {InputPer1K: 1, OutputPer1K: 1}})
	if err := c.PublishPrices(ctx, "ent-1", built2, nil, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if tb, v := latestTable(t, c); v != 3 || tb.Models["x.grok"].InputPer1K != 3 || tb.Manual["x.grok"].SetInVersion != 2 {
		t.Fatalf("version %d: %+v %+v", v, tb.Models, tb.Manual)
	}

	// Dropping needs a note; an unknown model is an error.
	if err := c.PublishPrices(ctx, "ent-1", built2, nil, PublishOptions{Drop: []string{"x.other"}, Note: "x"}); err == nil || !strings.Contains(err.Error(), "no manual price for x.other") {
		t.Fatalf("unknown drop: %v", err)
	}
	if err := c.PublishPrices(ctx, "ent-1", built2, nil, PublishOptions{Drop: []string{"us.x.grok"}}); !errors.Is(err, ErrNoteNeeded) {
		t.Fatalf("drop without a note: %v", err)
	}
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", built2, nil, PublishOptions{Drop: []string{"us.x.grok"}, Note: "AWS lists grok now"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "removed  x.grok") || !strings.Contains(got, "manual price dropped for x.grok") {
		t.Fatalf("drop output:\n%s", got)
	}
	if tb, _ := latestTable(t, c); tb.Manual != nil || strings.Contains(tb.Basis, prices.ManualNote) {
		t.Fatalf("after the drop: %+v, basis %q", tb.Manual, tb.Basis)
	}
}

func TestManualPriceNotes(t *testing.T) {
	ctx := context.Background()
	c, _, out, status, _ := manualSetup(t)
	release := &ai.Table{Models: map[string]config.ModelPrice{"x.grok-old": {InputPer1K: 0.0022, OutputPer1K: 0.0066}}}

	// a.m1 is priced by the Price List higher on input; x.grok-cheap is below
	// its provider's floor; x.grok-fine is above it.
	built := table(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}, "a.m2": {InputPer1K: 1, OutputPer1K: 1}})
	manual := map[string]config.ModelPrice{
		"a.m1":         {InputPer1K: 0.5, OutputPer1K: 3},
		"x.grok-cheap": {InputPer1K: 0.001, OutputPer1K: 0.01},
		"x.grok-fine":  {InputPer1K: 0.003, OutputPer1K: 0.01},
	}
	if err := c.PublishPrices(ctx, "ent-1", built, nil, PublishOptions{Manual: manual, Note: "n", Release: release}); err != nil {
		t.Fatal(err)
	}
	got, warn := out.String(), status.String()
	for _, want := range []string{
		"raised   a.m1: in 1 / out 2 per 1k -> in 1 / out 3 per 1k (manual)",
		"note     a.m1: the Price List price is higher on input (in 1 / out 2 per 1k), so it is published there",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if !strings.Contains(warn, "guards will price x.grok-cheap at no less than in 0.0022 / out 0.0066 per 1k") || !strings.Contains(warn, "above its manual price on input") || strings.Contains(warn, "x.grok-fine") {
		t.Errorf("floor warnings:\n%s", warn)
	}
	if strings.Contains(got, "now prices it") {
		t.Errorf("a price given in this publish must not be told to drop:\n%s", got)
	}

	// Next publish carries a.m1 forward: suggest dropping it.
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", built, nil, PublishOptions{Release: release}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "note     a.m1: the Price List now prices it; drop the manual price with --drop-override a.m1") {
		t.Errorf("carry-forward note:\n%s", out)
	}
}

// A manual price the Price List now covers is dropped by itself, and the CLI
// says so (add-scheduled-price-publish D8).
func TestManualPriceDroppedWhenCovered(t *testing.T) {
	ctx := context.Background()
	c, _, out, _, built := manualSetup(t)
	if err := c.PublishPrices(ctx, "ent-1", built, nil, PublishOptions{Manual: map[string]config.ModelPrice{"x.grok": {InputPer1K: 3, OutputPer1K: 15}}, Note: "n"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	covered := table(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}, "a.m2": {InputPer1K: 1, OutputPer1K: 1}, "x.grok": {InputPer1K: 3, OutputPer1K: 15}})
	if err := c.PublishPrices(ctx, "ent-1", covered, nil, PublishOptions{}); err != nil {
		t.Fatalf("no note needed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "manual price dropped for x.grok: the Price List now prices it at or above it") || !strings.Contains(got, "manual prices changed") || !strings.Contains(got, "published price version 3") {
		t.Fatalf("output:\n%s", got)
	}
	v, _ := c.J.LatestPrices(ctx)
	if !strings.Contains(v.Note, "Manual price for x.grok dropped") {
		t.Fatalf("version note %q", v.Note)
	}
}
