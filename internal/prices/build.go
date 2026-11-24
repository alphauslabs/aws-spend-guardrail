// Package prices builds the AI pack's price table from the AWS Price List API
// and Bedrock's model list (design D5 of add-shared-prices). The same build
// produces the table embedded in a release and the shared table published to
// Jennah.
package prices

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
)

// Model is one entry of Bedrock's foundation model list.
type Model struct {
	ID      string   `json:"modelId"`
	Name    string   `json:"modelName"`
	Outputs []string `json:"outputModalities"`
}

// Table is a built price table, in the format the AI pack reads.
type Table struct {
	Source    string `json:"source"`
	Published string `json:"published"`
	Basis     string `json:"basis"`
	// Models are the us-east-1 prices, plus each model not offered there at its
	// highest price in any region: what guards of releases before regional
	// prices read (add-regional-prices D2).
	Models map[string]config.ModelPrice `json:"models"`
	// Regions are the prices per region (add-regional-prices D1). A chunk
	// leaves it out: each chunk is one region's Models.
	Regions map[string]map[string]config.ModelPrice `json:"regions,omitempty"`
	// Tiers are a chunk's service tier prices, by model and tier; Models
	// holds the standard tier (add-tier-pricing D2). Guards of earlier
	// releases ignore them.
	Tiers map[string]map[string]config.ModelPrice `json:"tiers,omitempty"`
	// RegionTiers are the built table's tier prices per region. A chunk
	// leaves it out: each chunk is one region's Tiers.
	RegionTiers map[string]map[string]map[string]config.ModelPrice `json:"region_tiers,omitempty"`
	// Manual lists the prices set by hand when publishing, as given
	// (add-manual-price-overrides D3). Guards ignore it; Models already holds
	// the merged prices.
	Manual map[string]ManualPrice `json:"manual,omitempty"`
}

// Basis describes which rates a table holds.
const Basis = "On-demand standard (regional) input and output token rates, USD per 1,000 tokens, with flex, priority, and ultrafast rates as tier prices. Excludes batch, reserved, long-context, latency-optimized, and prompt-cache rates. Where only a global rate is listed, that rate is used."

// Source names where a table's prices come from.
const Source = "AWS Price List API, service codes AmazonBedrock and AmazonBedrockFoundationModels, every region, matched to model ids from bedrock list-foundation-models and the bedrock-mantle model list in every enabled region"

// LegacyRegion is the region whose chunk guards of releases before regional
// prices read.
const LegacyRegion = "us-east-1"

// Unmatched is a text model the Price List gave no price for.
type Unmatched struct {
	ID, Name string
}

var (
	skipUsage = []string{"batch", "reserved", "long_ctx", "cache", "latency", "tpm"}
	// TierNames are the service tiers priced apart from the standard tier,
	// as the Price List's usage types and inference types name them
	// (add-tier-pricing D1).
	TierNames  = []string{"flex", "priority", "ultrafast"}
	stripWords = map[string]bool{"instruct": true, "openai": true, "nvidia": true, "bf16": true, "vl": true, "dense": true, "it": true, "pt": true}
	parens     = regexp.MustCompile(`\(.*?\)`)
	nonAlnum   = regexp.MustCompile(`[^a-z0-9]+`)
	// usageModel is a usage type's model part after its region prefix (USE1-,
	// APN1-, EUW2-, ...). A marketplace usage type whose part starts with the
	// prefix again (USE1_input_tokens...) names no model (usageNoModel).
	usageModel   = regexp.MustCompile(`^[A-Z]+[0-9]*-(?:MP:)?(.+?)-(?:input|output)-tokens`)
	usageNoModel = regexp.MustCompile(`^[A-Z]+[0-9]*_`)
	// usageMantle and usageModality are a usage type's model part's -mantle
	// suffix and a speech or text segment right before the token direction
	// (design D2 of fix-price-builder-matches).
	usageMantle   = regexp.MustCompile(`-mantle$`)
	usageModality = regexp.MustCompile(`-(?:speech|text)$`)
	usageDir      = regexp.MustCompile(`-(input|output)-tokens`)
	idVersion     = regexp.MustCompile(`-v\d+(:\d+)?$`)
	idDate        = regexp.MustCompile(`-\d{8}$`)
	// idSnapshot is a dated snapshot's suffix, as in openai.gpt-5.4-2026-03-05
	// (add-regional-prices D5): the snapshot is priced as its base model.
	idSnapshot   = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)
	idTrailColon = regexp.MustCompile(`:\d+$`)
)

// norm reduces a model name or id to a key both lists can share: lowercase
// letters and digits, without words one list adds and the other doesn't.
func norm(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "2.0", "2"))
	s = parens.ReplaceAllString(s, "")
	var out []string
	for _, w := range strings.Fields(nonAlnum.ReplaceAllString(s, " ")) {
		if !stripWords[w] {
			out = append(out, w)
		}
	}
	return strings.Join(out, "")
}

func idKey(id string) string {
	core := id
	if i := strings.Index(core, "."); i >= 0 {
		core = core[i+1:]
	}
	core = idVersion.ReplaceAllString(core, "")
	core = idTrailColon.ReplaceAllString(core, "")
	core = idDate.ReplaceAllString(core, "")
	core = idSnapshot.ReplaceAllString(core, "")
	return norm(core)
}

type priceList struct {
	Product struct {
		Attributes map[string]string `json:"attributes"`
	} `json:"product"`
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				Unit         string            `json:"unit"`
				PricePerUnit map[string]string `json:"pricePerUnit"`
			} `json:"priceDimensions"`
		} `json:"OnDemand"`
	} `json:"terms"`
	PublicationDate string `json:"publicationDate"`
}

// rate is one Price List rate per 1,000 tokens and the usage type that lists
// it.
type rate struct {
	v     float64
	usage string
}

type rates struct{ in, out map[bool][]rate } // keyed by "is a global rate"

// RateSource names the Price List usage types that set a model's input and output
// prices in a region, so a publish can say where a large raise came from.
type RateSource struct{ In, Out string }

// Build joins Price List entries (each a JSON document, as GetProducts returns
// them) with Bedrock's model list. It prices every text model it can match,
// and lists the ones it can't.
func Build(priceItems []string, models []Model) (*Table, []Unmatched, error) {
	t, _, miss, err := build(priceItems, models)
	return t, miss, err
}

// build is Build, with the usage types that set each model's prices.
func build(priceItems []string, models []Model) (*Table, map[string]RateSource, []Unmatched, error) {
	byKey := map[string]*rates{}
	byTier := map[string]map[string]*rates{} // tier, then key
	// bags maps a word-order-insensitive key to the Price List model names
	// that produce it (design D3).
	bags := map[string][]string{}
	published := ""
	for _, raw := range priceItems {
		var p priceList
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, nil, nil, fmt.Errorf("price list entry: %w", err)
		}
		if p.PublicationDate > published {
			published = p.PublicationDate
		}
		a := p.Product.Attributes
		usage := a["usagetype"]
		lu := strings.ToLower(usage)
		input, ok := direction(a["inferenceType"], usage)
		if !ok || containsAny(lu, skipUsage) || containsAny(strings.ToLower(a["inferenceType"]), skipUsage) {
			continue
		}
		global := strings.Contains(lu, "global") || strings.Contains(strings.ToLower(a["inferenceType"]), "global")
		tier := tierOf(lu, strings.ToLower(a["inferenceType"]))
		keys := map[string]bool{}
		for _, n := range []string{a["model"], a["titanModel"]} {
			if k := norm(n); k != "" {
				keys[k] = true
			}
			// A model attribute that is a model id, such as xai.grok-4.6.
			if strings.Contains(n, ".") && !strings.Contains(n, " ") {
				if k := idKey(n); k != "" {
					keys[k] = true
				}
			}
			if b := bagKey(n); b != "" {
				bags[b] = append(bags[b], n)
			}
		}
		if m := usageModel.FindStringSubmatch(usage); m != nil && !usageNoModel.MatchString(m[1]) {
			core := m[1]
			if i := strings.Index(core, "."); i >= 0 {
				core = core[i+1:]
			}
			core = usageModality.ReplaceAllString(usageMantle.ReplaceAllString(core, ""), "")
			if k := norm(core); k != "" {
				keys[k] = true
			}
		}
		for _, t := range p.Terms.OnDemand {
			for _, d := range t.PriceDimensions {
				usd, err := strconv.ParseFloat(d.PricePerUnit["USD"], 64)
				if err != nil {
					continue
				}
				unit := strings.ToLower(d.Unit)
				var per1k float64
				switch {
				case strings.Contains(unit, "1m"):
					per1k = usd / 1000
				case strings.Contains(unit, "1k"):
					per1k = usd
				default:
					continue
				}
				into := byKey
				if tier != "" {
					if byTier[tier] == nil {
						byTier[tier] = map[string]*rates{}
					}
					into = byTier[tier]
				}
				for k := range keys {
					r := into[k]
					if r == nil {
						r = &rates{in: map[bool][]rate{}, out: map[bool][]rate{}}
						into[k] = r
					}
					if input {
						r.in[global] = append(r.in[global], rate{per1k, usage})
					} else {
						r.out[global] = append(r.out[global], rate{per1k, usage})
					}
				}
			}
		}
	}

	t := &Table{Source: Source, Published: published, Basis: Basis, Models: map[string]config.ModelPrice{}}
	sources := map[string]RateSource{}
	var miss []Unmatched
	for _, m := range models {
		if !contains(m.Outputs, "TEXT") || strings.Count(m.ID, ":") > 1 {
			continue // not a text model, or a context-length variant of one
		}
		var r *rates
		key := ""
		for _, k := range []string{norm(m.Name), idKey(m.ID)} {
			if c := byKey[k]; c != nil && len(c.in) > 0 && len(c.out) > 0 {
				r, key = c, k
				break
			}
		}
		if r == nil {
			r, key = byBag(byKey, bags, m)
		}
		if r == nil {
			miss = append(miss, Unmatched{ID: m.ID, Name: m.Name})
			continue
		}
		in, inUsage := pick(r.in)
		out, outUsage := pick(r.out)
		t.Models[m.ID] = config.ModelPrice{InputPer1K: in, OutputPer1K: out}
		sources[m.ID] = RateSource{In: inUsage, Out: outUsage}
		// A tier is priced from the rows under the same name the standard
		// price matched (add-tier-pricing D1).
		for _, tier := range TierNames {
			c := byTier[tier][key]
			if c == nil || len(c.in) == 0 || len(c.out) == 0 {
				continue
			}
			in, _ := pick(c.in)
			out, _ := pick(c.out)
			if t.Tiers == nil {
				t.Tiers = map[string]map[string]config.ModelPrice{}
			}
			if t.Tiers[m.ID] == nil {
				t.Tiers[m.ID] = map[string]config.ModelPrice{}
			}
			t.Tiers[m.ID][tier] = config.ModelPrice{InputPer1K: in, OutputPer1K: out}
		}
	}
	sort.Slice(miss, func(i, j int) bool { return miss[i].ID < miss[j].ID })
	return t, sources, miss, nil
}

// direction says whether a Price List entry is an input or output token rate:
// from inferenceType in any wording ("Input tokens", "Text Input Token",
// "Speech Understanding input token"), or, when that is empty, from the usage
// type (design D1). ok is false for anything else.
func direction(inferenceType, usage string) (input, ok bool) {
	it := strings.ToLower(inferenceType)
	switch {
	case strings.Contains(it, "input token"):
		return true, true
	case strings.Contains(it, "output token"):
		return false, true
	case it != "":
		return false, false
	}
	if m := usageDir.FindStringSubmatch(strings.ToLower(usage)); m != nil {
		return m[1] == "input", true
	}
	return false, false
}

// bagKey is a name's normalized words, sorted, so "Nova Sonic 2.0" and "Nova
// 2 Sonic" share one: "2 nova sonic".
func bagKey(name string) string {
	s := strings.ToLower(strings.ReplaceAll(name, "2.0", "2"))
	s = parens.ReplaceAllString(s, "")
	var words []string
	for _, w := range strings.Fields(nonAlnum.ReplaceAllString(s, " ")) {
		if !stripWords[w] {
			words = append(words, w)
		}
	}
	if len(words) < 2 {
		return ""
	}
	sort.Strings(words)
	return strings.Join(words, " ")
}

// byBag finds a model's rates through its name's word-order-insensitive key,
// only when exactly one Price List model produced that key, and that model's
// own key has both input and output rates (design D3).
func byBag(byKey map[string]*rates, bags map[string][]string, m Model) (*rates, string) {
	names := uniq(bags[bagKey(m.Name)])
	if len(names) != 1 {
		return nil, ""
	}
	k := norm(names[0])
	if c := byKey[k]; c != nil && len(c.in) > 0 && len(c.out) > 0 {
		return c, k
	}
	return nil, ""
}

// tierOf is the service tier a Price List row prices, from its usage type or
// inference type, or "" for the standard tier (add-tier-pricing D1).
func tierOf(usage, inferenceType string) string {
	for _, t := range TierNames {
		if strings.Contains(usage, t) || strings.Contains(inferenceType, t) {
			return t
		}
	}
	return ""
}

func uniq(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// pick prefers the regional rate and takes the highest seen, rounded to 9
// decimal places, with the usage type that lists it.
func pick(m map[bool][]rate) (float64, string) {
	v := m[false]
	if len(v) == 0 {
		v = m[true]
	}
	best := rate{}
	for _, x := range v {
		if x.v > best.v {
			best = x
		}
	}
	return math.Round(best.v*1e9) / 1e9, best.usage
}

func containsAny(s string, subs []string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortChanges(c []Change) {
	sort.Slice(c, func(i, j int) bool { return c[i].Model < c[j].Model })
}

// Built is a build across regions (add-regional-prices D1, D5).
type Built struct {
	// Table holds the prices per region in Regions, and in Models the legacy
	// view: us-east-1, plus models not offered there at their highest.
	Table *Table
	// Offered maps each model to the regions whose model lists name it.
	Offered map[string]map[string]bool
	// Unlisted are the regions whose model lists couldn't be read; they are
	// priced from the Price List for the models listed elsewhere.
	Unlisted []string
	// Sources are the usage types that set each model's prices, by region.
	Sources map[string]map[string]RateSource
}

// BuildRegions prices every model a region's model list names, in every
// region the Price List has entries for (add-regional-prices D5): items are
// the Price List entries per region, listed the models per region. A model is
// unmatched when no region prices it.
func BuildRegions(items map[string][]string, listed map[string][]Model, unlisted []string) (*Built, []Unmatched, error) {
	b := &Built{Offered: map[string]map[string]bool{}, Unlisted: append([]string(nil), unlisted...), Sources: map[string]map[string]RateSource{}}
	sort.Strings(b.Unlisted)
	var union []Model
	seen := map[string]bool{}
	regions := make([]string, 0, len(listed))
	for r := range listed {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	for _, r := range regions {
		for _, m := range listed[r] {
			if b.Offered[m.ID] == nil {
				b.Offered[m.ID] = map[string]bool{}
			}
			b.Offered[m.ID][r] = true
			if !seen[m.ID] {
				seen[m.ID] = true
				union = append(union, m)
			}
		}
	}
	t := &Table{Source: Source, Basis: Basis, Models: map[string]config.ModelPrice{}, Regions: map[string]map[string]config.ModelPrice{}}
	priced := map[string]bool{}
	missName := map[string]string{}
	for r, its := range items {
		rt, src, miss, err := build(its, union)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", r, err)
		}
		if rt.Published > t.Published {
			t.Published = rt.Published
		}
		for _, u := range miss {
			missName[u.ID] = u.Name
		}
		if len(rt.Models) == 0 {
			continue
		}
		t.Regions[r] = rt.Models
		b.Sources[r] = src
		if len(rt.Tiers) > 0 {
			if t.RegionTiers == nil {
				t.RegionTiers = map[string]map[string]map[string]config.ModelPrice{}
			}
			t.RegionTiers[r] = rt.Tiers
		}
		for id := range rt.Models {
			priced[id] = true
		}
	}
	t.Models = Legacy(t.Regions)
	var miss []Unmatched
	for id, name := range missName {
		if !priced[id] {
			miss = append(miss, Unmatched{ID: id, Name: name})
		}
	}
	sort.Slice(miss, func(i, j int) bool { return miss[i].ID < miss[j].ID })
	b.Table = t
	return b, miss, nil
}

// Legacy is the price view guards of releases before regional prices read
// (add-regional-prices D2): the us-east-1 prices, plus each model not priced
// there at its highest price in any region, input and output separately.
func Legacy(regions map[string]map[string]config.ModelPrice) map[string]config.ModelPrice {
	out := map[string]config.ModelPrice{}
	for id, p := range regions[LegacyRegion] {
		out[id] = p
	}
	for _, hi := range Highest(regions) {
		if _, ok := out[hi.ID]; !ok {
			out[hi.ID] = hi.Price
		}
	}
	return out
}

// ModelHigh is a model's highest price across regions.
type ModelHigh struct {
	ID    string
	Price config.ModelPrice
}

// Highest returns each model's highest input and output price across regions,
// sorted by model.
func Highest(regions map[string]map[string]config.ModelPrice) []ModelHigh {
	hi := map[string]config.ModelPrice{}
	for _, models := range regions {
		for id, p := range models {
			h := hi[id]
			hi[id] = config.ModelPrice{InputPer1K: max(h.InputPer1K, p.InputPer1K), OutputPer1K: max(h.OutputPer1K, p.OutputPer1K)}
		}
	}
	out := make([]ModelHigh, 0, len(hi))
	for id, p := range hi {
		out = append(out, ModelHigh{ID: id, Price: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
