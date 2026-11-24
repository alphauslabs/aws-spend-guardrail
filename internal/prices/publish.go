package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
)

// The AI price chunks' pack, and the region of the chunk guards of releases
// before regional prices read.
const (
	AIPack   = "ai"
	AIRegion = LegacyRegion
)

// Store is the shared prices dataset, as the publisher reads and writes it.
type Store interface {
	LatestPrices(ctx context.Context) (*jennah.PriceVersion, error)
	PriceChunkDoc(ctx context.Context, version int64, pack, region string) ([]byte, error)
	PublishPrices(ctx context.Context, chunks []jennah.PriceChunk, note string, now time.Time) (int64, error)
}

// ErrNoteNeeded is returned when an operator adds, changes, or drops a manual
// price without a note (add-manual-price-overrides D4).
var ErrNoteNeeded = errors.New("manual prices changed; give --note with the reason")

// PublishOptions are a publish's choices beyond the built table.
type PublishOptions struct {
	// Manual are prices set by hand (--overrides), already validated by
	// ReadManual. They add to or replace the latest version's.
	Manual map[string]config.ModelPrice
	// Drop removes manual prices (--drop-override).
	Drop   []string
	Note   string
	DryRun bool
	// Release is the price table guards of this release use, for the floor
	// warning; nil skips it.
	Release *ai.Table
	Now     time.Time
}

// SideNote is a manual model whose published or charged price differs from
// its manual price on some side.
type SideNote struct {
	Model string
	Sides string  // "input", "output", or "input and output"
	Price PriceOf // the Price List's price, or the guards' floor
}

// StalePrice is a manual price above the Price List's on some side
// (add-scheduled-price-publish D9).
type StalePrice struct {
	Model  string  `json:"model"`
	Manual PriceOf `json:"manual"`
	List   PriceOf `json:"list"`
}

// PublishResult is what a publish found and did.
type PublishResult struct {
	Read          bool                   // the latest version was read
	Latest        *jennah.PriceVersion   // nil when none is published
	Manual        map[string]ManualPrice // the manual prices published
	Changes       []Change               // published prices against the latest version's, per model across regions
	Dropped       []string               // manual prices dropped on request
	AutoDropped   []string               // manual prices the Price List now covers, dropped by themselves (D8)
	ListHigher    []SideNote             // manual prices the Price List beats on some side, in some region
	Stale         []StalePrice           // manual prices above the Price List on some side, in some region
	CarriedStale  []string               // stale models carried forward, not given in this publish
	BelowFloor    []SideNote             // manual prices below what guards charge
	Unmatched     []Unmatched            // models with no price in any region, manual ones left out
	Unlisted      []string               // regions whose model lists couldn't be read
	Regions       int                    // regions published
	FirstRegional bool                   // the latest version had no regional chunks (add-regional-prices D8)
	FirstTiers    bool                   // the latest version had no tier prices (add-tier-pricing D6)
	ChunksChanged bool                   // some chunk differs from the latest version's
	ManualChanged bool                   // the manual prices differ from the latest version's
	Note          string                 // the version note, with automatic drops
	Version       int64                  // the version written; 0 when none
}

// Written reports whether a new version was published.
func (r *PublishResult) Written() bool { return r.Version > 0 }

// Publish compares a regional build, with the latest version's manual prices
// carried forward and changed as asked, against the latest version, and
// publishes one chunk per region unless nothing changed or o.DryRun is set
// (add-regional-prices D2, D7). The CLI prints the result; a publishing guard
// alerts from it (add-scheduled-price-publish D3). On error, the result still
// carries what was read.
func Publish(ctx context.Context, s Store, built *Built, unmatched []Unmatched, o PublishOptions) (*PublishResult, error) {
	r := &PublishResult{Unlisted: built.Unlisted}
	current := map[string]*Table{} // the latest version's chunks, by region
	latest, err := s.LatestPrices(ctx)
	if err != nil {
		return r, err
	}
	r.Read, r.Latest = true, latest
	next := int64(1)
	if latest != nil {
		next = latest.Number + 1
		for _, c := range latest.Chunks {
			if c.Pack != AIPack {
				continue
			}
			doc, err := s.PriceChunkDoc(ctx, latest.Number, AIPack, c.Region)
			if err != nil {
				return r, err
			}
			if doc == nil {
				continue
			}
			t := &Table{}
			if err := json.Unmarshal(doc, t); err != nil {
				return r, fmt.Errorf("latest AI chunk for %s: %w", c.Region, err)
			}
			current[c.Region] = t
		}
	}
	legacy := current[LegacyRegion]
	chunks := map[string]*Table{} // filled once manual prices are settled

	// Manual prices: the latest version's, then the file's, then the drops.
	var prev map[string]ManualPrice
	if legacy != nil {
		prev = legacy.Manual
	}
	manual := map[string]ManualPrice{}
	for id, m := range prev {
		manual[id] = m
	}
	for id, p := range o.Manual {
		if m, ok := manual[id]; ok && m.Price() == p {
			continue // unchanged: keeps the version that set it
		}
		manual[id] = ManualPrice{InputPer1K: p.InputPer1K, OutputPer1K: p.OutputPer1K, SetInVersion: next}
	}
	for _, d := range o.Drop {
		id := ai.ModelKey(d)
		if _, given := o.Manual[id]; given {
			return r, fmt.Errorf("%s is both priced in the manual price file and dropped", id)
		}
		if _, ok := manual[id]; !ok {
			return r, fmt.Errorf("no manual price for %s to drop", id)
		}
		delete(manual, id)
		r.Dropped = append(r.Dropped, id)
	}
	sort.Strings(r.Dropped)
	if !sameManual(prev, manual) && o.Note == "" && !o.DryRun {
		return r, ErrNoteNeeded
	}

	// A carried-forward manual price the Price List covers at or above, on
	// both sides, in every region the model is offered in, changes no
	// published price: drop it (add-scheduled-price-publish D8,
	// add-regional-prices D7).
	var auto []string
	for _, id := range sortedKeys(manual) {
		if _, given := o.Manual[id]; given {
			continue
		}
		if built.coversEverywhere(id, manual[id].Price()) {
			delete(manual, id)
			r.AutoDropped = append(r.AutoDropped, id)
			auto = append(auto, fmt.Sprintf("Manual price for %s dropped: the Price List now prices it at or above it in every region it's offered in.", id))
		}
	}
	chunks = chunksFor(built.Table, manual)
	r.FirstRegional = latest != nil && len(current) <= 1 && len(chunks) > 1
	note := o.Note
	if r.FirstRegional {
		note = strings.TrimSpace(note + " First version with prices per region.")
	}
	r.FirstTiers = latest != nil && !hasTiers(current) && hasTiers(chunks)
	if r.FirstTiers {
		note = strings.TrimSpace(note + " First version with service tier prices (flex, priority, and ultrafast), so each is listed as added.")
	}
	r.Note = strings.TrimSpace(note + " " + strings.Join(auto, " "))
	r.ManualChanged = !sameManual(prev, manual)
	r.Manual = manual

	r.Regions = len(chunks)
	perRegion := map[string][]Change{}
	for region, t := range chunks {
		base := current[region]
		if base == nil {
			base = legacy // what guards charged in this region until now
		}
		perRegion[region] = append(Diff(Of(base), Of(t)), Diff(OfTiers(current[region]), OfTiers(t))...)
	}
	for region, t := range current {
		if _, ok := chunks[region]; !ok {
			perRegion[region] = append(Diff(Of(t), nil), Diff(OfTiers(t), nil)...)
		}
	}
	r.Changes = MergeChanges(perRegion)
	SplitTiers(r.Changes)
	for i := range r.Changes {
		c := &r.Changes[i]
		if c.Kind != "raised" || c.factor() < JumpFactor {
			continue
		}
		c.Factor = math.Round(c.factor()*10) / 10
		// Name the usage types only when the build set the price shown, not a
		// manual price. Tier prices don't keep their usage types.
		if p, ok := built.Table.Regions[c.Region][c.Model]; ok && c.Tier == "" && p.InputPer1K == c.to.In && p.OutputPer1K == c.to.Out {
			c.Usage = built.Sources[c.Region][c.Model]
		}
	}
	for _, id := range sortedKeys(manual) {
		m := manual[id].Price()
		lo, hi, listed := built.listRange(id)
		if listed {
			if side := higherSides(hi, m); side != "" {
				r.ListHigher = append(r.ListHigher, SideNote{Model: id, Sides: side, Price: priceOf(hi)})
			}
			if higherSides(m, lo) != "" {
				r.Stale = append(r.Stale, StalePrice{Model: id, Manual: priceOf(m), List: priceOf(lo)})
				if _, given := o.Manual[id]; !given {
					r.CarriedStale = append(r.CarriedStale, id)
				}
			}
			continue
		}
		if o.Release == nil || releasePrices(o.Release, id) {
			continue
		}
		f := o.Release.Floor(id)
		if side := higherSides(f, m); side != "" {
			r.BelowFloor = append(r.BelowFloor, SideNote{Model: id, Sides: side, Price: priceOf(f)})
		}
	}
	for _, u := range unmatched {
		if _, ok := manual[ai.ModelKey(u.ID)]; !ok {
			r.Unmatched = append(r.Unmatched, u)
		}
	}
	r.ChunksChanged = changedChunks(current, chunks)
	if !r.ChunksChanged && !r.ManualChanged || o.DryRun {
		return r, nil
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	regions := make([]string, 0, len(chunks))
	for region := range chunks {
		regions = append(regions, region)
	}
	sort.Strings(regions)
	var out []jennah.PriceChunk
	for _, region := range regions {
		t := chunks[region]
		doc, err := json.Marshal(t)
		if err != nil {
			return r, err
		}
		out = append(out, jennah.PriceChunk{Pack: AIPack, Region: region, Document: doc, Models: len(t.Models)})
	}
	r.Version, err = s.PublishPrices(ctx, out, r.Note, now)
	return r, err
}

// chunksFor is a version's chunks: each region's built prices with the manual
// prices merged (add-regional-prices D7), and the us-east-1 chunk as the
// legacy view with the manual prices recorded (D2).
func chunksFor(t *Table, manual map[string]ManualPrice) map[string]*Table {
	merged := map[string]map[string]config.ModelPrice{}
	for region, models := range t.Regions {
		merged[region] = MergeManual(&Table{Models: models}, manual).Models
	}
	if _, ok := merged[LegacyRegion]; !ok {
		merged[LegacyRegion] = MergeManual(&Table{Models: map[string]config.ModelPrice{}}, manual).Models
	}
	out := map[string]*Table{}
	for region, models := range merged {
		c := &Table{Source: t.Source, Published: t.Published, Basis: strings.TrimSuffix(t.Basis, ManualNote), Models: models, Tiers: t.RegionTiers[region]}
		if len(manual) > 0 {
			c.Basis += ManualNote
		}
		out[region] = c
	}
	l := out[LegacyRegion]
	l.Models = Legacy(merged)
	if len(manual) > 0 {
		l.Manual = make(map[string]ManualPrice, len(manual))
		for id, m := range manual {
			l.Manual[id] = m
		}
	}
	return out
}

// hasTiers reports whether any chunk has tier prices.
func hasTiers(chunks map[string]*Table) bool {
	for _, c := range chunks {
		if len(c.Tiers) > 0 {
			return true
		}
	}
	return false
}

// changedChunks reports whether a version's chunks differ from the latest's.
// The Price List's publication date is left out: AWS restamps it about daily
// without changing a price, and a version that changes nothing else would
// only make every guard fetch the same prices again.
func changedChunks(current, next map[string]*Table) bool {
	if len(current) != len(next) {
		return true
	}
	for region, n := range next {
		c, ok := current[region]
		if !ok {
			return true
		}
		cc, nn := *c, *n
		cc.Published, nn.Published = "", ""
		a, _ := json.Marshal(cc)
		b, _ := json.Marshal(nn)
		if jennah.HashChunk(a) != jennah.HashChunk(b) {
			return true
		}
	}
	return false
}

// coversEverywhere reports whether the Price List prices a model at or above
// m, on both sides, in every region the model is offered in, or, when no
// model list names it, in every region it's priced in.
func (b *Built) coversEverywhere(id string, m config.ModelPrice) bool {
	regions := b.Offered[id]
	if len(regions) == 0 {
		regions = map[string]bool{}
		for region, models := range b.Table.Regions {
			if _, ok := models[id]; ok {
				regions[region] = true
			}
		}
	}
	if len(regions) == 0 {
		return false
	}
	for region := range regions {
		p, ok := b.Table.Regions[region][id]
		if !ok || p.InputPer1K < m.InputPer1K || p.OutputPer1K < m.OutputPer1K {
			return false
		}
	}
	return true
}

// listRange is a model's lowest and highest Price List price across regions,
// input and output separately; listed is false when no region prices it.
func (b *Built) listRange(id string) (lo, hi config.ModelPrice, listed bool) {
	for _, models := range b.Table.Regions {
		p, ok := models[id]
		if !ok {
			continue
		}
		if !listed {
			lo, hi, listed = p, p, true
			continue
		}
		lo = config.ModelPrice{InputPer1K: min(lo.InputPer1K, p.InputPer1K), OutputPer1K: min(lo.OutputPer1K, p.OutputPer1K)}
		hi = config.ModelPrice{InputPer1K: max(hi.InputPer1K, p.InputPer1K), OutputPer1K: max(hi.OutputPer1K, p.OutputPer1K)}
	}
	return lo, hi, listed
}

// releasePrices reports whether a release table prices a model in any region.
func releasePrices(t *ai.Table, id string) bool {
	for _, models := range t.ByRegion() {
		if _, ok := models[id]; ok {
			return true
		}
	}
	return false
}

func priceOf(p config.ModelPrice) PriceOf { return PriceOf{In: p.InputPer1K, Out: p.OutputPer1K} }

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

func sameManual(a, b map[string]ManualPrice) bool {
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

func sortedKeys(m map[string]ManualPrice) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
