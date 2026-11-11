package ai

import (
	"math"
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
