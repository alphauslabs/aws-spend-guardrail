package guard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// org with the two scoped accounts from doc(), one OU, and extra accounts.
func coverageHarness(t *testing.T, extra ...Account) *harness {
	h := newHarness(t)
	h.org.accounts = append([]Account{
		{ID: "111122223333", Name: "acme-prod", State: "ACTIVE"},
		{ID: "444455556666", Name: "acme-dev", State: "ACTIVE"},
		{ID: payer, Name: "payer", State: "ACTIVE"},
	}, extra...)
	return h
}

func daily(h *harness) {
	h.t.Helper()
	if err := h.g.Daily(context.Background()); err != nil {
		h.t.Fatalf("daily: %v", err)
	}
}

func eventsOf(h *harness, kind string) []string {
	var ids []string
	for _, e := range h.events.list {
		if e.Kind == kind {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func TestNewAccountIsReportedOnce(t *testing.T) {
	h := coverageHarness(t, Account{ID: "555566667777", Name: "new-customer", State: "ACTIVE"})
	h.publish(doc("50000", "50000"))
	h.pull()
	daily(h)
	got := eventsOf(h, schema.KindUnprotected)
	if len(got) != 1 || !strings.HasPrefix(got[0], "unprotected_account:555566667777:") {
		t.Fatalf("events %v", got)
	}
	if !h.alerts.has("1 unprotected account(s)") || !h.alerts.has("555566667777 (new-customer, under root)") {
		t.Fatalf("alert should name the account, its name and parent: %v", h.alerts.list)
	}
	h.now = h.now.Add(24 * time.Hour)
	daily(h)
	if n := len(eventsOf(h, schema.KindUnprotected)); n != 1 {
		t.Fatalf("a second day must report nothing new, got %d events", n)
	}
	if n := countAlerts(h, "unprotected account"); n != 1 {
		t.Fatalf("one alert only, got %d", n)
	}
}

func TestPayerAndExcludedAreNeverReported(t *testing.T) {
	h := coverageHarness(t, Account{ID: "999988887777", Name: "sso", State: "ACTIVE"})
	h.publish(`{"payer_account_id":"123456789012","excluded_accounts":["999988887777"],"scopes":[
		{"account_id":"111122223333","ceiling_usd":50000},
		{"account_id":"444455556666","ceiling_usd":50000}]}`)
	h.pull()
	daily(h)
	if got := eventsOf(h, schema.KindUnprotected); len(got) != 0 {
		t.Fatalf("payer and excluded accounts must not be reported: %v", got)
	}
}

func TestCoveredThroughAnOU(t *testing.T) {
	h := coverageHarness(t, Account{ID: "222233334444", Name: "in-ou", State: "ACTIVE"})
	h.org.ous["ou-ab12-cd34ef56"] = []string{"222233334444"}
	h.publish(`{"payer_account_id":"123456789012","scopes":[
		{"account_id":"111122223333","ceiling_usd":1},{"account_id":"444455556666","ceiling_usd":1},
		{"ou_id":"ou-ab12-cd34ef56","ceiling_usd":1}]}`)
	h.pull()
	daily(h)
	if got := eventsOf(h, schema.KindUnprotected); len(got) != 0 {
		t.Fatalf("an account in a scoped OU is covered: %v", got)
	}
}

func TestSuspendedAccountIsIgnored(t *testing.T) {
	h := coverageHarness(t, Account{ID: "888899990000", Name: "old", State: "SUSPENDED"})
	h.publish(doc("50000", "50000"))
	h.pull()
	daily(h)
	if got := eventsOf(h, schema.KindUnprotected); len(got) != 0 {
		t.Fatalf("suspended accounts are ignored: %v", got)
	}
}

func TestCoverThenUncoverReportsAgain(t *testing.T) {
	h := coverageHarness(t, Account{ID: "555566667777", Name: "new-customer", State: "ACTIVE"})
	h.publish(doc("50000", "50000"))
	h.pull()
	daily(h) // reported
	h.publish(`{"payer_account_id":"123456789012","scopes":[
		{"account_id":"111122223333","ceiling_usd":50000},{"account_id":"444455556666","ceiling_usd":50000},
		{"account_id":"555566667777","ceiling_usd":100}]}`)
	h.pull()
	h.now = h.now.Add(24 * time.Hour)
	daily(h)                         // covered now: forgotten
	h.publish(doc("50000", "50000")) // scope removed again (a loosening)
	h.pull()
	approveAndWait(h, "3", time.Hour)
	h.pull()
	h.now = h.now.Add(24 * time.Hour)
	daily(h)
	if n := len(eventsOf(h, schema.KindUnprotected)); n != 2 {
		t.Fatalf("an account uncovered again must be reported again, got %d events", n)
	}
}

func TestScopedAccountThatLeftIsStale(t *testing.T) {
	h := newHarness(t)
	h.org.accounts = []Account{{ID: "111122223333", Name: "acme-prod", State: "ACTIVE"}, {ID: payer, State: "ACTIVE"}}
	h.publish(doc("50000", "50000")) // also scopes 444455556666, which isn't in the org
	h.pull()
	daily(h)
	daily(h)
	got := eventsOf(h, schema.KindStaleScope)
	if len(got) != 1 || !strings.Contains(got[0], "444455556666") || !h.alerts.has("stale scope") {
		t.Fatalf("stale events %v alerts %v", got, h.alerts.list)
	}
}

func TestFirstRunWithManyAccountsSendsOneAlert(t *testing.T) {
	var extra []Account
	for i := 0; i < 25; i++ {
		extra = append(extra, Account{ID: "5555666677" + string(rune('0'+i/10)) + string(rune('0'+i%10)), State: "ACTIVE"})
	}
	h := coverageHarness(t, extra...)
	h.publish(doc("50000", "50000"))
	h.pull()
	daily(h)
	if n := len(eventsOf(h, schema.KindUnprotected)); n != 25 {
		t.Fatalf("one event per account, got %d", n)
	}
	if n := countAlerts(h, "unprotected account"); n != 1 || !h.alerts.has("25 unprotected account(s)") {
		t.Fatalf("one combined alert, got %d: %v", n, h.alerts.list)
	}
}

func TestNoConfigNoCoverageReports(t *testing.T) {
	h := coverageHarness(t, Account{ID: "555566667777", State: "ACTIVE"})
	daily(h)
	if n := len(eventsOf(h, schema.KindUnprotected)); n != 0 {
		t.Fatal("without a config there is nothing to compare with")
	}
}
