package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
)

// The AI price chunk's pack and region.
const (
	AIPack   = "ai"
	AIRegion = "us-east-1"
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
	Changes       []Change               // published prices against the latest version's
	Dropped       []string               // manual prices dropped on request
	AutoDropped   []string               // manual prices the Price List now covers, dropped by themselves (D8)
	ListHigher    []SideNote             // manual prices the Price List beats on some side
	Stale         []StalePrice           // manual prices above the Price List on some side
	CarriedStale  []string               // stale models carried forward, not given in this publish
	BelowFloor    []SideNote             // manual prices below what guards charge
	Unmatched     []Unmatched            // models with no price, manual ones left out
	ManualChanged bool                   // the manual prices differ from the latest version's
	Note          string                 // the version note, with automatic drops
	Version       int64                  // the version written; 0 when none
}

// Written reports whether a new version was published.
func (r *PublishResult) Written() bool { return r.Version > 0 }

// Publish compares a built table, with the latest version's manual prices
// carried forward and changed as asked, against the latest version, and
// publishes it unless nothing changed or o.DryRun is set. The CLI prints the
// result; a publishing guard alerts from it (add-scheduled-price-publish D3).
// On error, the result still carries what was read.
func Publish(ctx context.Context, s Store, built *Table, unmatched []Unmatched, o PublishOptions) (*PublishResult, error) {
	r := &PublishResult{}
	var current *Table
	latest, err := s.LatestPrices(ctx)
	if err != nil {
		return r, err
	}
	r.Read, r.Latest = true, latest
	next := int64(1)
	if latest != nil {
		next = latest.Number + 1
		doc, err := s.PriceChunkDoc(ctx, latest.Number, AIPack, AIRegion)
		if err != nil {
			return r, err
		}
		if doc != nil {
			current = &Table{}
			if err := json.Unmarshal(doc, current); err != nil {
				return r, fmt.Errorf("latest AI chunk: %w", err)
			}
		}
	}

	// Manual prices: the latest version's, then the file's, then the drops.
	var prev map[string]ManualPrice
	if current != nil {
		prev = current.Manual
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
	// both sides, changes no published price: drop it (D8).
	var auto []string
	for _, id := range sortedKeys(manual) {
		m := manual[id]
		b, listed := built.Models[id]
		if _, given := o.Manual[id]; given || !listed {
			continue
		}
		if b.InputPer1K >= m.InputPer1K && b.OutputPer1K >= m.OutputPer1K {
			delete(manual, id)
			r.AutoDropped = append(r.AutoDropped, id)
			auto = append(auto, fmt.Sprintf("Manual price for %s dropped: the Price List now prices it at or above it.", id))
		}
	}
	r.Note = strings.TrimSpace(o.Note + " " + strings.Join(auto, " "))
	r.ManualChanged = !sameManual(prev, manual)
	r.Manual = manual

	t := MergeManual(built, manual)
	r.Changes = Diff(Of(current), Of(t))
	for _, id := range sortedKeys(manual) {
		m := manual[id].Price()
		if b, listed := built.Models[id]; listed {
			if side := higherSides(b, m); side != "" {
				r.ListHigher = append(r.ListHigher, SideNote{Model: id, Sides: side, Price: priceOf(b)})
			}
			if higherSides(m, b) != "" {
				r.Stale = append(r.Stale, StalePrice{Model: id, Manual: priceOf(m), List: priceOf(b)})
				if _, given := o.Manual[id]; !given {
					r.CarriedStale = append(r.CarriedStale, id)
				}
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
		if side := higherSides(f, m); side != "" {
			r.BelowFloor = append(r.BelowFloor, SideNote{Model: id, Sides: side, Price: priceOf(f)})
		}
	}
	for _, u := range unmatched {
		if _, ok := manual[ai.ModelKey(u.ID)]; !ok {
			r.Unmatched = append(r.Unmatched, u)
		}
	}
	if (len(r.Changes) == 0 && !r.ManualChanged) || o.DryRun {
		return r, nil
	}
	doc, err := json.Marshal(t)
	if err != nil {
		return r, err
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	r.Version, err = s.PublishPrices(ctx, []jennah.PriceChunk{{Pack: AIPack, Region: AIRegion, Document: doc, Models: len(t.Models)}}, r.Note, now)
	return r, err
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
