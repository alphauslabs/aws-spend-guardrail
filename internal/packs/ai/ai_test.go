package ai

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
)

const acct = "111122223333"

var at = time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

func table(t *testing.T) *Table {
	t.Helper()
	tb, err := Prices()
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func call(name, model string, in, out int64) activity.Record {
	return activity.Record{EventTime: at, EventSource: "bedrock.amazonaws.com", EventName: name, EventType: "AwsApiCall",
		Region: "us-east-1", Account: acct, CallerARN: "arn:aws:sts::" + acct + ":assumed-role/app/s1", ModelID: model, InputTokens: in, OutputTokens: out}
}

func TestPriceTableIsSane(t *testing.T) {
	tb := table(t)
	if len(tb.Models) < 20 || tb.Source == "" || tb.Published == "" {
		t.Fatalf("table: %d models, source %q, published %q", len(tb.Models), tb.Source, tb.Published)
	}
	for id, p := range tb.Models {
		if p.InputPer1K <= 0 || p.OutputPer1K <= 0 || p.InputPer1K > 1 || p.OutputPer1K > 1 {
			t.Errorf("%s has an implausible price: %+v", id, p)
		}
	}
	if p := tb.Models["amazon.nova-micro-v1:0"]; p.InputPer1K != 0.000035 || p.OutputPer1K != 0.00014 {
		t.Errorf("nova micro price: %+v", p)
	}
	// Prices per region (add-regional-prices D1): every region's prices are
	// plausible, us-east-1's match the legacy view where both list a model,
	// and the legacy view prices every model some region prices.
	if len(tb.Regions) < 10 {
		t.Fatalf("table: %d regions", len(tb.Regions))
	}
	for region, models := range tb.Regions {
		for id, p := range models {
			if p.InputPer1K <= 0 || p.OutputPer1K <= 0 || p.InputPer1K > 1 || p.OutputPer1K > 1 {
				t.Errorf("%s in %s has an implausible price: %+v", id, region, p)
			}
			if _, ok := tb.Models[id]; !ok {
				t.Errorf("%s is priced in %s but missing from the legacy view", id, region)
			}
		}
	}
	for id, p := range tb.Regions[DefaultRegion] {
		if tb.Models[id] != p {
			t.Errorf("%s: legacy %+v, us-east-1 %+v", id, tb.Models[id], p)
		}
	}
}

func TestModelKey(t *testing.T) {
	for in, want := range map[string]string{
		"us.amazon.nova-micro-v1:0": "amazon.nova-micro-v1:0",
		"arn:aws:bedrock:us-east-1:111122223333:inference-profile/us.amazon.nova-micro-v1:0": "amazon.nova-micro-v1:0",
		"arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-micro-v1:0":                 "amazon.nova-micro-v1:0",
		"amazon.nova-micro-v1:0:128k":                                                 "amazon.nova-micro-v1:0",
		"global.anthropic.claude-haiku-5-5":                                           "anthropic.claude-haiku-5-5",
		"arn:aws:bedrock:us-east-1:111122223333:application-inference-profile/abc123": "arn:aws:bedrock:us-east-1:111122223333:application-inference-profile/abc123",
	} {
		if got := ModelKey(in); got != want {
			t.Errorf("ModelKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPricingTwoNamingsOneModel(t *testing.T) {
	p := New(table(t), nil)
	u1, h1 := p.Evaluate(call("Converse", "us.amazon.nova-micro-v1:0", 1000, 1000))
	u2, h2 := p.Evaluate(call("InvokeModel", "arn:aws:bedrock:us-east-1:"+acct+":inference-profile/us.amazon.nova-micro-v1:0", 1000, 1000))
	if h1 != nil || h2 != nil || u1 == nil || u2 == nil {
		t.Fatalf("usage %v %v hits %v %v", u1, u2, h1, h2)
	}
	want := 0.000035 + 0.00014
	if math.Abs(u1.USD-want) > 1e-12 || u1.USD != u2.USD || u1.Model != "amazon.nova-micro-v1:0" {
		t.Fatalf("usd %v %v, want %v", u1.USD, u2.USD, want)
	}
}

func TestStreamingPricedOnceFromCompletion(t *testing.T) {
	p := New(table(t), nil)
	api := call("ConverseStream", "us.amazon.nova-micro-v1:0", 0, 0)
	if u, h := p.Evaluate(api); u != nil || h != nil {
		t.Fatalf("the call record of a stream must not be priced: %v %v", u, h)
	}
	done := api
	done.EventType, done.InputTokens, done.OutputTokens = "AwsServiceEvent", 2000, 0
	done.ModelID = "arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-micro-v1:0"
	u, _ := p.Evaluate(done)
	if u == nil || math.Abs(u.USD-0.00007) > 1e-12 {
		t.Fatalf("completion usage %+v", u)
	}
}

func TestRefusedCallsCostNothing(t *testing.T) {
	r := call("Converse", "us.amazon.nova-micro-v1:0", 1000, 1000)
	r.ErrorCode = "AccessDeniedException"
	if u, h := New(table(t), nil).Evaluate(r); u != nil || h != nil {
		t.Fatalf("refused call: %v %v", u, h)
	}
}

func TestOverrideWinsOverTable(t *testing.T) {
	p := New(table(t), &config.ResolvedAI{Prices: map[string]config.ModelPrice{"us.amazon.nova-micro-v1:0": {InputPer1K: 1, OutputPer1K: 0}}})
	u, _ := p.Evaluate(call("Converse", "amazon.nova-micro-v1:0", 1000, 1000))
	if u == nil || u.USD != 1 {
		t.Fatalf("override not used: %+v", u)
	}
}

func TestTripwires(t *testing.T) {
	tb := table(t)
	bearer := call("Converse", "us.amazon.nova-micro-v1:0", 1, 1)
	bearer.BearerToken, bearer.CallerARN = true, "arn:aws:iam::"+acct+":user/leaked"
	cases := []struct {
		name string
		cfg  *config.ResolvedAI
		r    activity.Record
		rule string
	}{
		{"api key by anyone", nil, bearer, "api_key_not_allowed"},
		{"region", &config.ResolvedAI{Regions: []string{"eu-west-1"}}, call("Converse", "us.amazon.nova-micro-v1:0", 1, 1), "region_not_allowed"},
		{"model", &config.ResolvedAI{Models: []string{"anthropic.claude-haiku-5-5"}}, call("Converse", "us.amazon.nova-micro-v1:0", 1, 1), "model_not_allowed"},
		{"empty model allowlist", &config.ResolvedAI{Models: []string{}}, call("Converse", "us.amazon.nova-micro-v1:0", 1, 1), "model_not_allowed"},
		{"unknown model", nil, call("Converse", "acme.secret-model-v1:0", 1, 1), "unknown_model"},
		{"logging change", nil, activity.Record{EventSource: "bedrock.amazonaws.com", EventName: "DeleteModelInvocationLoggingConfiguration", EventType: "AwsApiCall", Account: acct}, "logging_changed"},
	}
	for _, c := range cases {
		p := New(tb, c.cfg)
		if !p.Wants(c.r) {
			t.Errorf("%s: pack doesn't want the record", c.name)
			continue
		}
		_, h := p.Evaluate(c.r)
		if h == nil || h.Rule != c.rule || h.Account != acct {
			t.Errorf("%s: hit %+v, want %s", c.name, h, c.rule)
		}
	}
}

func TestAllowedAPIKeyCallerAndPricedUnknownModel(t *testing.T) {
	tb := table(t)
	r := call("Converse", "us.amazon.nova-micro-v1:0", 1, 1)
	r.BearerToken = true
	p := New(tb, &config.ResolvedAI{APIKeyCallers: []string{"arn:aws:iam::" + acct + ":role/app"}})
	if _, h := p.Evaluate(r); h != nil {
		t.Fatalf("an assumed-role session of an allowed role tripped: %+v", h)
	}
	p = New(tb, &config.ResolvedAI{Models: []string{"acme.secret-model-v1:0"}, Prices: map[string]config.ModelPrice{"acme.secret-model-v1:0": {InputPer1K: 0.01, OutputPer1K: 0.01}}})
	if u, h := p.Evaluate(call("Converse", "acme.secret-model-v1:0", 1000, 0)); h != nil || u == nil || u.USD != 0.01 {
		t.Fatalf("allowlisted and priced model: %+v %+v", u, h)
	}
}

func TestSharedPricesOnlyTighten(t *testing.T) {
	tb := &Table{Models: map[string]config.ModelPrice{
		"anthropic.claude-a": {InputPer1K: 0.003, OutputPer1K: 0.015},
		"anthropic.claude-b": {InputPer1K: 0.0055, OutputPer1K: 0.0275},
		"amazon.nova-micro":  {InputPer1K: 0.000035, OutputPer1K: 0.00014},
	}}
	shared := map[string]config.ModelPrice{
		"anthropic.claude-a":   {InputPer1K: 0.004, OutputPer1K: 0.010}, // input raised, output lower
		"amazon.nova-micro":    {InputPer1K: 0.00001, OutputPer1K: 0.0001},
		"anthropic.claude-new": {InputPer1K: 0.001, OutputPer1K: 0.001},
		"acme.model-x":         {InputPer1K: 0.0001, OutputPer1K: 0.0001},
	}
	p := NewShared(tb, inUSEast1(shared), nil)
	cases := map[string]config.ModelPrice{
		"anthropic.claude-a":   {InputPer1K: 0.004, OutputPer1K: 0.015},      // higher of each
		"amazon.nova-micro":    {InputPer1K: 0.000035, OutputPer1K: 0.00014}, // lower ignored
		"anthropic.claude-new": {InputPer1K: 0.0055, OutputPer1K: 0.0275},    // floored at the provider's highest
		"acme.model-x":         {InputPer1K: 0.0055, OutputPer1K: 0.0275},    // provider absent: the table's highest
	}
	for id, want := range cases {
		if got, ok := p.Price(id); !ok || got != want {
			t.Errorf("%s: %+v, want %+v", id, got, want)
		}
	}
	over := NewShared(tb, inUSEast1(shared), &config.ResolvedAI{Prices: map[string]config.ModelPrice{"amazon.nova-micro": {InputPer1K: 0.00001, OutputPer1K: 0.00001}}})
	if got, _ := over.Price("amazon.nova-micro"); got.InputPer1K != 0.00001 {
		t.Errorf("an org's approved override must win: %+v", got)
	}
	if r, a, i := SharedEffect(tb.Models, shared); r != 1 || a != 2 || i != 1 {
		t.Errorf("effect raised %d added %d ignored %d", r, a, i)
	}
	if _, h := p.Evaluate(call("Converse", "anthropic.claude-new", 1, 1)); h != nil {
		t.Errorf("a shared-only model must not trip as unknown: %+v", h)
	}
	// Floor, which publishing warns with, agrees with what the pack charges.
	for _, id := range []string{"anthropic.claude-new", "acme.model-x"} {
		if f, _ := p.Price(id); tb.Floor(id) != f {
			t.Errorf("Floor(%s) = %+v, pack charges %+v", id, tb.Floor(id), f)
		}
	}
}

// The price an unpriced model is counted at: the highest input and output
// known across the release, shared, and override tables (add-unknown-model-policy D2).
func TestUnknownPriceIsTheHighestKnown(t *testing.T) {
	tb := &Table{Models: map[string]config.ModelPrice{
		"anthropic.claude-a": {InputPer1K: 0.015, OutputPer1K: 0.075},
		"amazon.nova-micro":  {InputPer1K: 0.000035, OutputPer1K: 0.00014},
	}}
	if got := NewShared(tb, nil, nil).UnknownPrice(); got != (config.ModelPrice{InputPer1K: 0.015, OutputPer1K: 0.075}) {
		t.Errorf("release only: %+v", got)
	}
	shared := map[string]config.ModelPrice{"xai.grok-new": {InputPer1K: 0.02, OutputPer1K: 0.01}}
	over := &config.ResolvedAI{Prices: map[string]config.ModelPrice{"custom.model": {InputPer1K: 0.001, OutputPer1K: 0.09}}}
	if got := NewShared(tb, inUSEast1(shared), over).UnknownPrice(); got != (config.ModelPrice{InputPer1K: 0.02, OutputPer1K: 0.09}) {
		t.Errorf("each side's maximum across all three tables: %+v", got)
	}
}

func TestUnknownModelsCountedOrCutOff(t *testing.T) {
	tb := table(t)
	count := &config.ResolvedAI{UnknownModels: config.UnknownCount}
	ceiling := NewShared(tb, nil, count).UnknownPrice()

	// Cutoff (the default): the call record trips, as before.
	if _, h := New(tb, &config.ResolvedAI{UnknownModels: config.UnknownCutoff}).Evaluate(call("Converse", "xai.grok-9", 1000, 1000)); h == nil || h.Rule != "unknown_model" {
		t.Fatalf("cutoff: %+v", h)
	}
	if _, h := New(tb, &config.ResolvedAI{}).Evaluate(call("Converse", "xai.grok-9", 1000, 1000)); h == nil || h.Rule != "unknown_model" {
		t.Fatalf("state without the setting must cut off: %+v", h)
	}

	p := New(tb, count)
	// Count: a non-streaming call is priced at the ceiling.
	u, h := p.Evaluate(call("Converse", "us.xai.grok-9", 1000, 1000))
	if h != nil || u == nil || !u.Unpriced || u.Model != "xai.grok-9" || math.Abs(u.USD-(ceiling.InputPer1K+ceiling.OutputPer1K)) > 1e-12 {
		t.Fatalf("counted call: %+v %+v", u, h)
	}
	// A streaming call: nothing at its call record, priced from its completion.
	api := call("ConverseStream", "xai.grok-9", 0, 0)
	if u, h := p.Evaluate(api); u != nil || h != nil {
		t.Fatalf("stream call record: %+v %+v", u, h)
	}
	done := api
	done.EventType, done.InputTokens, done.OutputTokens = "AwsServiceEvent", 2000, 0
	if u, h := p.Evaluate(done); h != nil || u == nil || !u.Unpriced || math.Abs(u.USD-2*ceiling.InputPer1K) > 1e-12 {
		t.Fatalf("stream completion: %+v %+v", u, h)
	}
	// No token counts: can't be estimated, so it still trips.
	if _, h := p.Evaluate(call("InvokeModel", "twelvelabs.pegasus-9", 0, 0)); h == nil || h.Rule != "unknown_model" || !strings.Contains(h.Detail, "no token counts") {
		t.Fatalf("no tokens: %+v", h)
	}
	// A priced model is not marked.
	if u, _ := p.Evaluate(call("Converse", "us.amazon.nova-micro-v1:0", 1000, 1000)); u == nil || u.Unpriced {
		t.Fatalf("priced model: %+v", u)
	}
	// An allowlist still applies first.
	listed := New(tb, &config.ResolvedAI{UnknownModels: config.UnknownCount, Models: []string{"amazon.nova-micro-v1:0"}})
	if _, h := listed.Evaluate(call("Converse", "xai.grok-9", 1000, 1000)); h == nil || h.Rule != "model_not_allowed" {
		t.Fatalf("allowlist: %+v", h)
	}
	if !p.Priced("us.amazon.nova-micro-v1:0") || p.Priced("xai.grok-9") {
		t.Error("Priced")
	}
}

// inUSEast1 is one region's shared prices, as a guard holds them.
func inUSEast1(m map[string]config.ModelPrice) map[string]map[string]config.ModelPrice {
	return map[string]map[string]config.ModelPrice{DefaultRegion: m}
}

// Each call is priced at its region's price (add-regional-prices D4).
func TestCallsPricedAtTheirRegion(t *testing.T) {
	tb := &Table{Regions: map[string]map[string]config.ModelPrice{
		"us-east-1": {"amazon.m1": {InputPer1K: 0.004, OutputPer1K: 0.01}, "anthropic.a": {InputPer1K: 0.003, OutputPer1K: 0.015}},
		"eu-west-2": {"amazon.m1": {InputPer1K: 0.0062, OutputPer1K: 0.0155}},
	}}
	at := func(p *Pack, model, region string) config.ModelPrice {
		t.Helper()
		r := call("Converse", model, 1000, 1000)
		r.Region = region
		u, h := p.Evaluate(r)
		if h != nil || u == nil {
			t.Fatalf("%s in %s: %+v %+v", model, region, u, h)
		}
		return config.ModelPrice{InputPer1K: u.USD} // input and output are equal token counts; compare sums
	}
	p := New(tb, nil)
	if got := at(p, "amazon.m1", "eu-west-2"); got.InputPer1K != 0.0062+0.0155 {
		t.Errorf("a call in London: %v", got.InputPer1K)
	}
	if got := at(p, "amazon.m1", "us-east-1"); got.InputPer1K != 0.004+0.01 {
		t.Errorf("a call in us-east-1: %v", got.InputPer1K)
	}
	// No price in the call's region: the model's highest, side by side.
	if got, _ := p.PriceIn("amazon.m1", "ap-southeast-7"); got != (config.ModelPrice{InputPer1K: 0.0062, OutputPer1K: 0.0155}) {
		t.Errorf("no regional price: %+v", got)
	}
	// A global inference profile is priced at the call region's price.
	if got := at(p, "global.amazon.m1", "eu-west-2"); got.InputPer1K != 0.0062+0.0155 {
		t.Errorf("a global profile in London: %v", got.InputPer1K)
	}
	// An override applies in every region.
	over := New(tb, &config.ResolvedAI{Prices: map[string]config.ModelPrice{"amazon.m1": {InputPer1K: 0.001, OutputPer1K: 0.001}}})
	for _, region := range []string{"us-east-1", "eu-west-2", "sa-east-1"} {
		if got, _ := over.PriceIn("amazon.m1", region); got.InputPer1K != 0.001 {
			t.Errorf("override in %s: %+v", region, got)
		}
	}
	// Shared prices tighten per region; a shared-only model is floored at its
	// provider's highest release price in that region, or in any region.
	shared := map[string]map[string]config.ModelPrice{
		"eu-west-2": {"amazon.m1": {InputPer1K: 0.007, OutputPer1K: 0.01}, "amazon.new": {InputPer1K: 0.001, OutputPer1K: 0.001}},
		"sa-east-1": {"anthropic.new": {InputPer1K: 0.0001, OutputPer1K: 0.0001}},
	}
	sp := NewShared(tb, shared, nil)
	if got, _ := sp.PriceIn("amazon.m1", "eu-west-2"); got != (config.ModelPrice{InputPer1K: 0.007, OutputPer1K: 0.0155}) {
		t.Errorf("shared tighten in eu-west-2: %+v", got)
	}
	if got, _ := sp.PriceIn("amazon.new", "eu-west-2"); got != (config.ModelPrice{InputPer1K: 0.0062, OutputPer1K: 0.0155}) {
		t.Errorf("amazon floor in eu-west-2: %+v", got)
	}
	if got, _ := sp.PriceIn("anthropic.new", "sa-east-1"); got != (config.ModelPrice{InputPer1K: 0.003, OutputPer1K: 0.015}) {
		t.Errorf("anthropic floor from another region: %+v", got)
	}
	if got := sp.UnknownPrice(); got != (config.ModelPrice{InputPer1K: 0.007, OutputPer1K: 0.0155}) {
		t.Errorf("unknown price, the highest in any region: %+v", got)
	}
}

// mantle is a minute's mantle usage of one account and model, read from
// metrics (add-mantle-usage D1).
func mantle(model, region string, calls, in, out int64) activity.Record {
	return activity.Record{EventTime: at, EventSource: activity.MantleSource, EventName: "CreateInference", EventType: "MetricDatapoint",
		EventCategory: activity.CategoryMetric, Region: region, Account: acct, ModelID: model, Calls: calls, InputTokens: in, OutputTokens: out}
}

func TestMantleUsage(t *testing.T) {
	tb := &Table{Regions: map[string]map[string]config.ModelPrice{
		"us-east-1": {"openai.gpt-oss-20b": {InputPer1K: 0.00007, OutputPer1K: 0.0003}, "openai.gpt-oss-20b-1:0": {InputPer1K: 0.00007, OutputPer1K: 0.0003}},
		"eu-west-2": {"openai.gpt-oss-20b": {InputPer1K: 0.0001, OutputPer1K: 0.0004}},
	}}
	p := New(tb, nil)
	r := mantle("openai.gpt-oss-20b", "eu-west-2", 2, 140, 117)
	if !p.Wants(r) {
		t.Fatal("the pack must want mantle usage")
	}
	u, h := p.Evaluate(r)
	want := 140.0/1000*0.0001 + 117.0/1000*0.0004
	if h != nil || u == nil || u.Calls != 2 || u.In != 140 || u.Out != 117 || math.Abs(u.USD-want) > 1e-12 || u.Region != "eu-west-2" {
		t.Fatalf("priced at its eu-west-2 price as 2 calls: %+v %+v", u, h)
	}
	// Mantle usage from another source is not ours.
	other := r
	other.EventSource = "other.amazonaws.com"
	if p.Wants(other) {
		t.Error("only mantle metric records")
	}
	// An allowlist naming the runtime id allows the mantle id.
	allow := New(tb, &config.ResolvedAI{Models: []string{"openai.gpt-oss-20b-1:0"}})
	if u, h := allow.Evaluate(mantle("openai.gpt-oss-20b", "us-east-1", 1, 10, 10)); h != nil || u == nil {
		t.Fatalf("allowlisted by its runtime id: %+v %+v", u, h)
	}
	// A model off the allowlist trips, naming mantle.
	strict := New(tb, &config.ResolvedAI{Models: []string{"anthropic.claude-haiku-5-5"}})
	if _, h := strict.Evaluate(mantle("openai.gpt-6-sol", "us-east-1", 1, 10, 10)); h == nil || h.Rule != "model_not_allowed" || !strings.Contains(h.Detail, "through mantle") {
		t.Fatalf("off the allowlist: %+v", h)
	}
	// A region off the region allowlist trips.
	if _, h := New(tb, &config.ResolvedAI{Regions: []string{"us-east-1"}}).Evaluate(mantle("openai.gpt-oss-20b", "eu-west-2", 1, 10, 10)); h == nil || h.Rule != "region_not_allowed" {
		t.Fatalf("region: %+v", h)
	}
	// An unpriced model follows the unknown-model policy.
	if _, h := p.Evaluate(mantle("acme.new-model", "us-east-1", 1, 10, 10)); h == nil || h.Rule != "unknown_model" || !strings.Contains(h.Detail, "through mantle") {
		t.Fatalf("unknown under cutoff: %+v", h)
	}
	count := New(tb, &config.ResolvedAI{UnknownModels: config.UnknownCount})
	if u, h := count.Evaluate(mantle("acme.new-model", "us-east-1", 1, 1000, 0)); h != nil || u == nil || !u.Unpriced || u.USD != count.UnknownPrice().InputPer1K {
		t.Fatalf("unknown under count: %+v %+v", u, h)
	}
	// Calls without tokens yet: nothing to price until the tokens arrive.
	if u, h := count.Evaluate(mantle("acme.new-model", "us-east-1", 1, 0, 0)); h != nil || u != nil {
		t.Fatalf("tokens not read yet: %+v %+v", u, h)
	}
}
