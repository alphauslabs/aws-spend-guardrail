package guard

import (
	"context"
	"errors"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/internal/prices"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

// publishKit is a guard publishing to a fake enterprise's shared prices from
// a build the test sets.
type publishKit struct {
	h         *harness
	f         *jennahtest.Server
	j         *jennah.Client
	build     map[string]config.ModelPrice
	unmatched []prices.Unmatched
	fail      error
}

func newPublishKit(t *testing.T) *publishKit {
	t.Helper()
	k := &publishKit{h: newHarness(t), build: map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}}}
	k.f, k.j = jennahtest.Start(t)
	ctx := context.Background()
	if _, err := k.f.CreateDataset(ctx, &datastorev1.CreateDatasetRequest{DatasetId: schema.PricesDatasetID}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.f.DeclareTables(ctx, &datastorev1.DeclareTablesRequest{DatasetId: schema.PricesDatasetID, Tables: schema.PricesTables()}); err != nil {
		t.Fatal(err)
	}
	k.h.g.PublishPrices = func(ctx context.Context) (*prices.PublishResult, error) {
		if k.fail != nil {
			return nil, k.fail
		}
		models := map[string]config.ModelPrice{}
		for id, p := range k.build {
			models[id] = p
		}
		t := builtOf(models)
		return prices.Publish(ctx, k.j, t, k.unmatched, prices.PublishOptions{Note: "scheduled publish by guard 123456789012", Now: k.h.now})
	}
	return k
}

func (k *publishKit) run() {
	k.h.t.Helper()
	if err := k.h.g.PublishSharedPrices(context.Background()); err != nil {
		k.h.t.Fatal(err)
	}
}

func (k *publishKit) latest() int64 {
	v, _ := k.j.LatestPrices(context.Background())
	if v == nil {
		return 0
	}
	return v.Number
}

func TestScheduledPublishWritesOnlyChanges(t *testing.T) {
	k := newPublishKit(t)
	k.run()
	if k.latest() != 1 || countAlerts(k.h, "shared prices version 1 published") != 1 || !k.h.alerts.has("a.m1 added (in us-east-1): in 1 / out 2 per 1k") {
		t.Fatalf("first run: version %d, alerts %v", k.latest(), k.h.alerts.list)
	}
	v, _ := k.j.LatestPrices(context.Background())
	if v.Note != "scheduled publish by guard 123456789012" {
		t.Fatalf("note %q", v.Note)
	}
	commits, alerts := k.f.Commits, len(k.h.alerts.list)
	k.run() // nothing changed
	if k.f.Commits != commits || len(k.h.alerts.list) != alerts {
		t.Fatalf("an unchanged day wrote %d commits and %d alerts", k.f.Commits-commits, len(k.h.alerts.list)-alerts)
	}
	k.build["a.m1"] = config.ModelPrice{InputPer1K: 1, OutputPer1K: 3}
	k.run()
	if k.latest() != 2 || !k.h.alerts.has("a.m1 raised (in us-east-1): in 1 / out 3 per 1k, was in 1 / out 2 per 1k") {
		t.Fatalf("a rise: version %d, alerts %v", k.latest(), k.h.alerts.list)
	}
}

func TestScheduledPublishKeepsManualPrices(t *testing.T) {
	k := newPublishKit(t)
	ctx := context.Background()
	built := builtOf(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}})
	if _, err := prices.Publish(ctx, k.j, built, nil, prices.PublishOptions{Manual: map[string]config.ModelPrice{"x.grok": {InputPer1K: 3, OutputPer1K: 15}}, Note: "by hand"}); err != nil {
		t.Fatal(err)
	}
	k.build["a.m2"] = config.ModelPrice{InputPer1K: 1, OutputPer1K: 1}
	k.run()
	if k.latest() != 2 {
		t.Fatalf("version %d", k.latest())
	}
	r, err := prices.Publish(ctx, k.j, built, nil, prices.PublishOptions{DryRun: true})
	if err != nil || r.Manual["x.grok"].OutputPer1K != 15 {
		t.Fatalf("the scheduled publish must carry manual prices forward: %+v %v", r.Manual, err)
	}
}

func TestScheduledPublishFailureAlertsOnceAndRecovers(t *testing.T) {
	k := newPublishKit(t)
	k.fail = errors.New("PermissionDenied: key revoked")
	k.run()
	k.run()
	if n := countAlerts(k.h, "shared prices not published"); n != 1 || !k.h.alerts.has("Error: PermissionDenied: key revoked") {
		t.Fatalf("%d failure alerts: %v", n, k.h.alerts.list)
	}
	k.fail = nil
	k.run()
	k.run()
	if n := countAlerts(k.h, "shared prices published again"); n != 1 {
		t.Fatalf("%d recovery alerts", n)
	}
}

func TestScheduledPublishReportsUnpricedModelsOnce(t *testing.T) {
	k := newPublishKit(t)
	k.unmatched = []prices.Unmatched{{ID: "xai.grok-5", Name: "Grok 5"}}
	k.run()
	k.run()
	if n := countAlerts(k.h, "models with no price"); n != 1 || !k.h.alerts.has("- xai.grok-5 (Grok 5)") {
		t.Fatalf("%d alerts: %v", n, k.h.alerts.list)
	}
	k.unmatched = nil // priced
	k.run()
	k.unmatched = []prices.Unmatched{{ID: "xai.grok-5", Name: "Grok 5"}} // unpriced again
	k.run()
	if n := countAlerts(k.h, "models with no price"); n != 2 {
		t.Fatalf("a model priced then unpriced must be reported again: %d", n)
	}
}

func TestScheduledPublishReportsStaleManualPricesOnce(t *testing.T) {
	k := newPublishKit(t)
	ctx := context.Background()
	built := builtOf(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}})
	if _, err := prices.Publish(ctx, k.j, built, nil, prices.PublishOptions{Manual: map[string]config.ModelPrice{"x.grok": {InputPer1K: 3, OutputPer1K: 15}}, Note: "by hand"}); err != nil {
		t.Fatal(err)
	}
	k.build["x.grok"] = config.ModelPrice{InputPer1K: 2, OutputPer1K: 10} // AWS prices it lower
	k.run()
	k.run()
	if n := countAlerts(k.h, "manual prices above the Price List"); n != 1 || !k.h.alerts.has("- x.grok: manual in 3 / out 15 per 1k, Price List in 2 / out 10 per 1k") {
		t.Fatalf("%d alerts: %v", n, k.h.alerts.list)
	}
	k.build["x.grok"] = config.ModelPrice{InputPer1K: 2, OutputPer1K: 11} // its Price List price changes
	k.run()
	if n := countAlerts(k.h, "manual prices above the Price List"); n != 2 {
		t.Fatalf("changed prices must alert again: %d", n)
	}
	k.build["x.grok"] = config.ModelPrice{InputPer1K: 3, OutputPer1K: 15} // the Price List catches up: dropped
	k.run()
	if !k.h.alerts.has("x.grok: manual price dropped, since the Price List now prices it at or above it") {
		t.Fatalf("auto-drop not reported: %v", k.h.alerts.list)
	}
}

// A stack without price publishing does nothing on the task.
func TestNoPublisherNoPublish(t *testing.T) {
	h := newHarness(t)
	if err := h.g.PublishSharedPrices(context.Background()); err != nil || len(h.alerts.list) != 0 {
		t.Fatalf("%v %v", err, h.alerts.list)
	}
}

// builtOf is a build pricing models in us-east-1 only.
func builtOf(models map[string]config.ModelPrice) *prices.Built {
	regions := map[string]map[string]config.ModelPrice{prices.LegacyRegion: models}
	return &prices.Built{Table: &prices.Table{Source: prices.Source, Basis: prices.Basis, Published: "2026-11-16T00:00:00Z", Models: prices.Legacy(regions), Regions: regions}}
}
