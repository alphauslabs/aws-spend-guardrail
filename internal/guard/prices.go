package guard

import (
	"context"
	"encoding/json"
	"fmt"
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

// The AI pack's price chunk.
const (
	aiPricePack   = "ai"
	aiPriceRegion = "us-east-1"
	heldAIPrices  = "prices.ai.us-east-1"
	pricesStatus  = "prices.status"
)

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

// pullPrices fetches the shared AI price chunk when its hash changed, and
// keeps the last good one. It never fails the pull: shared prices only raise
// or add, so going without them never loosens anything.
func (g *Guard) pullPrices(ctx context.Context) {
	if g.Fast == nil || g.Prices == nil {
		return
	}
	store := g.Fast.Engine.Store
	var held heldChunk
	_, _ = store.GetMeta(ctx, heldAIPrices, &held)
	var status priceStatus
	_, _ = store.GetMeta(ctx, pricesStatus, &status)

	fail := func(why string) {
		if status.Failing == "" {
			g.alert(ctx, "guardrail: shared prices not read", why+". The AI guard keeps pricing with "+keptWhat(held)+"; shared prices can only raise prices or add models, so nothing is loosened.")
		}
		status.Failing = why
		_ = store.PutMeta(ctx, pricesStatus, status)
	}
	latest, err := g.Prices.LatestPrices(ctx)
	if err != nil {
		why := "cannot read shared prices from Jennah: " + err.Error()
		if strings.Contains(err.Error(), "PermissionDenied") || strings.Contains(err.Error(), "NotFound") {
			why = "the guard's reader key can't read " + schema.PricesDatasetID + " (mint it with that dataset in its reach, or run setup): " + err.Error()
		}
		fail(why)
		return
	}
	if latest == nil {
		return // nothing published yet
	}
	if latest.Layout > schema.PricesLayoutVersion {
		fail(fmt.Sprintf("price version %d uses layout %d, newer than this guard understands (%d); upgrade the guard", latest.Number, latest.Layout, schema.PricesLayoutVersion))
		return
	}
	ref, ok := latest.Chunk(aiPricePack, aiPriceRegion)
	if !ok {
		return
	}
	if ref.SHA256 != held.SHA256 {
		doc, err := g.Prices.PriceChunkDoc(ctx, latest.Number, aiPricePack, aiPriceRegion)
		if err != nil {
			fail("cannot read shared AI prices: " + err.Error())
			return
		}
		var t ai.Table
		if doc == nil || jennah.HashChunk(doc) != ref.SHA256 {
			fail(fmt.Sprintf("shared AI prices of version %d don't match their manifest hash", latest.Number))
			return
		}
		if err := json.Unmarshal(doc, &t); err != nil {
			fail(fmt.Sprintf("shared AI prices of version %d don't parse: %v", latest.Number, err))
			return
		}
		held = heldChunk{Version: latest.Number, SHA256: ref.SHA256, Doc: doc}
		if err := store.PutMeta(ctx, heldAIPrices, held); err != nil {
			fail("cannot keep shared prices: " + err.Error())
			return
		}
		raised, added, ignored := 0, 0, 0
		if g.Fast.Prices != nil {
			raised, added, ignored = ai.SharedEffect(g.Fast.Prices.Models, t.Models)
		}
		g.emit(ctx, schema.KindPricesApplied, fmt.Sprintf("%s:%d", schema.KindPricesApplied, latest.Number), "", 0,
			map[string]any{"price_version": latest.Number, "chunks": []map[string]any{{
				"pack": aiPricePack, "region": aiPriceRegion, "raised": raised, "added": added, "ignored_lower": ignored,
			}}})
	} else if held.Version != latest.Number {
		// Same content under a newer version: record the version in use.
		held.Version = latest.Number
		_ = store.PutMeta(ctx, heldAIPrices, held)
	}
	if status.Failing != "" {
		status.Failing = ""
		_ = store.PutMeta(ctx, pricesStatus, status)
		g.alert(ctx, "guardrail: shared prices read again", fmt.Sprintf("The AI guard reads shared prices again (version %d).", held.Version))
	}
}

func keptWhat(h heldChunk) string {
	if h.Version > 0 {
		return fmt.Sprintf("the shared prices it already had (version %d)", h.Version)
	}
	return "the prices in its release"
}

// sharedAIPrices returns the shared AI prices the guard holds, and their
// version; nil and 0 when it holds none.
func (g *Guard) sharedAIPrices(ctx context.Context) (map[string]config.ModelPrice, int64) {
	if g.Fast == nil {
		return nil, 0
	}
	var held heldChunk
	if ok, _ := g.Fast.Engine.Store.GetMeta(ctx, heldAIPrices, &held); !ok || len(held.Doc) == 0 {
		return nil, 0
	}
	var t ai.Table
	if err := json.Unmarshal(held.Doc, &t); err != nil {
		return nil, 0
	}
	return t.Models, held.Version
}
