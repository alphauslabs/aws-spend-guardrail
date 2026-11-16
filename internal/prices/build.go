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
	Source    string                       `json:"source"`
	Published string                       `json:"published"`
	Basis     string                       `json:"basis"`
	Models    map[string]config.ModelPrice `json:"models"`
	// Manual lists the prices set by hand when publishing, as given
	// (add-manual-price-overrides D3). Guards ignore it; Models already holds
	// the merged prices.
	Manual map[string]ManualPrice `json:"manual,omitempty"`
}

// Basis describes which rates a table holds.
const Basis = "On-demand standard (regional) input and output token rates, USD per 1,000 tokens. Excludes batch, reserved, flex, priority, long-context, latency-optimized, and prompt-cache rates. Where only a global rate is listed, that rate is used."

// Source names where a table's prices come from.
const Source = "AWS Price List API, service codes AmazonBedrock and AmazonBedrockFoundationModels, region us-east-1, matched to model ids from bedrock list-foundation-models"

// Unmatched is a text model the Price List gave no price for.
type Unmatched struct {
	ID, Name string
}

var (
	skipUsage  = []string{"batch", "reserved", "long_ctx", "flex", "priority", "cache", "latency", "tpm"}
	stripWords = map[string]bool{"instruct": true, "openai": true, "nvidia": true, "bf16": true, "vl": true, "dense": true, "it": true, "pt": true}
	parens     = regexp.MustCompile(`\(.*?\)`)
	nonAlnum   = regexp.MustCompile(`[^a-z0-9]+`)
	usageModel = regexp.MustCompile(`^USE1-(?:MP:)?(.+?)-(?:input|output)-tokens`)
	// usageMantle and usageModality are a usage type's model part's -mantle
	// suffix and a speech or text segment right before the token direction
	// (design D2 of fix-price-builder-matches).
	usageMantle   = regexp.MustCompile(`-mantle$`)
	usageModality = regexp.MustCompile(`-(?:speech|text)$`)
	usageDir      = regexp.MustCompile(`-(input|output)-tokens`)
	idVersion     = regexp.MustCompile(`-v\d+(:\d+)?$`)
	idDate        = regexp.MustCompile(`-\d{8}$`)
	idTrailColon  = regexp.MustCompile(`:\d+$`)
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

type rates struct{ in, out map[bool][]float64 } // keyed by "is a global rate"

// Build joins Price List entries (each a JSON document, as GetProducts returns
// them) with Bedrock's model list. It prices every text model it can match,
// and lists the ones it can't.
func Build(priceItems []string, models []Model) (*Table, []Unmatched, error) {
	byKey := map[string]*rates{}
	// bags maps a word-order-insensitive key to the Price List model names
	// that produce it (design D3).
	bags := map[string][]string{}
	published := ""
	for _, raw := range priceItems {
		var p priceList
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, nil, fmt.Errorf("price list entry: %w", err)
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
		if m := usageModel.FindStringSubmatch(usage); m != nil && !strings.HasPrefix(m[1], "USE1_") {
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
				for k := range keys {
					r := byKey[k]
					if r == nil {
						r = &rates{in: map[bool][]float64{}, out: map[bool][]float64{}}
						byKey[k] = r
					}
					if input {
						r.in[global] = append(r.in[global], per1k)
					} else {
						r.out[global] = append(r.out[global], per1k)
					}
				}
			}
		}
	}

	t := &Table{Source: Source, Published: published, Basis: Basis, Models: map[string]config.ModelPrice{}}
	var miss []Unmatched
	for _, m := range models {
		if !contains(m.Outputs, "TEXT") || strings.Count(m.ID, ":") > 1 {
			continue // not a text model, or a context-length variant of one
		}
		var r *rates
		for _, k := range []string{norm(m.Name), idKey(m.ID)} {
			if c := byKey[k]; c != nil && len(c.in) > 0 && len(c.out) > 0 {
				r = c
				break
			}
		}
		if r == nil {
			r = byBag(byKey, bags, m)
		}
		if r == nil {
			miss = append(miss, Unmatched{ID: m.ID, Name: m.Name})
			continue
		}
		t.Models[m.ID] = config.ModelPrice{InputPer1K: pick(r.in), OutputPer1K: pick(r.out)}
	}
	sort.Slice(miss, func(i, j int) bool { return miss[i].ID < miss[j].ID })
	return t, miss, nil
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
func byBag(byKey map[string]*rates, bags map[string][]string, m Model) *rates {
	names := uniq(bags[bagKey(m.Name)])
	if len(names) != 1 {
		return nil
	}
	if c := byKey[norm(names[0])]; c != nil && len(c.in) > 0 && len(c.out) > 0 {
		return c
	}
	return nil
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
// decimal places.
func pick(m map[bool][]float64) float64 {
	v := m[false]
	if len(v) == 0 {
		v = m[true]
	}
	best := 0.0
	for _, x := range v {
		best = math.Max(best, x)
	}
	return math.Round(best*1e9) / 1e9
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
