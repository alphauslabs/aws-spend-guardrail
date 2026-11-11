package config

import (
	"encoding/json"
	"testing"
)

func TestAISchemaAgreesWithGo(t *testing.T) {
	var s struct {
		Properties struct {
			AI struct {
				Properties map[string]struct {
					Items struct {
						Pattern string `json:"pattern"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"ai"`
		} `json:"properties"`
		Defs struct {
			Caps  struct{ Properties map[string]json.RawMessage } `json:"ai_caps"`
			Scope struct{ Properties map[string]json.RawMessage } `json:"ai_scope"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(SchemaJSON, &s); err != nil {
		t.Fatal(err)
	}
	var aiNames []string
	for k := range s.Properties.AI.Properties {
		aiNames = append(aiNames, k)
	}
	sameSet(t, "ai fields", aiNames, aiFields)
	sameSet(t, "ai cap fields", keys(s.Defs.Caps.Properties), capFields)
	sameSet(t, "ai scope fields", keys(s.Defs.Scope.Properties), aiScopeFld)
	if got := s.Properties.AI.Properties["lifts"].Items.Pattern; got != LiftIDPattern {
		t.Errorf("lift pattern: schema %q, Go %q", got, LiftIDPattern)
	}
	if got := s.Properties.AI.Properties["regions"].Items.Pattern; got != regionRe.String() {
		t.Errorf("region pattern: schema %q, Go %q", got, regionRe.String())
	}
	if got := s.Properties.AI.Properties["api_key_callers"].Items.Pattern; got != iamCallRe.String() {
		t.Errorf("caller pattern: schema %q, Go %q", got, iamCallRe.String())
	}
}

func TestMinimalAISection(t *testing.T) {
	d := mustParse(t, `{"payer_account_id":"123456789012","scopes":[],"ai":{"defaults":{"monthly_usd":500},"scopes":[{"account_id":"111122223333"}]}}`)
	ai := d.Resolve().AI
	s, ok := ai.Scope(Target{Kind: "account", ID: "111122223333"})
	if !ok || s.MonthlyUSD != 500 || s.HourlyUSD != 0 || s.Mode != ModeDryRun {
		t.Fatalf("resolved AI scope: %+v %v", s, ok)
	}
	if ai.Models != nil || ai.Regions != nil || ai.APIKeyCallers != nil {
		t.Fatalf("absent allowlists must stay nil (any): %+v", ai)
	}
	if d := mustParse(t, `{"payer_account_id":"123456789012","scopes":[]}`); d.Resolve().AI != nil {
		t.Fatal("no ai section resolves to nil")
	}
}

func TestAIValidation(t *testing.T) {
	base := `{"payer_account_id":"123456789012","scopes":[],"ai":`
	cases := []struct{ ai, field, msg string }{
		{`{"scopes":[{"account_id":"111122223333","hourly_usd":-1}]}`, "ai.scopes[0].hourly_usd", "must not be negative"},
		{`{"defaults":{"daily_usd":"5"}}`, "ai.defaults.daily_usd", "must be a number"},
		{`{"scopes":[{"account_id":"111122223333"},{"account_id":"111122223333"}]}`, "ai.scopes[1]", "already an AI scope"},
		{`{"scopes":[{"ou_id":"r-ab12"}]}`, "ai.scopes[0].ou_id", "organization root"},
		{`{"prices":{"amazon.nova-micro-v1:0":{"input_per_1k":0.1}}}`, "ai.prices.amazon.nova-micro-v1:0.output_per_1k", "is required"},
		{`{"prices":{"m":{"input_per_1k":-0.1,"output_per_1k":0.1}}}`, "ai.prices.m.input_per_1k", "must not be negative"},
		{`{"lifts":["ai:1111:x"]}`, "ai.lifts[0]", "cutoff id"},
		{`{"regions":["useast1"]}`, "ai.regions[0]", "AWS region"},
		{`{"api_key_callers":["bob"]}`, "ai.api_key_callers[0]", "IAM user or role ARN"},
		{`{"models":["a","a"]}`, "ai.models[1]", "listed twice"},
		{`{"budget":1}`, "ai.budget", "unknown field"},
		{`{"scopes":[{"account_id":"111122223333","mode":"off"}]}`, "ai.scopes[0].mode", "must be one of"},
	}
	for _, c := range cases {
		probs := problems(t, base+c.ai+`}`, Options{})
		if !hasProblem(probs, c.field, c.msg) {
			t.Errorf("%s: want %q at %q, got %v", c.ai, c.msg, c.field, probs)
		}
	}
}
