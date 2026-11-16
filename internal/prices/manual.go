package prices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
)

// ManualPrice is a model's price set by hand when publishing
// (add-manual-price-overrides D3): the operator's price as given, and the
// version that set it.
type ManualPrice struct {
	InputPer1K   float64 `json:"input_per_1k"`
	OutputPer1K  float64 `json:"output_per_1k"`
	SetInVersion int64   `json:"set_in_version"`
}

// Price returns the manual price without its version.
func (m ManualPrice) Price() config.ModelPrice {
	return config.ModelPrice{InputPer1K: m.InputPer1K, OutputPer1K: m.OutputPer1K}
}

// ManualNote is the line added to Basis when a table has manual prices.
const ManualNote = " Some prices were set by hand; see manual."

// ReadManual reads and validates a manual price file (design D1):
// {"models": {"<model id>": {"input_per_1k": N, "output_per_1k": N}}}, no
// other fields, both prices above zero, ids normalized like the AI pack's, and
// no model twice after normalizing.
func ReadManual(raw []byte) (map[string]config.ModelPrice, error) {
	var f struct {
		Models map[string]config.ModelPrice `json:"models"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("manual prices: %w", err)
	}
	if dec.More() {
		return nil, errors.New("manual prices: extra data after the JSON object")
	}
	if len(f.Models) == 0 {
		return nil, errors.New(`manual prices: no models; the file must be {"models": {"<model id>": {"input_per_1k": N, "output_per_1k": N}}}`)
	}
	ids := make([]string, 0, len(f.Models))
	for id := range f.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := map[string]config.ModelPrice{}
	from := map[string]string{}
	var probs []string
	for _, id := range ids {
		p := f.Models[id]
		key := ai.ModelKey(strings.TrimSpace(id))
		if key == "" {
			probs = append(probs, fmt.Sprintf("%q is not a model id", id))
			continue
		}
		if p.InputPer1K <= 0 {
			probs = append(probs, fmt.Sprintf("%s: input_per_1k must be above zero", id))
		}
		if p.OutputPer1K <= 0 {
			probs = append(probs, fmt.Sprintf("%s: output_per_1k must be above zero", id))
		}
		if prev, dup := from[key]; dup {
			probs = append(probs, fmt.Sprintf("%s and %s are the same model (%s)", prev, id, key))
			continue
		}
		from[key] = id
		out[key] = p
	}
	if len(probs) > 0 {
		return nil, fmt.Errorf("manual prices: %s", strings.Join(probs, "; "))
	}
	return out, nil
}

// MergeManual publishes a built table with manual prices (design D2): a model
// the build priced gets the higher of the two prices for input and output
// separately, and one it didn't is added. It returns a new table recording
// the manual prices; t is unchanged.
func MergeManual(t *Table, manual map[string]ManualPrice) *Table {
	out := *t
	out.Models = make(map[string]config.ModelPrice, len(t.Models)+len(manual))
	for id, p := range t.Models {
		out.Models[id] = p
	}
	out.Basis = strings.TrimSuffix(t.Basis, ManualNote)
	out.Manual = nil
	if len(manual) == 0 {
		return &out
	}
	out.Manual = make(map[string]ManualPrice, len(manual))
	for id, m := range manual {
		out.Manual[id] = m
		b, ok := out.Models[id]
		if !ok {
			out.Models[id] = m.Price()
			continue
		}
		out.Models[id] = config.ModelPrice{InputPer1K: max(b.InputPer1K, m.InputPer1K), OutputPer1K: max(b.OutputPer1K, m.OutputPer1K)}
	}
	out.Basis += ManualNote
	return &out
}
