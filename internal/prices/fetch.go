package prices

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// PricingAPI is the part of the Price List API the builder uses.
type PricingAPI interface {
	GetProducts(context.Context, *pricing.GetProductsInput, ...func(*pricing.Options)) (*pricing.GetProductsOutput, error)
	GetAttributeValues(context.Context, *pricing.GetAttributeValuesInput, ...func(*pricing.Options)) (*pricing.GetAttributeValuesOutput, error)
}

// ModelsAPI lists Bedrock's foundation models.
type ModelsAPI interface {
	ListFoundationModels(context.Context, *bedrock.ListFoundationModelsInput, ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error)
}

// Sources are where a regional build reads from (add-regional-prices D5).
type Sources struct {
	// Pricing is the Price List API (one global endpoint).
	Pricing PricingAPI
	// Models is a Bedrock client for a region.
	Models func(region string) ModelsAPI
	// Mantle lists the bedrock-mantle endpoint's model ids in a region; ok is
	// false when the region has no mantle endpoint.
	Mantle func(ctx context.Context, region string) (ids []string, ok bool, err error)
}

// Concurrency of a regional build (add-regional-prices D6): the Price List API
// throttles beyond two regions at a time; the model lists don't.
const (
	priceListConcurrency = 2
	modelListConcurrency = 8
	// modelListTimeout bounds one region's model lists: an endpoint in a
	// region the account hasn't enabled can hang instead of refusing
	// (me-central-1 on 2026-11-17).
	modelListTimeout = 15 * time.Second
)

// otherPartition is a region code outside the commercial partition.
var otherPartition = regexp.MustCompile(`^(us-gov-|eusc-|cn-|us-iso)`)

// Fetch reads the Price List in every region and the model lists of every
// region that answers, and builds the regional table (add-regional-prices D5).
func Fetch(ctx context.Context, s Sources) (*Built, []Unmatched, error) {
	regions, err := priceListRegions(ctx, s.Pricing)
	if err != nil {
		return nil, nil, err
	}
	items := map[string][]string{}
	var mu sync.Mutex
	err = forEach(ctx, regions, priceListConcurrency, func(ctx context.Context, r string) error {
		var its []string
		for _, code := range []string{"AmazonBedrock", "AmazonBedrockFoundationModels"} {
			in := &pricing.GetProductsInput{
				ServiceCode: aws.String(code),
				Filters: []pricingtypes.Filter{
					{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("regionCode"), Value: aws.String(r)},
				},
			}
			for {
				out, err := s.Pricing.GetProducts(ctx, in)
				if err != nil {
					return fmt.Errorf("price list %s in %s: %w", code, r, err)
				}
				its = append(its, out.PriceList...)
				if out.NextToken == nil {
					break
				}
				in.NextToken = out.NextToken
			}
		}
		mu.Lock()
		items[r] = its
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Model lists are tried in every region; one the account hasn't enabled
	// refuses, and is priced for the models listed elsewhere.
	listed := map[string][]Model{}
	var unlisted []string
	_ = forEach(ctx, regions, modelListConcurrency, func(ctx context.Context, r string) error {
		ctx, cancel := context.WithTimeout(ctx, modelListTimeout)
		defer cancel()
		models, err := listModels(ctx, s, r)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			unlisted = append(unlisted, r)
			return nil
		}
		listed[r] = models
		return nil
	})
	if len(listed) == 0 {
		return nil, nil, fmt.Errorf("no region's model list could be read (tried %s)", strings.Join(regions, ", "))
	}
	return BuildRegions(items, listed, unlisted)
}

// listModels reads one region's Bedrock and mantle model lists.
func listModels(ctx context.Context, s Sources, region string) ([]Model, error) {
	out, err := s.Models(region).ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, fmt.Errorf("list foundation models in %s: %w", region, err)
	}
	var models []Model
	for _, m := range out.ModelSummaries {
		mod := Model{ID: aws.ToString(m.ModelId), Name: aws.ToString(m.ModelName)}
		for _, o := range m.OutputModalities {
			mod.Outputs = append(mod.Outputs, string(o))
		}
		models = append(models, mod)
	}
	if s.Mantle == nil {
		return models, nil
	}
	ids, ok, err := s.Mantle(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("list mantle models in %s: %w", region, err)
	}
	if ok {
		for _, id := range ids {
			// The mantle endpoint serves text inference only.
			models = append(models, Model{ID: id, Outputs: []string{"TEXT"}})
		}
	}
	return models, nil
}

// priceListRegions lists the commercial regions the Price List has Bedrock
// entries for.
func priceListRegions(ctx context.Context, p PricingAPI) ([]string, error) {
	set := map[string]bool{}
	for _, code := range []string{"AmazonBedrock", "AmazonBedrockFoundationModels"} {
		in := &pricing.GetAttributeValuesInput{ServiceCode: aws.String(code), AttributeName: aws.String("regionCode")}
		for {
			out, err := p.GetAttributeValues(ctx, in)
			if err != nil {
				return nil, fmt.Errorf("price list regions of %s: %w", code, err)
			}
			for _, v := range out.AttributeValues {
				if r := aws.ToString(v.Value); r != "" && !otherPartition.MatchString(r) {
					set[r] = true
				}
			}
			if out.NextToken == nil {
				break
			}
			in.NextToken = out.NextToken
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out, nil
}

// forEach runs f for each region, n at a time, and returns the first error.
func forEach(ctx context.Context, regions []string, n int, f func(context.Context, string) error) error {
	sem := make(chan struct{}, n)
	errs := make(chan error, len(regions))
	var wg sync.WaitGroup
	for _, r := range regions {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			errs <- f(ctx, r)
		}(r)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// Change is one model's difference between two tables. Merged across
// regions, it shows the region with the largest change, and counts the
// regions with a change of that kind (add-regional-prices D8).
type Change struct {
	Model    string
	Kind     string // added, raised, lowered, changed, removed
	From, To string
	Region   string // the region shown, when merged across regions
	Regions  int    // the regions with this kind of change, when merged
	// Factor is how many times the previous price a raise is, on its larger
	// side, set only when it is at least JumpFactor; Usage names the Price
	// List usage types that set the new price, when the build set it.
	Factor float64
	Usage  RateSource

	from, to PriceOf
}

// JumpFactor is the raise, as a multiple of the previous price, that a
// publish points out: AWS rarely raises a price this much, and a new premium
// tier the build doesn't know (as ultrafast was) looks just like it.
const JumpFactor = 2.0

// Jump describes a raise of JumpFactor or more, or is empty.
func (c Change) Jump() string {
	if c.Factor == 0 {
		return ""
	}
	s := fmt.Sprintf("%.1f times the previous price", c.Factor)
	switch {
	case c.Usage.In != "" && c.Usage.In == c.Usage.Out:
		s += ", from AWS usage type " + c.Usage.In
	case c.Usage.In != "" || c.Usage.Out != "":
		s += ", from AWS usage types " + c.Usage.In + " (input) and " + c.Usage.Out + " (output)"
	}
	return s + "; a raise this large is often a new premium tier the build doesn't know, not a price change"
}

// factor is how many times the previous price a raise is, on its larger side.
func (c Change) factor() float64 {
	f := 0.0
	if c.from.In > 0 {
		f = c.to.In / c.from.In
	}
	if c.from.Out > 0 && c.to.Out/c.from.Out > f {
		f = c.to.Out / c.from.Out
	}
	return f
}

// Diff lists the differences from old to new, sorted by model.
func Diff(old, new map[string]PriceOf) []Change {
	var out []Change
	add := func(id, kind string, o, n PriceOf, hasOld, hasNew bool) {
		c := Change{Model: id, Kind: kind, from: o, to: n}
		if hasOld {
			c.From = o.String()
		}
		if hasNew {
			c.To = n.String()
		}
		out = append(out, c)
	}
	for id, n := range new {
		o, ok := old[id]
		switch {
		case !ok:
			add(id, "added", PriceOf{}, n, false, true)
		case n.In > o.In || n.Out > o.Out:
			if n.In < o.In || n.Out < o.Out {
				add(id, "changed", o, n, true, true)
			} else {
				add(id, "raised", o, n, true, true)
			}
		case n.In < o.In || n.Out < o.Out:
			add(id, "lowered", o, n, true, true)
		}
	}
	for id, o := range old {
		if _, ok := new[id]; !ok {
			add(id, "removed", o, PriceOf{}, true, false)
		}
	}
	sortChanges(out)
	return out
}

// MergeChanges merges per-region changes into one per model and kind, showing
// the region with the largest change (add-regional-prices D8).
func MergeChanges(perRegion map[string][]Change) []Change {
	type key struct{ model, kind string }
	merged := map[key]*Change{}
	size := func(c Change) float64 {
		d := func(a, b float64) float64 {
			if a > b {
				return a - b
			}
			return b - a
		}
		return d(c.to.In, c.from.In) + d(c.to.Out, c.from.Out)
	}
	regions := make([]string, 0, len(perRegion))
	for r := range perRegion {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	for _, r := range regions {
		for _, c := range perRegion[r] {
			k := key{c.Model, c.Kind}
			m := merged[k]
			if m == nil {
				c.Region, c.Regions = r, 1
				merged[k] = &c
				continue
			}
			m.Regions++
			if size(c) > size(*m) {
				n := c
				n.Region, n.Regions = r, m.Regions
				merged[k] = &n
			}
		}
	}
	out := make([]Change, 0, len(merged))
	for _, c := range merged {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// Where says where a merged change applies: "in eu-west-2" or "in 12
// regions, largest in eu-west-2"; empty for an unmerged change.
func (c Change) Where() string {
	switch {
	case c.Regions > 1:
		return fmt.Sprintf("in %d regions, largest in %s", c.Regions, c.Region)
	case c.Region != "":
		return "in " + c.Region
	}
	return ""
}

// PriceOf is a model's input and output price per 1,000 tokens.
type PriceOf struct{ In, Out float64 }

func (p PriceOf) String() string {
	return fmt.Sprintf("in %s / out %s per 1k", config.Decimal(p.In), config.Decimal(p.Out))
}

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
