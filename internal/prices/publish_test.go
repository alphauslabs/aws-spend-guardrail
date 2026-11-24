package prices

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

func pricesStore(t *testing.T) (*jennahtest.Server, *jennah.Client) {
	t.Helper()
	f, c := jennahtest.Start(t)
	ctx := context.Background()
	if _, err := f.CreateDataset(ctx, &datastorev1.CreateDatasetRequest{DatasetId: schema.PricesDatasetID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DeclareTables(ctx, &datastorev1.DeclareTablesRequest{DatasetId: schema.PricesDatasetID, Tables: schema.PricesTables()}); err != nil {
		t.Fatal(err)
	}
	return f, c
}

// built is a build that prices models in us-east-1 only.
func built(models map[string]config.ModelPrice) *Built {
	return builtIn(map[string]map[string]config.ModelPrice{LegacyRegion: models}, nil)
}

// builtIn is a build with prices per region, and the regions each model's
// list names.
func builtIn(regions map[string]map[string]config.ModelPrice, offered map[string][]string) *Built {
	b := &Built{Table: &Table{Source: Source, Basis: Basis, Published: "2026-11-16T00:00:00Z", Models: Legacy(regions), Regions: regions}, Offered: map[string]map[string]bool{}}
	for id, rs := range offered {
		b.Offered[id] = map[string]bool{}
		for _, r := range rs {
			b.Offered[id][r] = true
		}
	}
	return b
}

func chunkOf(t *testing.T, c *jennah.Client, region string) *Table {
	t.Helper()
	v, err := c.LatestPrices(context.Background())
	if err != nil || v == nil {
		t.Fatalf("latest %v %v", v, err)
	}
	doc, _ := c.PriceChunkDoc(context.Background(), v.Number, AIPack, region)
	if doc == nil {
		return nil
	}
	var tb Table
	if err := json.Unmarshal(doc, &tb); err != nil {
		t.Fatal(err)
	}
	return &tb
}

func latestOf(t *testing.T, c *jennah.Client) *Table {
	t.Helper()
	v, err := c.LatestPrices(context.Background())
	if err != nil || v == nil {
		t.Fatalf("latest %v %v", v, err)
	}
	doc, _ := c.PriceChunkDoc(context.Background(), v.Number, AIPack, AIRegion)
	var tb Table
	if err := json.Unmarshal(doc, &tb); err != nil {
		t.Fatal(err)
	}
	return &tb
}

// The result says what was compared and written (design D3).
func TestPublishReturnsWhatItDid(t *testing.T) {
	ctx := context.Background()
	f, c := pricesStore(t)
	t1 := built(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}})
	r, err := Publish(ctx, c, t1, []Unmatched{{ID: "x.new", Name: "New"}}, PublishOptions{Note: "first", Now: time.Now()})
	if err != nil || !r.Read || r.Latest != nil || r.Version != 1 || len(r.Changes) != 1 || r.Changes[0].Kind != "added" || len(r.Unmatched) != 1 || r.Note != "first" {
		t.Fatalf("first: %+v %v", r, err)
	}
	commits := f.Commits
	r, err = Publish(ctx, c, t1, nil, PublishOptions{})
	if err != nil || r.Written() || r.Latest.Number != 1 || len(r.Changes) != 0 || f.Commits != commits {
		t.Fatalf("unchanged: %+v %v", r, err)
	}
	restamped := built(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}})
	restamped.Table.Published = "2026-11-17T00:00:00Z"
	r, err = Publish(ctx, c, restamped, nil, PublishOptions{})
	if err != nil || r.Written() || len(r.Changes) != 0 || f.Commits != commits {
		t.Fatalf("only the Price List's date changed: %+v %v", r, err)
	}
	r, err = Publish(ctx, c, built(map[string]config.ModelPrice{"a.m1": {InputPer1K: 2, OutputPer1K: 2}}), nil, PublishOptions{DryRun: true})
	if err != nil || r.Written() || len(r.Changes) != 1 || r.Changes[0].Kind != "raised" || f.Commits != commits {
		t.Fatalf("dry run: %+v %v", r, err)
	}
}

// A carried-forward manual price the Price List covers at or above on both
// sides is dropped by itself, with no note needed (design D8); one above the
// Price List on a side is kept and reported stale (D9).
func TestPublishDropsCoveredManualPricesAndReportsStale(t *testing.T) {
	ctx := context.Background()
	_, c := pricesStore(t)
	base := map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 1}}
	manual := map[string]config.ModelPrice{
		"x.equal":     {InputPer1K: 3, OutputPer1K: 15},
		"x.above-one": {InputPer1K: 3, OutputPer1K: 15},
		"x.stale":     {InputPer1K: 3, OutputPer1K: 15},
		"x.given":     {InputPer1K: 3, OutputPer1K: 15},
	}
	if _, err := Publish(ctx, c, built(base), nil, PublishOptions{Manual: manual, Note: "price by hand"}); err != nil {
		t.Fatal(err)
	}

	// The Price List now prices all four: equal, higher on output only,
	// lower on output, and equal for one given again in this publish.
	listed := map[string]config.ModelPrice{
		"a.m1":        {InputPer1K: 1, OutputPer1K: 1},
		"x.equal":     {InputPer1K: 3, OutputPer1K: 15},
		"x.above-one": {InputPer1K: 3, OutputPer1K: 18},
		"x.stale":     {InputPer1K: 3, OutputPer1K: 10},
		"x.given":     {InputPer1K: 3, OutputPer1K: 15},
	}
	r, err := Publish(ctx, c, built(listed), nil, PublishOptions{Manual: map[string]config.ModelPrice{"x.given": {InputPer1K: 3, OutputPer1K: 15}}})
	if err != nil {
		t.Fatalf("no note is needed for automatic drops: %v", err)
	}
	if strings.Join(r.AutoDropped, ",") != "x.above-one,x.equal" {
		t.Fatalf("auto-dropped %v", r.AutoDropped)
	}
	if !strings.Contains(r.Note, "Manual price for x.equal dropped: the Price List now prices it at or above it in every region it's offered in.") || !strings.Contains(r.Note, "x.above-one") {
		t.Fatalf("note %q", r.Note)
	}
	if len(r.Stale) != 1 || r.Stale[0].Model != "x.stale" || r.Stale[0].Manual.Out != 15 || r.Stale[0].List.Out != 10 || strings.Join(r.CarriedStale, ",") != "x.stale" {
		t.Fatalf("stale %+v %v", r.Stale, r.CarriedStale)
	}
	tb := latestOf(t, c)
	if _, ok := tb.Manual["x.equal"]; ok || tb.Manual["x.stale"].OutputPer1K != 15 || tb.Manual["x.given"].OutputPer1K != 15 {
		t.Fatalf("manual %+v", tb.Manual)
	}
	// Published prices: unchanged for the equal one, the Price List's higher
	// output for the other, the manual output for the stale one.
	if tb.Models["x.equal"].OutputPer1K != 15 || tb.Models["x.above-one"].OutputPer1K != 18 || tb.Models["x.stale"].OutputPer1K != 15 {
		t.Fatalf("models %+v", tb.Models)
	}
	// An operator's own change still needs a note.
	if _, err := Publish(ctx, c, built(listed), nil, PublishOptions{Drop: []string{"x.stale"}}); !errors.Is(err, ErrNoteNeeded) {
		t.Fatalf("drop without a note: %v", err)
	}
}

// A version holds a chunk per region; the us-east-1 chunk also prices models
// not offered there at their highest, for guards of earlier releases, which
// read only it (add-regional-prices D2).
func TestPublishChunksPerRegion(t *testing.T) {
	ctx := context.Background()
	_, c := pricesStore(t)
	// The latest version is from before regional prices: one chunk.
	if _, err := Publish(ctx, c, built(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}}), nil, PublishOptions{Note: "old"}); err != nil {
		t.Fatal(err)
	}
	regions := map[string]map[string]config.ModelPrice{
		"us-east-1": {"a.m1": {InputPer1K: 1, OutputPer1K: 2}},
		"eu-west-2": {"a.m1": {InputPer1K: 1.5, OutputPer1K: 3}, "d.regional": {InputPer1K: 0.5, OutputPer1K: 1}},
		"us-west-2": {"a.m1": {InputPer1K: 1, OutputPer1K: 2}, "d.regional": {InputPer1K: 0.4, OutputPer1K: 1.2}},
	}
	r, err := Publish(ctx, c, builtIn(regions, nil), nil, PublishOptions{})
	if err != nil || !r.Written() || r.Regions != 3 || !r.FirstRegional || !strings.Contains(r.Note, "First version with prices per region.") {
		t.Fatalf("publish: %+v %v", r, err)
	}
	// Changes are against the us-east-1 prices guards used everywhere.
	var raised, added *Change
	for i, ch := range r.Changes {
		switch {
		case ch.Model == "a.m1" && ch.Kind == "raised":
			raised = &r.Changes[i]
		case ch.Model == "d.regional" && ch.Kind == "added":
			added = &r.Changes[i]
		}
	}
	if raised == nil || raised.Region != "eu-west-2" || raised.Regions != 1 || added == nil || added.Regions != 3 {
		t.Fatalf("changes %+v", r.Changes)
	}
	if eu := chunkOf(t, c, "eu-west-2"); eu == nil || eu.Models["a.m1"].InputPer1K != 1.5 || len(eu.Regions) != 0 {
		t.Fatalf("eu-west-2 chunk %+v", eu)
	}
	// What a v0.2.4 guard reads: us-east-1 prices, and the regional-only
	// model at its highest, input and output separately.
	legacy := chunkOf(t, c, "us-east-1")
	if legacy.Models["a.m1"].InputPer1K != 1 || legacy.Models["d.regional"] != (config.ModelPrice{InputPer1K: 0.5, OutputPer1K: 1.2}) {
		t.Fatalf("us-east-1 chunk %+v", legacy.Models)
	}
	// The same build again writes nothing.
	if r, err := Publish(ctx, c, builtIn(regions, nil), nil, PublishOptions{}); err != nil || r.Written() || r.FirstRegional {
		t.Fatalf("unchanged: %+v %v", r, err)
	}
}

// A manual price applies in every region, and is dropped by itself only once
// the Price List covers it in every region the model is offered in
// (add-regional-prices D7).
func TestManualPricesAcrossRegions(t *testing.T) {
	ctx := context.Background()
	_, c := pricesStore(t)
	offered := map[string][]string{"z.glm": {"us-east-1", "eu-central-1"}}
	start := map[string]map[string]config.ModelPrice{
		"us-east-1":    {"a.m1": {InputPer1K: 1, OutputPer1K: 1}},
		"eu-central-1": {"a.m1": {InputPer1K: 1.2, OutputPer1K: 1.2}},
	}
	manual := map[string]config.ModelPrice{"z.glm": {InputPer1K: 0.001, OutputPer1K: 0.004}}
	if _, err := Publish(ctx, c, builtIn(start, offered), nil, PublishOptions{Manual: manual, Note: "price GLM by hand"}); err != nil {
		t.Fatal(err)
	}
	if eu := chunkOf(t, c, "eu-central-1"); eu.Models["z.glm"] != manual["z.glm"] {
		t.Fatalf("manual price in eu-central-1: %+v", eu.Models)
	}
	// The Price List covers it in us-east-1 only, and higher in input: the
	// manual price is kept, and us-east-1 takes the higher input.
	some := map[string]map[string]config.ModelPrice{
		"us-east-1":    {"a.m1": {InputPer1K: 1, OutputPer1K: 1}, "z.glm": {InputPer1K: 0.0012, OutputPer1K: 0.004}},
		"eu-central-1": {"a.m1": {InputPer1K: 1.2, OutputPer1K: 1.2}},
	}
	r, err := Publish(ctx, c, builtIn(some, offered), nil, PublishOptions{})
	if err != nil || len(r.AutoDropped) != 0 || r.Manual["z.glm"].InputPer1K != 0.001 {
		t.Fatalf("kept: %+v %v", r, err)
	}
	if us, eu := chunkOf(t, c, "us-east-1"), chunkOf(t, c, "eu-central-1"); us.Models["z.glm"].InputPer1K != 0.0012 || eu.Models["z.glm"].InputPer1K != 0.001 {
		t.Fatalf("us %+v eu %+v", us.Models["z.glm"], eu.Models["z.glm"])
	}
	// Covered in both offered regions: dropped by itself.
	all := map[string]map[string]config.ModelPrice{
		"us-east-1":    some["us-east-1"],
		"eu-central-1": {"a.m1": {InputPer1K: 1.2, OutputPer1K: 1.2}, "z.glm": {InputPer1K: 0.0013, OutputPer1K: 0.005}},
	}
	r, err = Publish(ctx, c, builtIn(all, offered), nil, PublishOptions{})
	if err != nil || strings.Join(r.AutoDropped, ",") != "z.glm" || len(r.Manual) != 0 {
		t.Fatalf("dropped: %+v %v", r, err)
	}
}

// A raise of JumpFactor or more names how large it is and the usage types
// that set it, unless a manual price set it.
func TestPublishPointsOutLargeRaises(t *testing.T) {
	ctx := context.Background()
	_, c := pricesStore(t)
	std := map[string]config.ModelPrice{"openai.gpt-6-astra": {InputPer1K: 0.011, OutputPer1K: 0.055}, "a.m1": {InputPer1K: 1, OutputPer1K: 2}}
	if _, err := Publish(ctx, c, built(std), nil, PublishOptions{Note: "first", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	b := built(map[string]config.ModelPrice{"openai.gpt-6-astra": {InputPer1K: 0.066, OutputPer1K: 0.33}, "a.m1": {InputPer1K: 1.5, OutputPer1K: 2}})
	b.Sources = map[string]map[string]RateSource{LegacyRegion: {"openai.gpt-6-astra": {In: "USE1-MP:USE1_input_tokens_ultrafast-Units", Out: "USE1-MP:USE1_output_tokens_ultrafast-Units"}}}
	r, err := Publish(ctx, c, b, nil, PublishOptions{DryRun: true})
	if err != nil || len(r.Changes) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	for _, ch := range r.Changes {
		switch ch.Model {
		case "a.m1":
			if ch.Factor != 0 || ch.Jump() != "" {
				t.Errorf("a 1.5x raise pointed out: %+v", ch)
			}
		case "openai.gpt-6-astra":
			want := "6.0 times the previous price, from AWS usage types USE1-MP:USE1_input_tokens_ultrafast-Units (input) and USE1-MP:USE1_output_tokens_ultrafast-Units (output)"
			if ch.Factor != 6 || !strings.HasPrefix(ch.Jump(), want) {
				t.Errorf("jump %q", ch.Jump())
			}
		}
	}
	// A manual price that set the new price names no usage type.
	r, err = Publish(ctx, c, b, nil, PublishOptions{DryRun: true, Manual: map[string]config.ModelPrice{"a.m1": {InputPer1K: 9, OutputPer1K: 9}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range r.Changes {
		if ch.Model == "a.m1" && (ch.Factor != 9 || ch.Usage != (RateSource{})) {
			t.Errorf("manual raise: %+v", ch)
		}
	}
}
