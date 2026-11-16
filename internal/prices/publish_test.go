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

func built(models map[string]config.ModelPrice) *Table {
	return &Table{Source: Source, Basis: Basis, Published: "2026-11-16T00:00:00Z", Models: models}
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
	if !strings.Contains(r.Note, "Manual price for x.equal dropped: the Price List now prices it at or above it.") || !strings.Contains(r.Note, "x.above-one") {
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
