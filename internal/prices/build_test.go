package prices

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
)

func sample(t *testing.T) ([]string, []Model) {
	t.Helper()
	raw, err := os.ReadFile("testdata/sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		PriceList []string `json:"price_list"`
		Models    []Model  `json:"models"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s.PriceList, s.Models
}

// The recorded sample covers the naming mismatches between the Price List and
// Bedrock's model list; each priced model must match today's shipped table.
func TestBuildMatchesTheShippedTable(t *testing.T) {
	items, models := sample(t)
	tab, miss, err := Build(items, models)
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := ai.Prices()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"amazon.nova-micro-v1:0",                    // same name
		"amazon.nova-2-lite-v1:0",                   // "Nova 2.0 Lite" in the Price List
		"anthropic.claude-sonnet-4-5-20250929-v1:0", // Marketplace, per 1M tokens, regional and global rates
		"anthropic.claude-haiku-5-5",
		"meta.llama3-3-70b-instruct-v1:0", // "Instruct" only in the model list
		"google.gemma-3-27b-it",           // "IT" and "PT"
		"mistral.ministral-3-3b-instruct", // matched through the usage type
		"openai.gpt-5.5",                  // "OpenAI GPT-5.5"
		"deepseek.v3.2",
		"mistral.mistral-large-2402-v1:0", // "(24.02)" only in the model list
	} {
		got, ok := tab.Models[id]
		want := shipped.Models[id]
		if !ok || got != want {
			t.Errorf("%s: built %+v (found %v), shipped %+v", id, got, ok, want)
		}
	}
	if _, ok := tab.Models["amazon.nova-micro-v1:0:128k"]; ok {
		t.Error("a context-length variant must not get its own entry")
	}
	// Not per token (Rerank, Pegasus), or not in the sample (Grok 4.7).
	var missed []string
	for _, m := range miss {
		missed = append(missed, m.ID)
	}
	if strings.Join(missed, ",") != "cohere.rerank-v3-5:0,twelvelabs.pegasus-1-2-v1:0,xai.grok-4.7" {
		t.Errorf("unmatched: %v", missed)
	}
	if tab.Published == "" || tab.Source != Source || tab.Basis != Basis {
		t.Errorf("metadata: %q %q", tab.Published, tab.Source)
	}
}

// Models AWS prices but labels in ways the builder used to miss
// (fix-price-builder-matches).
func TestBuildMatchesHowAWSLabelsModels(t *testing.T) {
	items, models := sample(t)
	tab, _, err := Build(items, models)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]config.ModelPrice{
		// Listed by id, with no inferenceType: the regional rate, not the global one (D1, D2).
		"xai.grok-4.6": {InputPer1K: 0.0022, OutputPer1K: 0.0066},
		// Standard rows mislabeled "Voxtral-Mini-24B"; the -mantle rows carry the id (D2).
		"mistral.voxtral-small-24b-2507": {InputPer1K: 0.0001, OutputPer1K: 0.0003},
		// Speech and text token rates: the higher per side (D1, D4).
		"amazon.nova-2-5-sonic": {InputPer1K: 0.003, OutputPer1K: 0.012},
		// "Nova Sonic 2.0" in the Price List, "Nova 2 Sonic" in Bedrock (D3).
		"amazon.nova-2-sonic-v1:0": {InputPer1K: 0.003, OutputPer1K: 0.012},
	} {
		if got := tab.Models[id]; got != want {
			t.Errorf("%s: %+v, want %+v", id, got, want)
		}
	}
	shipped, _ := ai.Prices()
	if got, want := tab.Models["mistral.voxtral-mini-3b-2507"], shipped.Models["mistral.voxtral-mini-3b-2507"]; got != want {
		t.Errorf("Voxtral Mini changed: %+v, shipped %+v", got, want)
	}
}

// A builder change never lowers a price: every model the shipped table
// prices that the sample also prices is the same or higher (D5).
func TestBuildNeverLowersAShippedPrice(t *testing.T) {
	items, models := sample(t)
	tab, _, err := Build(items, models)
	if err != nil {
		t.Fatal(err)
	}
	shipped, _ := ai.Prices()
	n := 0
	for id, got := range tab.Models {
		was, ok := shipped.Models[id]
		if !ok {
			continue
		}
		n++
		if got.InputPer1K < was.InputPer1K || got.OutputPer1K < was.OutputPer1K {
			t.Errorf("%s lowered: %+v, shipped %+v", id, got, was)
		}
	}
	if n < 10 {
		t.Fatalf("only %d shipped models in the sample", n)
	}
}

// The ultrafast tier is a premium rate like priority, not the standard one:
// GPT-6 Astra's standard rows are $11 and $55 per 1M tokens, its ultrafast
// rows $66 and $330, as the Price List listed them in Nov 2026.
func TestBuildSkipsUltrafastRates(t *testing.T) {
	row := func(usage, inference, usd string) string {
		return `{"product":{"attributes":{"regionCode":"us-east-1","inferenceType":"` + inference + `","feature":"On-demand Inference","provider":"OpenAI","usagetype":"USE1-MP:USE1_` + usage + `-Units","model":"OpenAI GPT-6 Astra","operation":""}},"terms":{"OnDemand":{"t":{"priceDimensions":{"d":{"unit":"1M tokens","pricePerUnit":{"USD":"` + usd + `"}}}}}},"publicationDate":"2026-11-23T00:00:00Z"}`
	}
	items := []string{
		row("input_tokens_standard", "Input tokens", "11.0000000000"),
		row("output_tokens_standard", "Output tokens", "55.0000000000"),
		row("input_tokens_ultrafast", "Input tokens ultrafast", "66.0000000000"),
		row("output_tokens_ultrafast", "Output tokens ultrafast", "330.0000000000"),
	}
	tab, _, err := Build(items, []Model{{ID: "openai.gpt-6-astra", Name: "GPT-6 Astra", Outputs: []string{"TEXT"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := config.ModelPrice{InputPer1K: 0.011, OutputPer1K: 0.055}
	if got := tab.Models["openai.gpt-6-astra"]; got != want {
		t.Errorf("built %+v, want %+v", got, want)
	}
	_, src, _, _ := build(items, []Model{{ID: "openai.gpt-6-astra", Name: "GPT-6 Astra", Outputs: []string{"TEXT"}}})
	if got := src["openai.gpt-6-astra"]; got != (RateSource{In: "USE1-MP:USE1_input_tokens_standard-Units", Out: "USE1-MP:USE1_output_tokens_standard-Units"}) {
		t.Errorf("sources %+v", got)
	}
}

func TestDirection(t *testing.T) {
	for _, tc := range []struct {
		it, usage string
		input, ok bool
	}{
		{"Input tokens", "USE1-x-input-tokens", true, true},
		{"Text Input Token", "", true, true},
		{"Speech Understanding output token", "", false, true},
		{"", "USE1-xai.grok-4.6-mantle-input-tokens-standard", true, true},
		{"", "USE1-xai.grok-4.6-mantle-output-tokens-global-standard", false, true},
		{"Input Video Token Count", "USE1-MP:USE1_inputVideoSecond-Units", false, false},
		{"", "USE1-MP:USE1_search_units-Units", false, false},
	} {
		if in, ok := direction(tc.it, tc.usage); in != tc.input || ok != tc.ok {
			t.Errorf("direction(%q, %q) = %v %v", tc.it, tc.usage, in, ok)
		}
	}
}

// The word-order fallback matches only when exactly one Price List model has
// the key, so it can't cross-wire two models (D3).
func TestBagKeyFallbackOnlyWhenUnique(t *testing.T) {
	row := func(model, usage, kind string, usd string) string {
		return `{"product":{"attributes":{"model":"` + model + `","usagetype":"` + usage + `","inferenceType":"` + kind +
			`"}},"terms":{"OnDemand":{"t":{"priceDimensions":{"d":{"unit":"1K tokens","pricePerUnit":{"USD":"` + usd + `"}}}}}}}`
	}
	items := []string{
		row("Acme Turbo 3", "USE1-AcmeTurbo3-input-tokens", "Input tokens", "0.001"),
		row("Acme Turbo 3", "USE1-AcmeTurbo3-output-tokens", "Output tokens", "0.002"),
		row("Turbo Acme 3", "USE1-TurboAcme3-input-tokens", "Input tokens", "0.005"),
		row("Turbo Acme 3", "USE1-TurboAcme3-output-tokens", "Output tokens", "0.006"),
		row("Zeta Max 2.0", "USE1-ZetaMax2-input-tokens", "Input tokens", "0.003"),
		row("Zeta Max 2.0", "USE1-ZetaMax2-output-tokens", "Output tokens", "0.004"),
	}
	models := []Model{
		{ID: "acme.3-turbo-v1:0", Name: "3 Turbo Acme", Outputs: []string{"TEXT"}}, // two Price List models share its bag key
		{ID: "zeta.max-v2:0", Name: "Max Zeta 2", Outputs: []string{"TEXT"}},       // one does
	}
	tab, miss, err := Build(items, models)
	if err != nil {
		t.Fatal(err)
	}
	if got := tab.Models["zeta.max-v2:0"]; got != (config.ModelPrice{InputPer1K: 0.003, OutputPer1K: 0.004}) {
		t.Errorf("unique bag key: %+v", got)
	}
	if _, ok := tab.Models["acme.3-turbo-v1:0"]; ok || len(miss) != 1 || miss[0].ID != "acme.3-turbo-v1:0" {
		t.Errorf("an ambiguous bag key must match nothing: %+v %+v", tab.Models, miss)
	}
}
