package prices

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// PricingAPI is the part of the Price List API the builder uses.
type PricingAPI interface {
	GetProducts(context.Context, *pricing.GetProductsInput, ...func(*pricing.Options)) (*pricing.GetProductsOutput, error)
}

// ModelsAPI lists Bedrock's foundation models.
type ModelsAPI interface {
	ListFoundationModels(context.Context, *bedrock.ListFoundationModelsInput, ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error)
}

// Fetch reads Bedrock's on-demand prices in us-east-1 from the Price List API
// and its model list, and builds the table.
func Fetch(ctx context.Context, p PricingAPI, m ModelsAPI) (*Table, []Unmatched, error) {
	var items []string
	for _, code := range []string{"AmazonBedrock", "AmazonBedrockFoundationModels"} {
		in := &pricing.GetProductsInput{
			ServiceCode: aws.String(code),
			Filters: []pricingtypes.Filter{
				{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("regionCode"), Value: aws.String("us-east-1")},
			},
		}
		for {
			out, err := p.GetProducts(ctx, in)
			if err != nil {
				return nil, nil, fmt.Errorf("price list %s: %w", code, err)
			}
			items = append(items, out.PriceList...)
			if out.NextToken == nil {
				break
			}
			in.NextToken = out.NextToken
		}
	}
	out, err := m.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, nil, fmt.Errorf("list foundation models: %w", err)
	}
	var models []Model
	for _, s := range out.ModelSummaries {
		mod := Model{ID: aws.ToString(s.ModelId), Name: aws.ToString(s.ModelName)}
		for _, o := range s.OutputModalities {
			mod.Outputs = append(mod.Outputs, string(o))
		}
		models = append(models, mod)
	}
	return Build(items, models)
}

// Change is one model's difference between two tables.
type Change struct {
	Model    string
	Kind     string // added, raised, lowered, removed
	From, To string
}

// Diff lists the differences from old to new, sorted by model.
func Diff(old, new map[string]PriceOf) []Change {
	var out []Change
	for id, n := range new {
		o, ok := old[id]
		switch {
		case !ok:
			out = append(out, Change{Model: id, Kind: "added", To: n.String()})
		case n.In > o.In || n.Out > o.Out:
			if n.In < o.In || n.Out < o.Out {
				out = append(out, Change{Model: id, Kind: "changed", From: o.String(), To: n.String()})
			} else {
				out = append(out, Change{Model: id, Kind: "raised", From: o.String(), To: n.String()})
			}
		case n.In < o.In || n.Out < o.Out:
			out = append(out, Change{Model: id, Kind: "lowered", From: o.String(), To: n.String()})
		}
	}
	for id, o := range old {
		if _, ok := new[id]; !ok {
			out = append(out, Change{Model: id, Kind: "removed", From: o.String()})
		}
	}
	sortChanges(out)
	return out
}

// PriceOf is a model's input and output price per 1,000 tokens.
type PriceOf struct{ In, Out float64 }

func (p PriceOf) String() string { return fmt.Sprintf("in %g / out %g per 1k", p.In, p.Out) }

// Of converts a table's models for Diff.
func Of(t *Table) map[string]PriceOf {
	out := map[string]PriceOf{}
	if t == nil {
		return out
	}
	for id, p := range t.Models {
		out[id] = PriceOf{In: p.InputPer1K, Out: p.OutputPer1K}
	}
	return out
}
