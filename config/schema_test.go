package config

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// The Go checks and config/schema.json must describe the same document. This
// test fails when one changes without the other.
func TestGoChecksAgreeWithSchemaJSON(t *testing.T) {
	var s struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
		Defs       struct {
			AccountID struct {
				Pattern string `json:"pattern"`
			} `json:"account_id"`
			OUID struct {
				Pattern string `json:"pattern"`
			} `json:"ou_id"`
			Settings struct {
				Properties map[string]struct {
					Enum    []string `json:"enum"`
					Minimum *int     `json:"minimum"`
					Maximum *int     `json:"maximum"`
				} `json:"properties"`
			} `json:"settings"`
			Scope struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"scope"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(SchemaJSON, &s); err != nil {
		t.Fatal(err)
	}

	if s.Defs.AccountID.Pattern != accountIDPattern {
		t.Errorf("account_id pattern: schema %q, Go %q", s.Defs.AccountID.Pattern, accountIDPattern)
	}
	if s.Defs.OUID.Pattern != ouIDPattern {
		t.Errorf("ou_id pattern: schema %q, Go %q", s.Defs.OUID.Pattern, ouIDPattern)
	}
	sameSet(t, "document fields", keys(s.Properties), documentFields)
	sameSet(t, "document required", s.Required, []string{"payer_account_id", "scopes"})
	sameSet(t, "scope fields", keys(s.Defs.Scope.Properties), scopeFields)
	sameSet(t, "scope required", s.Defs.Scope.Required, []string{"ceiling_usd"})

	var settingNames []string
	for name, p := range s.Defs.Settings.Properties {
		settingNames = append(settingNames, name)
		if name == "threshold_pct" {
			if p.Minimum == nil || *p.Minimum != minThreshold || p.Maximum == nil || *p.Maximum != maxThreshold {
				t.Errorf("threshold_pct range differs from %d..%d", minThreshold, maxThreshold)
			}
			continue
		}
		sameSet(t, name+" values", p.Enum, enums[name])
	}
	sameSet(t, "settings fields", settingNames, settingsFields)

	var emails struct {
		MaxItems int `json:"maxItems"`
	}
	if err := json.Unmarshal(s.Properties["alert_emails"], &emails); err != nil || emails.MaxItems != maxAlertEmails {
		t.Errorf("alert_emails maxItems: schema %d, Go %d (%v)", emails.MaxItems, maxAlertEmails, err)
	}
}

func keys(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sameSet(t *testing.T, what string, a, b []string) {
	t.Helper()
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) != len(y) {
		t.Errorf("%s: schema %v, Go %v", what, x, y)
		return
	}
	for i := range x {
		if x[i] != y[i] {
			t.Errorf("%s: schema %v, Go %v", what, x, y)
			return
		}
	}
}

func TestShapeMessages(t *testing.T) {
	cases := []struct {
		doc, field, msg string
	}{
		{`[]`, "", "must be a JSON object"},
		{`{"scopes":[]}`, "payer_account_id", "is required"},
		{`{"payer_account_id":"123"}`, "payer_account_id", "12-digit"},
		{`{"payer_account_id":"123456789012"}`, "scopes", "is required"},
		{`{"payer_account_id":"123456789012","scopes":{}}`, "scopes", "must be an array"},
		{`{"payer_account_id":"123456789012","scopes":[{"ceiling_usd":1}]}`, "scopes[0]", "account_id or ou_id is required"},
		{`{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333"}]}`, "scopes[0].ceiling_usd", "is required"},
		{`{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":0}]}`, "scopes[0].ceiling_usd", "must be greater than 0"},
		{`{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":"5"}]}`, "scopes[0].ceiling_usd", "must be a number"},
		{`{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":1,"threshold_pct":90.5}]}`, "scopes[0].threshold_pct", "whole number"},
		{`{"payer_account_id":"123456789012","scopes":[{"ou_id":"ou-bad","ceiling_usd":1}]}`, "scopes[0].ou_id", "OU id"},
		{`{"payer_account_id":"123456789012","defaults":{"mode":"off"},"scopes":[]}`, "defaults.mode", "must be one of dry-run, enforce"},
		{`{"payer_account_id":"123456789012","defaults":{"ceiling_usd":1},"scopes":[]}`, "defaults.ceiling_usd", "unknown field"},
		{`{"payer_account_id":"123456789012","scopes":[],"alert_emails":"a@example.com"}`, "alert_emails", "must be an array"},
		{`{"payer_account_id":"123456789012","scopes":[],"alert_emails":["Ops <ops@example.com>"]}`, "alert_emails[0]", "plain email address"},
		{`{"payer_account_id":"123456789012","scopes":[],"alert_emails":["ops"]}`, "alert_emails[0]", "plain email address"},
		{`{"payer_account_id":"123456789012","scopes":[],"alert_emails":[5]}`, "alert_emails[0]", "must be a string"},
		{`{"payer_account_id":"123456789012","scopes":[],"alert_emails":[` + strings.TrimSuffix(strings.Repeat(`"a@example.com",`, 21), ",") + `]}`, "alert_emails", "at most 20"},
	}
	for _, c := range cases {
		probs := checkSchema([]byte(c.doc))
		if !hasProblem(probs, c.field, c.msg) {
			t.Errorf("%s: want %q at %q, got %v", c.doc, c.msg, c.field, probs)
		}
	}
}
