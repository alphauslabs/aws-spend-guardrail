package prices

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// regionalSample is testdata/regional.json: real Price List entries for three
// regions and model lists for two (add-regional-prices task 2.3).
func regionalSample(t *testing.T) (map[string][]string, map[string][]Model) {
	t.Helper()
	raw, err := os.ReadFile("testdata/regional.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		PriceList map[string][]string `json:"price_list"`
		Models    map[string][]Model  `json:"models"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s.PriceList, s.Models
}

func TestBuildRegions(t *testing.T) {
	items, listed := regionalSample(t)
	b, miss, err := BuildRegions(items, listed, []string{"eu-west-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(miss) != 0 || strings.Join(b.Unlisted, ",") != "eu-west-2" {
		t.Fatalf("unmatched %v, unlisted %v", miss, b.Unlisted)
	}
	reg := b.Table.Regions
	in := func(region, id string) (config.ModelPrice, bool) { p, ok := reg[region][id]; return p, ok }

	// Prices differ by region.
	use1, _ := in("us-east-1", "amazon.nova-micro-v1:0")
	euw2, ok := in("eu-west-2", "amazon.nova-micro-v1:0")
	if !ok || euw2.InputPer1K <= use1.InputPer1K || euw2.OutputPer1K <= use1.OutputPer1K {
		t.Errorf("nova micro: us-east-1 %+v, eu-west-2 %+v", use1, euw2)
	}
	// Offered outside us-east-1 only, and priced in eu-west-2, whose model
	// list wasn't read, since us-west-2 lists it.
	if _, ok := in("us-east-1", "deepseek.v3-v1:0"); ok {
		t.Error("deepseek.v3-v1:0 has no us-east-1 price")
	}
	usw2, ok1 := in("us-west-2", "deepseek.v3-v1:0")
	euds, ok2 := in("eu-west-2", "deepseek.v3-v1:0")
	if !ok1 || !ok2 {
		t.Fatalf("deepseek.v3-v1:0 priced in us-west-2 %v, eu-west-2 %v", ok1, ok2)
	}
	// The legacy view prices it at its highest, side by side.
	if got, want := b.Table.Models["deepseek.v3-v1:0"], (config.ModelPrice{InputPer1K: max(usw2.InputPer1K, euds.InputPer1K), OutputPer1K: max(usw2.OutputPer1K, euds.OutputPer1K)}); got != want {
		t.Errorf("legacy deepseek %+v, want %+v", got, want)
	}
	if b.Table.Models["amazon.nova-micro-v1:0"] != use1 {
		t.Error("the legacy view keeps us-east-1 prices")
	}
	// A mantle-only model, a dated snapshot, and the moonshotai. alias.
	if _, ok := in("us-east-1", "google.gemma-4-31b"); !ok {
		t.Error("google.gemma-4-31b is priced from its mantle usage types")
	}
	snap, ok := in("us-east-1", "openai.gpt-5.4-2026-03-05")
	if !ok || snap.InputPer1K == 0 {
		t.Errorf("snapshot %+v %v", snap, ok)
	}
	alias, ok1 := in("us-east-1", "moonshotai.kimi-k2-thinking")
	kimi, ok2 := in("us-east-1", "moonshot.kimi-k2-thinking")
	if !ok1 || !ok2 || alias != kimi {
		t.Errorf("moonshotai %+v, moonshot %+v", alias, kimi)
	}
	if !b.Offered["deepseek.v3-v1:0"]["us-west-2"] || b.Offered["deepseek.v3-v1:0"]["us-east-1"] {
		t.Errorf("offered %v", b.Offered["deepseek.v3-v1:0"])
	}
}

// A builder change never lowers a price (fix-price-builder-matches): the
// regional build of the us-east-1 sample prices every model as Build did.
func TestBuildRegionsKeepsUSEast1Prices(t *testing.T) {
	items, models := sample(t)
	single, _, err := Build(items, models)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := BuildRegions(map[string][]string{"us-east-1": items}, map[string][]Model{"us-east-1": models}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, p := range single.Models {
		if got := b.Table.Regions["us-east-1"][id]; got.InputPer1K < p.InputPer1K || got.OutputPer1K < p.OutputPer1K {
			t.Errorf("%s: %+v, was %+v", id, got, p)
		}
	}
}

type fakePricing struct {
	items map[string][]string
}

func (f *fakePricing) GetAttributeValues(_ context.Context, in *pricing.GetAttributeValuesInput, _ ...func(*pricing.Options)) (*pricing.GetAttributeValuesOutput, error) {
	out := &pricing.GetAttributeValuesOutput{}
	for _, r := range []string{"us-east-1", "us-west-2", "eu-west-2", "us-gov-west-1"} {
		out.AttributeValues = append(out.AttributeValues, pricingtypes.AttributeValue{Value: aws.String(r)})
	}
	return out, nil
}

func (f *fakePricing) GetProducts(_ context.Context, in *pricing.GetProductsInput, _ ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
	if aws.ToString(in.ServiceCode) != "AmazonBedrock" {
		return &pricing.GetProductsOutput{}, nil
	}
	region := aws.ToString(in.Filters[0].Value)
	if region == "us-gov-west-1" {
		return nil, errors.New("another partition must not be read")
	}
	return &pricing.GetProductsOutput{PriceList: f.items[region]}, nil
}

type fakeModels struct{ models []Model }

func (f fakeModels) ListFoundationModels(context.Context, *bedrock.ListFoundationModelsInput, ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error) {
	out := &bedrock.ListFoundationModelsOutput{}
	for _, m := range f.models {
		s := bedrocktypes.FoundationModelSummary{ModelId: aws.String(m.ID), ModelName: aws.String(m.Name)}
		for _, o := range m.Outputs {
			s.OutputModalities = append(s.OutputModalities, bedrocktypes.ModelModality(o))
		}
		out.ModelSummaries = append(out.ModelSummaries, s)
	}
	return out, nil
}

type refusingModels struct{}

func (refusingModels) ListFoundationModels(context.Context, *bedrock.ListFoundationModelsInput, ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error) {
	return nil, errors.New("UnrecognizedClientException: the security token included in the request is invalid")
}

// Fetch reads every commercial region; one whose model list is refused (not
// enabled) is named and priced for the models listed elsewhere, and a region
// without a mantle endpoint is not a failure (add-regional-prices D5).
func TestFetchEveryRegion(t *testing.T) {
	items, listed := regionalSample(t)
	var bedrockOnly, mantle = map[string][]Model{}, map[string][]string{}
	for r, ms := range listed {
		for _, m := range ms {
			if m.Name == "" {
				mantle[r] = append(mantle[r], m.ID)
			} else {
				bedrockOnly[r] = append(bedrockOnly[r], m)
			}
		}
	}
	s := Sources{
		Pricing: &fakePricing{items: items},
		Models: func(region string) ModelsAPI {
			if region == "eu-west-2" {
				return refusingModels{}
			}
			return fakeModels{models: bedrockOnly[region]}
		},
		Mantle: func(_ context.Context, region string) ([]string, bool, error) {
			ids, ok := mantle[region]
			return ids, ok, nil // us-west-2: no mantle endpoint
		},
	}
	b, miss, err := Fetch(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(miss) != 0 || strings.Join(b.Unlisted, ",") != "eu-west-2" || len(b.Table.Regions) != 3 {
		t.Fatalf("unmatched %v, unlisted %v, regions %d", miss, b.Unlisted, len(b.Table.Regions))
	}
	if _, ok := b.Table.Regions["eu-west-2"]["deepseek.v3-v1:0"]; !ok {
		t.Error("eu-west-2 is priced for models listed elsewhere")
	}
	if _, ok := b.Table.Regions["us-east-1"]["google.gemma-4-31b"]; !ok {
		t.Error("mantle models are priced")
	}
}
