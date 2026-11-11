package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustParse(t *testing.T, doc string) *Document {
	t.Helper()
	d, err := Parse([]byte(doc), Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func problems(t *testing.T, doc string, opts Options) []Problem {
	t.Helper()
	_, err := Parse([]byte(doc), opts)
	var ie *InvalidError
	if !errors.As(err, &ie) {
		t.Fatalf("expected *InvalidError, got %v", err)
	}
	return ie.Problems
}

func hasProblem(probs []Problem, field, substr string) bool {
	for _, p := range probs {
		if p.Field == field && strings.Contains(p.Message, substr) {
			return true
		}
	}
	return false
}

func TestExamplesAreValid(t *testing.T) {
	files, err := filepath.Glob("examples/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(raw, Options{PayerAccountID: "123456789012"}); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestDefaultsFillAMinimalScope(t *testing.T) {
	d := mustParse(t, `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":20000}]}`)
	got := d.Resolve().Scopes[0]
	want := ResolvedScope{
		Target:       Target{Kind: "account", ID: "111122223333"},
		CeilingUSD:   20000,
		Period:       PeriodMonthly,
		ThresholdPct: 100,
		FreezeStyle:  StyleB,
		Mode:         ModeDryRun,
		MonthEnd:     MonthEndStayFrozen,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDocumentDefaultsBeatBuiltinsAndScopeBeatsBoth(t *testing.T) {
	d := mustParse(t, `{"payer_account_id":"123456789012",
		"defaults":{"mode":"enforce","threshold_pct":90},
		"scopes":[{"account_id":"111122223333","ceiling_usd":1,"threshold_pct":80}]}`)
	s := d.Resolve().Scopes[0]
	if s.Mode != ModeEnforce || s.ThresholdPct != 80 || s.FreezeStyle != StyleB {
		t.Fatalf("got %+v", s)
	}
}

func TestValidationRules(t *testing.T) {
	cases := []struct {
		name  string
		doc   string
		opts  Options
		field string
		msg   string
	}{
		{
			name:  "approvers field is refused",
			doc:   `{"payer_account_id":"123456789012","approvers":["a@example.com"],"scopes":[]}`,
			field: "approvers",
			msg:   "unknown field",
		},
		{
			name:  "another payer account",
			doc:   `{"payer_account_id":"123456789012","scopes":[]}`,
			opts:  Options{PayerAccountID: "999999999999"},
			field: "payer_account_id",
			msg:   "expected 999999999999",
		},
		{
			name:  "duplicate scope",
			doc:   `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":1},{"account_id":"111122223333","ceiling_usd":2}]}`,
			field: "scopes[1]",
			msg:   "111122223333 is already a scope at scopes[0]",
		},
		{
			name:  "payer account as scope",
			doc:   `{"payer_account_id":"123456789012","scopes":[{"account_id":"123456789012","ceiling_usd":1}]}`,
			field: "scopes[0].account_id",
			msg:   "payer account cannot be a scope",
		},
		{
			name:  "organization root as scope",
			doc:   `{"payer_account_id":"123456789012","scopes":[{"ou_id":"r-ab12","ceiling_usd":1}]}`,
			field: "scopes[0].ou_id",
			msg:   "organization root cannot be a scope",
		},
		{
			name:  "threshold above 100",
			doc:   `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":1,"threshold_pct":101}]}`,
			field: "scopes[0].threshold_pct",
			msg:   "100",
		},
		{
			name:  "threshold below 1",
			doc:   `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":1,"threshold_pct":0}]}`,
			field: "scopes[0].threshold_pct",
			msg:   "1",
		},
		{
			name:  "both account and ou",
			doc:   `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ou_id":"ou-ab12-cd34ef56","ceiling_usd":1}]}`,
			field: "scopes[0]",
			msg:   "",
		},
		{
			name:  "zero ceiling",
			doc:   `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":0}]}`,
			field: "scopes[0].ceiling_usd",
			msg:   "",
		},
		{
			name:  "unknown mode",
			doc:   `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":1,"mode":"off"}]}`,
			field: "scopes[0].mode",
			msg:   "",
		},
		{
			name:  "not JSON",
			doc:   `{`,
			field: "",
			msg:   "not valid JSON",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			probs := problems(t, c.doc, c.opts)
			if !hasProblem(probs, c.field, c.msg) {
				t.Fatalf("want problem at %q containing %q, got %v", c.field, c.msg, probs)
			}
		})
	}
}

func TestSameVerdictTwice(t *testing.T) {
	doc := `{"payer_account_id":"123456789012","scopes":[{"account_id":"123456789012","ceiling_usd":1}]}`
	_, e1 := Parse([]byte(doc), Options{})
	_, e2 := Parse([]byte(doc), Options{})
	if e1 == nil || e1.Error() != e2.Error() {
		t.Fatalf("verdicts differ: %v / %v", e1, e2)
	}
}

func TestExcludedAccounts(t *testing.T) {
	ok := `{"payer_account_id":"123456789012","excluded_accounts":["999988887777"],
		"scopes":[{"ou_id":"ou-ab12-cd34ef56","ceiling_usd":1}]}`
	d := mustParse(t, ok)
	r := d.Resolve()
	if !r.Excluded("999988887777") || !r.Excluded("123456789012") || r.Excluded("111122223333") {
		t.Fatalf("excluded: %v (payer must always count as excluded)", r.ExcludedAccounts)
	}

	both := `{"payer_account_id":"123456789012","excluded_accounts":["111122223333"],
		"scopes":[{"account_id":"111122223333","ceiling_usd":1}]}`
	if !hasProblem(problems(t, both, Options{}), "excluded_accounts[0]", "both protected and excluded") {
		t.Fatal("an account both excluded and scoped must be invalid")
	}

	dup := `{"payer_account_id":"123456789012","excluded_accounts":["111122223333","111122223333"],"scopes":[]}`
	if !hasProblem(problems(t, dup, Options{}), "excluded_accounts[1]", "listed twice") {
		t.Fatal("a duplicate exclusion must be invalid")
	}

	bad := `{"payer_account_id":"123456789012","excluded_accounts":["123"],"scopes":[]}`
	if !hasProblem(problems(t, bad, Options{}), "excluded_accounts[0]", "12-digit") {
		t.Fatal("a malformed exclusion must be invalid")
	}
}

func TestAlertEmails(t *testing.T) {
	d := mustParse(t, `{"payer_account_id":"123456789012","scopes":[],"alert_emails":["OnCall@Example.com","finops@example.com"]}`)
	got := d.Resolve().AlertEmails
	if strings.Join(got, ",") != "finops@example.com,oncall@example.com" {
		t.Errorf("resolved recipients = %v, want lowercased and sorted", got)
	}

	d = mustParse(t, `{"payer_account_id":"123456789012","scopes":[]}`)
	if got := d.Resolve().AlertEmails; got != nil {
		t.Errorf("absent alert_emails resolved to %v, want none", got)
	}

	probs := problems(t, `{"payer_account_id":"123456789012","scopes":[],"alert_emails":["ops@example.com","OPS@example.com"]}`, Options{})
	if !hasProblem(probs, "alert_emails[1]", "already listed at alert_emails[0]") {
		t.Errorf("case-insensitive duplicate not refused: %v", probs)
	}
}
