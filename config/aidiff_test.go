package config

import (
	"encoding/json"
	"testing"
)

func aiDoc(t *testing.T, ai string) *Resolved {
	t.Helper()
	return mustParse(t, `{"payer_account_id":"123456789012","scopes":[],"ai":`+ai+`}`).Resolve()
}

func only1(t *testing.T, ch []Change) Change {
	t.Helper()
	if len(ch) != 1 {
		t.Fatalf("want one change, got %v", ch)
	}
	return ch[0]
}

func TestAIChangeClassification(t *testing.T) {
	const acct = `{"account_id":"111122223333"`
	cases := []struct {
		name, from, to string
		dir            Direction
	}{
		{"lower cap", `{"scopes":[` + acct + `,"monthly_usd":500}]}`, `{"scopes":[` + acct + `,"monthly_usd":400}]}`, Tightening},
		{"new cap", `{"scopes":[` + acct + `}]}`, `{"scopes":[` + acct + `,"hourly_usd":5}]}`, Tightening},
		{"higher cap", `{"scopes":[` + acct + `,"monthly_usd":500}]}`, `{"scopes":[` + acct + `,"monthly_usd":600}]}`, Loosening},
		{"removed cap", `{"scopes":[` + acct + `,"daily_usd":50}]}`, `{"scopes":[` + acct + `}]}`, Loosening},
		{"to enforce", `{"scopes":[` + acct + `}]}`, `{"scopes":[` + acct + `,"mode":"enforce"}]}`, Tightening},
		{"to dry-run", `{"scopes":[` + acct + `,"mode":"enforce"}]}`, `{"scopes":[` + acct + `}]}`, Loosening},
		{"scope added", `{}`, `{"scopes":[` + acct + `}]}`, Tightening},
		{"scope removed", `{"scopes":[` + acct + `}]}`, `{}`, Loosening},
		{"allowlist set", `{}`, `{"models":["m1"]}`, Tightening},
		{"allowlist dropped", `{"models":["m1"]}`, `{}`, Loosening},
		{"model added", `{"models":["m1"]}`, `{"models":["m1","m2"]}`, Loosening},
		{"model removed", `{"models":["m1","m2"]}`, `{"models":["m1"]}`, Tightening},
		{"region added", `{"regions":["us-east-1"]}`, `{"regions":["us-east-1","eu-west-1"]}`, Loosening},
		{"api key caller added", `{}`, `{"api_key_callers":["arn:aws:iam::111122223333:user/ci"]}`, Loosening},
		{"api key caller removed", `{"api_key_callers":["arn:aws:iam::111122223333:user/ci"]}`, `{}`, Tightening},
		{"higher price", `{"prices":{"m1":{"input_per_1k":1,"output_per_1k":1}}}`, `{"prices":{"m1":{"input_per_1k":2,"output_per_1k":1}}}`, Tightening},
		{"lower price", `{"prices":{"m1":{"input_per_1k":1,"output_per_1k":1}}}`, `{"prices":{"m1":{"input_per_1k":1,"output_per_1k":0.5}}}`, Loosening},
		{"new price override", `{}`, `{"prices":{"m1":{"input_per_1k":9,"output_per_1k":9}}}`, Loosening},
		{"removed price override", `{"prices":{"m1":{"input_per_1k":9,"output_per_1k":9}}}`, `{}`, Loosening},
		{"default mode to enforce", `{}`, `{"defaults":{"mode":"enforce"}}`, Tightening},
		{"default mode to dry-run", `{"defaults":{"mode":"enforce"}}`, `{}`, Loosening},
		{"lift added", `{}`, `{"lifts":["ai:111122223333:20261111T0930"]}`, Loosening},
		{"lift removed", `{"lifts":["ai:111122223333:20261111T0930"]}`, `{}`, Notice},
	}
	for _, c := range cases {
		ch := only1(t, Diff(aiDoc(t, c.from), aiDoc(t, c.to)))
		if ch.Direction != c.dir {
			t.Errorf("%s: got %s (%s), want %s", c.name, ch.Direction, ch, c.dir)
		}
	}
}

func TestStricterAIWaitsForLoosening(t *testing.T) {
	a := aiDoc(t, `{"scopes":[{"account_id":"111122223333","monthly_usd":500,"hourly_usd":10}],"models":["m1"],"api_key_callers":["arn:aws:iam::111122223333:user/ci"]}`)
	d := aiDoc(t, `{"scopes":[{"account_id":"111122223333","monthly_usd":400,"hourly_usd":20,"mode":"enforce"}],"models":["m1","m2"],"lifts":["ai:111122223333:20261111T0930"]}`)
	got := Stricter(a, d).AI
	s, _ := got.Scope(Target{Kind: "account", ID: "111122223333"})
	if s.MonthlyUSD != 400 || s.HourlyUSD != 10 || s.Mode != ModeEnforce {
		t.Fatalf("scope: %+v", s)
	}
	if len(got.Models) != 1 || got.Models[0] != "m1" || got.APIKeyCallers != nil || got.Lifts != nil {
		t.Fatalf("loosening applied early: %+v", got)
	}
	if !HasLoosening(Diff(a, d)) || Equal(a, d) {
		t.Fatal("mixed version must be a loosening and not equal")
	}
}

func TestEmptyAllowlistSurvivesASave(t *testing.T) {
	r := aiDoc(t, `{"models":[]}`)
	raw, _ := json.Marshal(r)
	var back Resolved
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.AI.Models == nil {
		t.Fatal(`an empty allowlist ("no model") came back as nil ("any model")`)
	}
	if aiDoc(t, `{}`).AI.Models != nil {
		t.Fatal("an absent allowlist must resolve to nil")
	}
}
