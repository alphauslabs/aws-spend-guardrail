package ai

import (
	"bytes"
	"compress/gzip"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
)

var (
	ossStd       = config.ModelPrice{InputPer1K: 0.00007, OutputPer1K: 0.0003}
	ossPriority  = config.ModelPrice{InputPer1K: 0.0001225, OutputPer1K: 0.000525}
	ossFlex      = config.ModelPrice{InputPer1K: 0.000035, OutputPer1K: 0.00015}
	astraStd     = config.ModelPrice{InputPer1K: 0.011, OutputPer1K: 0.055}
	astraUltra   = config.ModelPrice{InputPer1K: 0.066, OutputPer1K: 0.33}
	tieredRegion = "us-east-1"
)

// tieredTable prices gpt-oss-20b with Priority and Flex tiers and GPT-6
// Astra with an Ultrafast tier, as the Price List did in Nov 2026.
func tieredTable() *Table {
	return &Table{
		Regions: map[string]map[string]config.ModelPrice{tieredRegion: {
			"openai.gpt-oss-20b-1:0": ossStd, "openai.gpt-oss-20b": ossStd, "openai.gpt-6-astra": astraStd,
			"amazon.nova-micro-v1:0": {InputPer1K: 0.000035, OutputPer1K: 0.00014},
		}},
		RegionTiers: map[string]map[string]map[string]config.ModelPrice{tieredRegion: {
			"openai.gpt-oss-20b-1:0": {"priority": ossPriority, "flex": ossFlex},
			"openai.gpt-oss-20b":     {"priority": ossPriority, "flex": ossFlex},
			"openai.gpt-6-astra":     {"ultrafast": astraUltra},
		}},
	}
}

func cost(p config.ModelPrice, in, out int64) float64 {
	return float64(in)/1000*p.InputPer1K + float64(out)/1000*p.OutputPer1K
}

func tiered(name, model, tier string, in, out int64) activity.Record {
	r := call(name, model, in, out)
	r.ServiceTier = tier
	return r
}

func priced(t *testing.T, p *Pack, r activity.Record) *activity.Usage {
	t.Helper()
	u, h := p.Evaluate(r)
	if h != nil || u == nil {
		t.Fatalf("%s %s %q: usage %+v, hit %+v", r.EventName, r.ModelID, r.ServiceTier, u, h)
	}
	return u
}

// Each call is priced at the tier it used (add-tier-pricing ai-pack spec).
func TestCallsPricedAtTheirTier(t *testing.T) {
	p := New(tieredTable(), nil)
	for _, tc := range []struct {
		name string
		r    activity.Record
		want config.ModelPrice
	}{
		{"priority", tiered("Converse", "openai.gpt-oss-20b-1:0", "priority", 69, 5), ossPriority},
		{"no tier", tiered("Converse", "openai.gpt-oss-20b-1:0", "", 69, 5), ossStd},
		{"flex", tiered("Converse", "openai.gpt-oss-20b-1:0", "flex", 69, 5), ossFlex},
		{"InvokeModel priority", tiered("InvokeModel", "openai.gpt-oss-20b-1:0", "priority", 69, 5), ossPriority},
		{"no tiers at all", tiered("Converse", "amazon.nova-micro-v1:0", "", 1000, 1000), config.ModelPrice{InputPer1K: 0.000035, OutputPer1K: 0.00014}},
	} {
		u := priced(t, p, tc.r)
		if math.Abs(u.USD-cost(tc.want, tc.r.InputTokens, tc.r.OutputTokens)) > 1e-15 || u.UnpricedTier != "" {
			t.Errorf("%s: $%v, want %+v", tc.name, u.USD, tc.want)
		}
	}
}

// A tier the tables don't price for the model is priced at its highest tier
// and flagged for a one-time report.
func TestUnknownTierPricedHigh(t *testing.T) {
	p := New(tieredTable(), nil)
	u := priced(t, p, tiered("Converse", "openai.gpt-oss-20b-1:0", "turbo", 1000, 1000))
	if math.Abs(u.USD-cost(ossPriority, 1000, 1000)) > 1e-15 || u.UnpricedTier != "turbo" {
		t.Fatalf("turbo: %+v", u)
	}
	// A model with no tier prices: its standard price.
	u = priced(t, p, tiered("Converse", "amazon.nova-micro-v1:0", "turbo", 1000, 1000))
	if math.Abs(u.USD-cost(config.ModelPrice{InputPer1K: 0.000035, OutputPer1K: 0.00014}, 1000, 1000)) > 1e-15 || u.UnpricedTier != "turbo" {
		t.Fatalf("turbo without tiers: %+v", u)
	}
}

// An ai.prices entry is the standard price; another tier is priced at the
// higher of the entry and the tables' tier price.
func TestOverrideSetsTheStandardTier(t *testing.T) {
	cfg := &config.ResolvedAI{Prices: map[string]config.ModelPrice{"openai.gpt-oss-20b-1:0": {InputPer1K: 0.0001, OutputPer1K: 0.0002}}}
	p := New(tieredTable(), cfg)
	u := priced(t, p, tiered("Converse", "openai.gpt-oss-20b-1:0", "", 1000, 1000))
	if math.Abs(u.USD-cost(config.ModelPrice{InputPer1K: 0.0001, OutputPer1K: 0.0002}, 1000, 1000)) > 1e-15 {
		t.Fatalf("standard with override: %+v", u)
	}
	u = priced(t, p, tiered("Converse", "openai.gpt-oss-20b-1:0", "priority", 1000, 1000))
	want := config.ModelPrice{InputPer1K: 0.0001225, OutputPer1K: 0.000525}
	if math.Abs(u.USD-cost(want, 1000, 1000)) > 1e-15 {
		t.Fatalf("priority with override: %+v", u)
	}
	u = priced(t, p, tiered("Converse", "openai.gpt-oss-20b-1:0", "flex", 1000, 1000))
	want = config.ModelPrice{InputPer1K: 0.0001, OutputPer1K: 0.0002} // the entry is above flex on both sides
	if math.Abs(u.USD-cost(want, 1000, 1000)) > 1e-15 {
		t.Fatalf("flex with override: %+v", u)
	}
}

// A shared tier price raises a release tier price and never lowers it.
func TestSharedTierPricesOnlyTighten(t *testing.T) {
	up := map[string]map[string]map[string]config.ModelPrice{tieredRegion: {"openai.gpt-6-astra": {"ultrafast": {InputPer1K: 0.07, OutputPer1K: 0.3}}}}
	p := NewTiered(tieredTable(), nil, up, nil)
	u := priced(t, p, tiered("Responses", "openai.gpt-6-astra", "ultrafast", 1000, 1000))
	if want := cost(config.ModelPrice{InputPer1K: 0.07, OutputPer1K: 0.33}, 1000, 1000); math.Abs(u.USD-want) > 1e-15 {
		t.Fatalf("merged per side: $%v, want $%v", u.USD, want)
	}
	// A shared tier the release lacks applies too.
	add := map[string]map[string]map[string]config.ModelPrice{tieredRegion: {"amazon.nova-micro-v1:0": {"priority": {InputPer1K: 0.0001, OutputPer1K: 0.0004}}}}
	p = NewTiered(tieredTable(), nil, add, nil)
	if u := priced(t, p, tiered("Converse", "amazon.nova-micro-v1:0", "priority", 1000, 1000)); math.Abs(u.USD-0.0005) > 1e-15 || u.UnpricedTier != "" {
		t.Fatalf("shared-only tier: %+v", u)
	}
}

// Usage that can't show its tier is priced at the model's highest tier: mantle
// usage, and a stream completion whose call record wasn't seen.
func TestTierBlindUsagePricedAtHighestTier(t *testing.T) {
	p := New(tieredTable(), nil)
	u, h := p.Evaluate(mantle("openai.gpt-6-astra", tieredRegion, 1, 1000, 1000))
	if h != nil || u == nil || math.Abs(u.USD-cost(astraUltra, 1000, 1000)) > 1e-15 {
		t.Fatalf("mantle with ultrafast: %+v %+v", u, h)
	}
	u, h = p.Evaluate(mantle("amazon.nova-micro-v1:0", tieredRegion, 1, 1000, 1000))
	if h != nil || u == nil || math.Abs(u.USD-cost(config.ModelPrice{InputPer1K: 0.000035, OutputPer1K: 0.00014}, 1000, 1000)) > 1e-15 {
		t.Fatalf("mantle without tiers: %+v %+v", u, h)
	}
	c := tiered("ConverseStream", "openai.gpt-oss-20b-1:0", "", 1000, 1000)
	c.EventType, c.TierUnknown = "AwsServiceEvent", true
	if u := priced(t, p, c); math.Abs(u.USD-cost(ossPriority, 1000, 1000)) > 1e-15 || u.UnpricedTier != "" {
		t.Fatalf("unmatched stream: %+v", u)
	}
}

// OpenAI-compatible calls are priced like InvokeModel, with the same
// tripwires; a streamed one is priced from its completion.
func TestOpenAICompatibleCalls(t *testing.T) {
	p := New(tieredTable(), nil)
	if !p.Wants(call("ChatCompletions", "openai.gpt-oss-20b-1:0", 69, 5)) || !p.Wants(call("Responses", "openai.gpt-6-astra", 1, 1)) {
		t.Fatal("the pack must want OpenAI-compatible calls")
	}
	if u := priced(t, p, tiered("ChatCompletions", "openai.gpt-oss-20b-1:0", "", 69, 5)); math.Abs(u.USD-cost(ossStd, 69, 5)) > 1e-15 || u.Calls != 1 {
		t.Fatalf("standard: %+v", u)
	}
	if u := priced(t, p, tiered("ChatCompletions", "openai.gpt-oss-20b-1:0", "priority", 69, 5)); math.Abs(u.USD-cost(ossPriority, 69, 5)) > 1e-15 {
		t.Fatalf("priority: %+v", u)
	}
	streamed := tiered("ChatCompletions", "openai.gpt-oss-20b-1:0", "priority", 0, 0)
	streamed.Stream = true
	if u, h := p.Evaluate(streamed); u != nil || h != nil {
		t.Fatalf("a streamed call record is priced from its completion: %+v %+v", u, h)
	}
	allow := New(tieredTable(), &config.ResolvedAI{Models: []string{"amazon.nova-micro-v1:0"}})
	if _, h := allow.Evaluate(call("ChatCompletions", "openai.gpt-oss-20b-1:0", 69, 5)); h == nil || h.Rule != "model_not_allowed" {
		t.Fatalf("allowlist: %+v", h)
	}
}

// A call with no token counts trips unmeasured_call, or under allow counts as
// one call at $0; a completion with tokens is priced as before.
func TestUnmeasuredCalls(t *testing.T) {
	completion := tiered("InvokeModelWithResponseStream", "openai.gpt-oss-20b-1:0", "priority", 0, 0)
	completion.EventType, completion.Stream = "AwsServiceEvent", true
	responses := tiered("Responses", "openai.gpt-6-astra", "", 0, 0)

	cut := New(tieredTable(), nil)
	for _, r := range []activity.Record{completion, responses} {
		_, h := cut.Evaluate(r)
		if h == nil || h.Rule != "unmeasured_call" || !strings.Contains(h.Detail, r.ModelID) || !strings.Contains(h.Detail, "ai.unmeasured_calls") {
			t.Fatalf("%s: %+v", r.EventName, h)
		}
	}
	allow := New(tieredTable(), &config.ResolvedAI{UnmeasuredCalls: config.UnmeasuredAllow})
	for _, r := range []activity.Record{completion, responses} {
		u := priced(t, allow, r)
		if !u.Unmeasured || u.Calls != 1 || u.USD != 0 {
			t.Fatalf("%s allowed: %+v", r.EventName, u)
		}
	}
	withTokens := completion
	withTokens.InputTokens, withTokens.OutputTokens = 69, 5
	if u := priced(t, cut, withTokens); u.Unmeasured || math.Abs(u.USD-cost(ossPriority, 69, 5)) > 1e-15 {
		t.Fatalf("a completion with tokens: %+v", u)
	}
}

// The records the Nov 2026 test calls left (internal/activity/testdata),
// evaluated one by one: each priced at its tier, stream call records left to
// their completions, and the streamed Chat Completions completion, which has
// no token counts, unmeasured (add-tier-pricing task 1).
func TestRecordedTierCalls(t *testing.T) {
	raw, err := os.ReadFile("../../activity/testdata/bedrock-tier-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	recs, err := activity.ParseFile(b.Bytes())
	if err != nil || len(recs) != 10 {
		t.Fatalf("%d records, %v", len(recs), err)
	}
	want := []*config.ModelPrice{&ossPriority, nil, &ossPriority, &ossPriority, nil, &ossStd, &ossStd, &ossPriority, nil, nil}
	for _, allow := range []bool{false, true} {
		cfg := &config.ResolvedAI{UnmeasuredCalls: config.UnmeasuredCutoff}
		if allow {
			cfg.UnmeasuredCalls = config.UnmeasuredAllow
		}
		p := New(tieredTable(), cfg)
		for i, r := range recs {
			if !p.Wants(r) {
				t.Fatalf("record %d (%s) not wanted", i, r.EventName)
			}
			u, h := p.Evaluate(r)
			switch {
			case i == 9 && !allow:
				if u != nil || h == nil || h.Rule != "unmeasured_call" {
					t.Errorf("streamed Chat Completions completion: %+v %+v", u, h)
				}
			case i == 9:
				if h != nil || u == nil || !u.Unmeasured || u.USD != 0 || u.Calls != 1 {
					t.Errorf("streamed Chat Completions completion, allowed: %+v %+v", u, h)
				}
			case want[i] == nil:
				if u != nil || h != nil {
					t.Errorf("record %d (%s %s): %+v %+v", i, r.EventName, r.EventType, u, h)
				}
			default:
				if h != nil || u == nil || math.Abs(u.USD-cost(*want[i], 69, 5)) > 1e-15 {
					t.Errorf("record %d (%s %s %q): %+v %+v", i, r.EventName, r.EventType, r.ServiceTier, u, h)
				}
			}
		}
	}
}
