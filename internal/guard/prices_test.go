package guard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

type fakePrices struct {
	latest  *jennah.PriceVersion
	docs    map[int64][]byte
	err     error
	fetches int
}

func (f *fakePrices) LatestPrices(context.Context) (*jennah.PriceVersion, error) {
	return f.latest, f.err
}

func (f *fakePrices) PriceChunkDoc(_ context.Context, v int64, pack, region string) ([]byte, error) {
	f.fetches++
	return f.docs[v], nil
}

// publish makes version v with these AI prices; corrupt stores a document
// that doesn't match the manifest hash.
func (f *fakePrices) publish(t *testing.T, v int64, models map[string]config.ModelPrice, corrupt bool) {
	t.Helper()
	doc, err := json.Marshal(map[string]any{"models": models})
	if err != nil {
		t.Fatal(err)
	}
	if f.docs == nil {
		f.docs = map[int64][]byte{}
	}
	f.docs[v] = doc
	if corrupt {
		f.docs[v] = append([]byte(nil), doc[:len(doc)-1]...)
	}
	f.latest = &jennah.PriceVersion{Number: v, Layout: schema.PricesLayoutVersion, Chunks: []jennah.ChunkRef{
		{Pack: aiPricePack, Region: aiPriceRegion, SHA256: jennah.HashChunk(doc), Models: len(models)},
	}}
}

const opus = "anthropic.claude-opus-5-5"

func withPrices(t *testing.T, h *harness) (*fastKit, *fakePrices) {
	k := withFast(t, h)
	src := &fakePrices{}
	h.g.Prices = src
	h.publish(doc("50000", "50000"))
	return k, src
}

func TestSharedPricesFetchedOnceAndApplied(t *testing.T) {
	h := newHarness(t)
	_, src := withPrices(t, h)
	release := h.g.Fast.Prices.Models[opus]
	src.publish(t, 12, map[string]config.ModelPrice{
		opus:                   {InputPer1K: release.InputPer1K * 2, OutputPer1K: release.OutputPer1K * 2},
		"anthropic.claude-new": {InputPer1K: 0.001, OutputPer1K: 0.001},
	}, false)
	h.pull()
	h.pull()
	if src.fetches != 1 {
		t.Fatalf("an unchanged chunk must not be fetched again: %d fetches", src.fetches)
	}
	if h.events.count(schema.KindPricesApplied) != 1 {
		t.Fatalf("events %v", h.events.kinds())
	}
	var d struct {
		Version int64 `json:"price_version"`
		Chunks  []struct{ Raised, Added int }
	}
	for _, e := range h.events.list {
		if e.Kind == schema.KindPricesApplied {
			_ = json.Unmarshal(e.Detail, &d)
		}
	}
	if d.Version != 12 || len(d.Chunks) != 1 || d.Chunks[0].Raised != 1 || d.Chunks[0].Added != 1 {
		t.Fatalf("prices_applied %+v", d)
	}
	if shared, v := h.g.sharedAIPrices(context.Background()); v != 12 || shared[opus].InputPer1K != release.InputPer1K*2 {
		t.Fatalf("held %d %+v", v, shared[opus])
	}
	// The same content under a newer version: no fetch, version recorded.
	src.latest.Number = 13
	h.pull()
	if _, v := h.g.sharedAIPrices(context.Background()); v != 13 || src.fetches != 1 {
		t.Fatalf("version %d, fetches %d", v, src.fetches)
	}
}

func TestCorruptChunkKeepsLastGoodAndAlertsOnce(t *testing.T) {
	h := newHarness(t)
	_, src := withPrices(t, h)
	src.publish(t, 1, map[string]config.ModelPrice{"anthropic.claude-new": {InputPer1K: 0.1, OutputPer1K: 0.1}}, false)
	h.pull()
	src.publish(t, 2, map[string]config.ModelPrice{"anthropic.claude-new": {InputPer1K: 0.2, OutputPer1K: 0.2}}, true)
	h.pull()
	h.pull()
	if _, v := h.g.sharedAIPrices(context.Background()); v != 1 {
		t.Fatalf("must keep version 1, holds %d", v)
	}
	n := 0
	for _, a := range h.alerts.list {
		if strings.Contains(a, "shared prices not read") {
			n++
		}
	}
	if n != 1 || !h.alerts.has("manifest hash") || !h.alerts.has("version 1") {
		t.Fatalf("alerts %v", h.alerts.list)
	}
	src.publish(t, 3, map[string]config.ModelPrice{"anthropic.claude-new": {InputPer1K: 0.3, OutputPer1K: 0.3}}, false)
	h.pull()
	if !h.alerts.has("again (version 3)") {
		t.Fatalf("no recovery alert: %v", h.alerts.list)
	}
}

func TestJennahDownAndKeyWithoutReachFallBackToRelease(t *testing.T) {
	for name, err := range map[string]error{
		"down":     errors.New("read latest price version: unavailable"),
		"no reach": errors.New("read latest price version: PermissionDenied: dataset guardrail.prices"),
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			_, src := withPrices(t, h)
			src.err = err
			h.pull()
			h.pull()
			if shared, _ := h.g.sharedAIPrices(context.Background()); shared != nil {
				t.Fatal("nothing must be held")
			}
			if len(h.alerts.list) != 1 || !h.alerts.has("the prices in its release") {
				t.Fatalf("alerts %v", h.alerts.list)
			}
			if name == "no reach" && !h.alerts.has("Mint the key with that dataset in its reach") {
				t.Fatalf("must say what to fix: %v", h.alerts.list)
			}
		})
	}
}

func TestCutoffNamesPriceVersion(t *testing.T) {
	h := newHarness(t)
	k, src := withPrices(t, h)
	src.publish(t, 7, map[string]config.ModelPrice{opus: {InputPer1K: 1, OutputPer1K: 1}}, false)
	h.publish(`{"payer_account_id":"123456789012","scopes":[],"ai":{"scopes":[{"account_id":"444455556666","hourly_usd":1.5,"mode":"enforce"}]}}`)
	h.pull()
	// At release prices this call is about 0.03 USD; at the shared price, 2 USD.
	k.call(h, "444455556666", "us.anthropic.claude-opus-5-5", 1000, 1000)
	h.trail()
	if !k.policies.Attached["p-ai/444455556666"] {
		t.Fatalf("the raised shared price must apply: %v", k.policies.Calls)
	}
	for _, e := range h.events.list {
		if e.Kind == schema.KindCutoff {
			var d map[string]any
			_ = json.Unmarshal(e.Detail, &d)
			if d["price_version"] != float64(7) {
				t.Fatalf("cutoff detail %v", d)
			}
			return
		}
	}
	t.Fatalf("no cutoff event: %v", h.events.kinds())
}
