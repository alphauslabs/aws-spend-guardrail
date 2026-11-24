package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// PriceSource reads the enterprise's shared prices. It is the guard's reader
// credential.
type PriceSource interface {
	LatestPrices(ctx context.Context) (*jennah.PriceVersion, error)
	PriceChunkDoc(ctx context.Context, version int64, pack, region string) ([]byte, error)
}

// The AI pack's price chunks: one per region, each held as its own state item
// (add-regional-prices D3). The us-east-1 item keeps the name it had before
// regional prices.
const (
	aiPricePack     = "ai"
	heldAIPricesFmt = "prices.ai.%s"
	heldAIRegions   = "prices.ai.regions" // the regions held
	pricesStatus    = "prices.status"
)

func heldAIPrices(region string) string { return fmt.Sprintf(heldAIPricesFmt, region) }

// heldRegions lists the regions whose chunks the guard holds.
type heldRegions struct {
	Regions []string `json:"regions"`
}

// heldChunk is a shared price chunk the guard holds, kept as its own item in
// the state table (design D4 of add-shared-prices).
type heldChunk struct {
	Version int64           `json:"version"`
	SHA256  string          `json:"sha256"`
	Doc     json.RawMessage `json:"doc"`
}

type priceStatus struct {
	Failing string `json:"failing,omitempty"` // why shared prices aren't read; empty when they are
}

// pullPrices fetches each shared AI price chunk whose hash changed, one per
// region, and keeps the last good copy of each (add-regional-prices D3). It
// never fails the pull: shared prices only raise or add, so going without them
// never loosens anything.
func (g *Guard) pullPrices(ctx context.Context) {
	if g.Fast == nil || g.Prices == nil {
		return
	}
	store := g.Fast.Engine.Store
	var index heldRegions
	_, _ = store.GetMeta(ctx, heldAIRegions, &index)
	held := map[string]heldChunk{}
	for _, r := range uniqueRegions(append(index.Regions, ai.DefaultRegion)) {
		var h heldChunk
		if ok, _ := store.GetMeta(ctx, heldAIPrices(r), &h); ok && len(h.Doc) > 0 {
			held[r] = h
		}
	}
	var status priceStatus
	_, _ = store.GetMeta(ctx, pricesStatus, &status)

	// fail reports once per failing stretch. why is a sentence; err, if any,
	// goes on the alert's Error: line.
	failed := false
	fail := func(why string, err error) {
		failed = true
		if status.Failing == "" {
			g.alert(ctx, "guardrail: shared prices not read", withError(why+" The AI guard keeps pricing with "+keptWhat(held[ai.DefaultRegion])+". Shared prices can only raise prices or add models, so nothing is loosened.", err))
		}
		status.Failing = withError(why, err)
		_ = store.PutMeta(ctx, pricesStatus, status)
	}
	latest, err := g.Prices.LatestPrices(ctx)
	if err != nil {
		why := "The guard can't read shared prices from Jennah."
		if strings.Contains(err.Error(), "PermissionDenied") || strings.Contains(err.Error(), "NotFound") {
			why = "The guard's reader key can't read " + schema.PricesDatasetID + ". Mint the key with that dataset in its reach, or run setup."
		}
		fail(why, err)
		return
	}
	if latest == nil {
		return // nothing published yet
	}
	if latest.Layout > schema.PricesLayoutVersion {
		fail(fmt.Sprintf("Price version %d uses layout %d, newer than this guard understands (%d). Upgrade the guard.", latest.Number, latest.Layout, schema.PricesLayoutVersion), nil)
		return
	}
	var applied []map[string]any
	listed := map[string]bool{}
	for _, ref := range latest.Chunks {
		if ref.Pack != aiPricePack {
			continue
		}
		listed[ref.Region] = true
		h := held[ref.Region]
		if ref.SHA256 == h.SHA256 {
			if h.Version != latest.Number {
				// Same content under a newer version: record the version in use.
				h.Version = latest.Number
				held[ref.Region] = h
				_ = store.PutMeta(ctx, heldAIPrices(ref.Region), h)
			}
			continue
		}
		doc, err := g.Prices.PriceChunkDoc(ctx, latest.Number, aiPricePack, ref.Region)
		if err != nil {
			fail(fmt.Sprintf("The guard can't read the shared AI prices for %s.", ref.Region), err)
			continue
		}
		var t ai.Table
		if doc == nil || jennah.HashChunk(doc) != ref.SHA256 {
			fail(fmt.Sprintf("The shared AI prices for %s of version %d don't match their manifest hash.", ref.Region, latest.Number), nil)
			continue
		}
		if err := json.Unmarshal(doc, &t); err != nil {
			fail(fmt.Sprintf("The shared AI prices for %s of version %d don't parse.", ref.Region, latest.Number), err)
			continue
		}
		h = heldChunk{Version: latest.Number, SHA256: ref.SHA256, Doc: doc}
		if err := store.PutMeta(ctx, heldAIPrices(ref.Region), h); err != nil {
			fail("The guard can't store the shared prices it read.", err)
			continue
		}
		held[ref.Region] = h
		raised, added, ignored := 0, 0, 0
		if g.Fast.Prices != nil {
			raised, added, ignored = ai.SharedEffect(g.Fast.Prices.ByRegion()[ref.Region], t.Models)
		}
		applied = append(applied, map[string]any{"pack": aiPricePack, "region": ref.Region, "raised": raised, "added": added, "ignored_lower": ignored})
	}
	// A region no longer published is no longer held.
	for r := range held {
		if !listed[r] && !failed {
			delete(held, r)
			_ = store.PutMeta(ctx, heldAIPrices(r), heldChunk{})
		}
	}
	regions := make([]string, 0, len(held))
	for r := range held {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	if !sameStrings(regions, index.Regions) {
		_ = store.PutMeta(ctx, heldAIRegions, heldRegions{Regions: regions})
	}
	if len(applied) > 0 {
		g.emit(ctx, schema.KindPricesApplied, fmt.Sprintf("%s:%d", schema.KindPricesApplied, latest.Number), "", 0,
			map[string]any{"price_version": latest.Number, "chunks": applied})
	}
	if status.Failing != "" && !failed {
		status.Failing = ""
		_ = store.PutMeta(ctx, pricesStatus, status)
		g.alert(ctx, "guardrail: shared prices read again", fmt.Sprintf("The AI guard reads shared prices again (version %d).", latest.Number))
	}
}

func uniqueRegions(rs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rs {
		if r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func keptWhat(h heldChunk) string {
	if h.Version > 0 {
		return fmt.Sprintf("the shared prices it already had (version %d)", h.Version)
	}
	return "the prices in its release"
}

// sharedAIPrices returns the shared AI prices the guard holds, per region,
// their tier prices per region, model, and tier (add-tier-pricing D2), and the
// newest version among them; nil and 0 when it holds none.
func (g *Guard) sharedAIPrices(ctx context.Context) (map[string]map[string]config.ModelPrice, map[string]map[string]map[string]config.ModelPrice, int64) {
	if g.Fast == nil {
		return nil, nil, 0
	}
	store := g.Fast.Engine.Store
	var index heldRegions
	_, _ = store.GetMeta(ctx, heldAIRegions, &index)
	out := map[string]map[string]config.ModelPrice{}
	tiers := map[string]map[string]map[string]config.ModelPrice{}
	var version int64
	for _, r := range uniqueRegions(append(index.Regions, ai.DefaultRegion)) {
		var h heldChunk
		if ok, _ := store.GetMeta(ctx, heldAIPrices(r), &h); !ok || len(h.Doc) == 0 {
			continue
		}
		var t ai.Table
		if err := json.Unmarshal(h.Doc, &t); err != nil {
			continue
		}
		out[r] = t.Models
		if len(t.Tiers) > 0 {
			tiers[r] = t.Tiers
		}
		version = max(version, h.Version)
	}
	if len(out) == 0 {
		return nil, nil, 0
	}
	return out, tiers, version
}
