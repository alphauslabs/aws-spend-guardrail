package guard

import (
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

func recipientsDoc(emails ...string) string {
	list := `[]`
	if len(emails) > 0 {
		list = `["` + strings.Join(emails, `","`) + `"]`
	}
	return `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":50000}],"alert_emails":` + list + `}`
}

// withRecipients applies a first version without recipients, then version 2
// listing them, approved. Recipients need approval even in a first config, so
// later versions in these tests start at 3.
func withRecipients(t *testing.T, emails ...string) *harness {
	h := newHarness(t)
	h.publish(recipientsDoc())
	h.pull()
	if len(emails) == 0 {
		h.nextVer++ // keep version numbers the same in every test
		return h
	}
	h.publish(recipientsDoc(emails...))
	h.pull()
	approveAndWait(h, "2", 31*time.Minute)
	h.pull()
	return h
}

func TestFirstConfigRecipientsNeedApproval(t *testing.T) {
	h := newHarness(t)
	h.publish(recipientsDoc("finops@example.com"))
	h.pull()
	if len(h.subs.subscribes) != 0 || h.ceiling(bX) != 50000 {
		t.Fatalf("subscribes %v; the scopes should apply at once", h.subs.subscribes)
	}
	if a := h.jennah.Approval("guardrail:123456789012:msp_a:v1"); a == nil {
		t.Fatal("no approval raised for a first config's recipients")
	}
}

func TestFirstConfigSubscribesRecipientsOnce(t *testing.T) {
	h := withRecipients(t, "finops@example.com")
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	if len(h.subs.subscribes) != 1 || h.subs.subscribes[0] != "finops@example.com" {
		t.Fatalf("subscribes: %v", h.subs.subscribes)
	}
	if h.events.count(schema.KindRecipientSubscribed) != 1 {
		t.Fatalf("events: %v", h.events.kinds())
	}
}

func TestPendingRecipientIsAskedAgainOnlyAfterADay(t *testing.T) {
	h := withRecipients(t, "finops@example.com")
	h.now = h.now.Add(23 * time.Hour)
	h.pull()
	if len(h.subs.subscribes) != 1 || h.events.count(schema.KindRecipientUnconfirmed) != 0 {
		t.Fatalf("asked again too soon: %v, events %v", h.subs.subscribes, h.events.kinds())
	}
	h.now = h.now.Add(2 * time.Hour)
	h.pull()
	if len(h.subs.subscribes) != 2 {
		t.Fatalf("want a second ask after a day, got %v", h.subs.subscribes)
	}
	h.now = h.now.Add(time.Hour)
	h.pull()
	if len(h.subs.subscribes) != 2 {
		t.Fatalf("asked a third time within the day: %v", h.subs.subscribes)
	}
}

func TestUnconfirmedIsReportedDaily(t *testing.T) {
	h := withRecipients(t, "finops@example.com")
	for i := 0; i < 287; i++ { // a day of 5-minute runs, less one
		h.now = h.now.Add(5 * time.Minute)
		h.pull()
	}
	if n := h.events.count(schema.KindRecipientUnconfirmed); n != 0 || len(h.subs.subscribes) != 1 {
		t.Fatalf("within the first day: %d reports, asks %v", n, h.subs.subscribes)
	}
	for day := 1; day <= 2; day++ {
		for i := 0; i < 288; i++ {
			h.now = h.now.Add(5 * time.Minute)
			h.pull()
		}
		if n := h.events.count(schema.KindRecipientUnconfirmed); n != day || len(h.subs.subscribes) != day+1 {
			t.Fatalf("after day %d: %d reports, asks %v", day, n, h.subs.subscribes)
		}
	}
	h.subs.confirm("finops@example.com")
	h.now = h.now.Add(48 * time.Hour)
	h.pull()
	if n := h.events.count(schema.KindRecipientUnconfirmed); n != 2 {
		t.Fatalf("reported after confirming: %d", n)
	}
}

func TestRemovedRecipientIsUnsubscribedAfterApproval(t *testing.T) {
	h := withRecipients(t, "finops@example.com")
	h.subs.confirm("finops@example.com")
	h.publish(recipientsDoc())
	h.pull()
	if len(h.subs.unsubscribes) != 0 {
		t.Fatal("removed before approval")
	}
	approveAndWait(h, "3", 31*time.Minute)
	h.pull()
	if len(h.subs.unsubscribes) != 1 || h.subs.unsubscribes[0] != "finops@example.com" {
		t.Fatalf("unsubscribes: %v", h.subs.unsubscribes)
	}
	if h.events.count(schema.KindRecipientRemoved) != 1 || len(h.st().OwnSubscriptions) != 0 {
		t.Fatalf("events %v own %v", h.events.kinds(), h.st().OwnSubscriptions)
	}
}

func TestAddedRecipientWaitsForApproval(t *testing.T) {
	h := withRecipients(t)
	h.publish(recipientsDoc("finops@example.com"))
	h.pull()
	if len(h.subs.subscribes) != 0 {
		t.Fatal("subscribed before approval")
	}
	a := h.jennah.Approval("guardrail:123456789012:msp_a:v3")
	if a == nil || !strings.HasPrefix(a.GetTitle(), "Change alert recipients for spend guard config v3") {
		t.Fatalf("approval title: %q", a.GetTitle())
	}
	if !strings.Contains(a.GetSummary(), "alert recipient added: finops@example.com") {
		t.Fatalf("approval summary: %q", a.GetSummary())
	}
	approveAndWait(h, "3", 31*time.Minute)
	h.pull()
	if len(h.subs.subscribes) != 1 {
		t.Fatalf("subscribes: %v", h.subs.subscribes)
	}
}

func TestMixedVersionKeepsTheLoosenTitle(t *testing.T) {
	h := withRecipients(t)
	h.publish(`{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":60000}],"alert_emails":["finops@example.com"]}`)
	h.pull()
	a := h.jennah.Approval("guardrail:123456789012:msp_a:v3")
	if a == nil || !strings.HasPrefix(a.GetTitle(), "Loosen spend guard config v3") {
		t.Fatalf("approval title: %q", a.GetTitle())
	}
}

func TestStackAndOutsideSubscriptionsAreNeverRemoved(t *testing.T) {
	h := newHarness(t)
	h.subs.subscribed("stack@example.com")
	h.subs.subscribed("noc@example.com")      // added by hand
	h.subs.subscribed("existing@example.com") // there before config listed it
	h.publish(recipientsDoc())
	h.pull()
	h.publish(recipientsDoc("stack@example.com", "existing@example.com"))
	h.pull()
	approveAndWait(h, "2", 31*time.Minute)
	h.pull()
	if len(h.subs.subscribes) != 0 {
		t.Fatalf("subscribed addresses that already had subscriptions: %v", h.subs.subscribes)
	}
	if len(h.st().OwnSubscriptions) != 0 {
		t.Fatalf("recorded others' subscriptions as its own: %v", h.st().OwnSubscriptions)
	}
	h.publish(recipientsDoc())
	h.pull()
	approveAndWait(h, "3", 31*time.Minute)
	h.pull()
	if len(h.subs.unsubscribes) != 0 {
		t.Fatalf("unsubscribed: %v", h.subs.unsubscribes)
	}
}

func TestStackAddressIsNeverSubscribedByTheGuard(t *testing.T) {
	h := withRecipients(t, "Stack@example.com")
	if len(h.subs.subscribes) != 0 || len(h.st().OwnSubscriptions) != 0 {
		t.Fatalf("the stack's address is the stack's: %v", h.subs.subscribes)
	}
}

func TestRemovedWhilePendingThenConfirmedIsUnsubscribed(t *testing.T) {
	h := withRecipients(t, "finops@example.com")
	h.publish(recipientsDoc())
	h.pull()
	approveAndWait(h, "3", 31*time.Minute)
	h.pull()
	if len(h.subs.unsubscribes) != 0 || len(h.st().OwnSubscriptions) != 1 {
		t.Fatalf("pending: unsubscribes %v own %v", h.subs.unsubscribes, h.st().OwnSubscriptions)
	}
	h.subs.confirm("finops@example.com") // the old link is clicked
	h.pull()
	if len(h.subs.unsubscribes) != 1 || len(h.st().OwnSubscriptions) != 0 {
		t.Fatalf("confirmed later: unsubscribes %v own %v", h.subs.unsubscribes, h.st().OwnSubscriptions)
	}
}

func TestRecipientsUnchangedWhileJennahIsDown(t *testing.T) {
	h := withRecipients(t, "finops@example.com")
	h.subs.confirm("finops@example.com")
	h.jennah.FailReads = true
	h.now = h.now.Add(48 * time.Hour)
	h.pull()
	if len(h.subs.subscribes) != 1 || len(h.subs.unsubscribes) != 0 {
		t.Fatalf("subscribes %v unsubscribes %v", h.subs.subscribes, h.subs.unsubscribes)
	}
	if _, ok := h.subs.byAddr["finops@example.com"]; !ok {
		t.Fatal("recipient lost its subscription during the outage")
	}
}
