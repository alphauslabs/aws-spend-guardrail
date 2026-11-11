package prices

import (
	"encoding/json"
	"os"
	"testing"

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
	if len(miss) != 1 || miss[0].ID != "xai.grok-4.7" {
		t.Errorf("unmatched: %+v", miss)
	}
	if tab.Published == "" || tab.Source != Source || tab.Basis != Basis {
		t.Errorf("metadata: %q %q", tab.Published, tab.Source)
	}
}
