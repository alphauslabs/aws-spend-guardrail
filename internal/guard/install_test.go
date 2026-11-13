package guard

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	approvalv1 "github.com/alphauslabs/jennah-sdk-go/jennah/approval/v1"
)

// deliver writes the events the guard queued into the fake Jennah's inbox, as
// the outbox consumer would.
func (h *harness) deliver() {
	h.t.Helper()
	for _, e := range h.events.list {
		if err := h.client.CommitEvent(context.Background(), schema.InboxDatasetID(msp, payer), e); err != nil {
			h.t.Fatal(err)
		}
	}
}

// reinstall delivers the old guard's events, then deletes its state, as
// deleting and recreating the stack does, and starts a new guard.
func (h *harness) reinstall() {
	h.t.Helper()
	h.deliver()
	h.now = h.now.Add(time.Minute)
	h.state = newMemState()
	h.events = &events{}
	h.g = h.newGuard()
}

// installEvent returns the guard_installed event's detail, failing unless
// there is exactly one.
func (h *harness) installEvent() map[string]any {
	h.t.Helper()
	var found []jennah.Event
	for _, e := range h.events.list {
		if e.Kind == schema.KindGuardInstalled {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		h.t.Fatalf("want one guard_installed event, got %d: %v", len(found), h.events.kinds())
	}
	var d map[string]any
	if err := json.Unmarshal(found[0].Detail, &d); err != nil {
		h.t.Fatal(err)
	}
	return d
}

func eventIDs(list []jennah.Event, kind string) []string {
	var out []string
	for _, e := range list {
		if e.Kind == kind {
			out = append(out, e.ID)
		}
	}
	return out
}

func TestFirstInstallAppliesAtOnceAndIsRecorded(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	if h.st().AppliedVersion != 1 || h.ceiling(bX) != 50000 || h.jennah.CreatedApprovals != 0 {
		t.Fatalf("state %+v", h.st())
	}
	if d := h.installEvent(); d["outcome"] != InstallFresh || d["previous_applied_version"] != float64(0) || d["latest_version"] != float64(1) {
		t.Fatalf("guard_installed: %v", d)
	}
	if len(h.alerts.list) != 0 {
		t.Fatalf("no alert for a first install: %v", h.alerts.list)
	}
}

func TestReinstallKeepsAPendingLooseningWaiting(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()
	if h.ceiling(bY) != 50000 || h.jennah.CreatedApprovals != 1 {
		t.Fatalf("v2 should wait for approval: ceiling %v, approvals %d", h.ceiling(bY), h.jennah.CreatedApprovals)
	}

	h.reinstall()
	h.pull()
	if h.ceiling(bY) != 50000 || h.st().AppliedVersion != 1 {
		t.Fatalf("the reinstall must put v1 back, not apply v2: ceiling %v, state %+v", h.ceiling(bY), h.st())
	}
	if h.jennah.CreatedApprovals != 1 || h.st().Pending == nil || h.st().Pending.Version != 2 {
		t.Fatalf("the existing approval should be reused: approvals %d, pending %+v", h.jennah.CreatedApprovals, h.st().Pending)
	}
	if d := h.installEvent(); d["outcome"] != InstallResumed || d["previous_applied_version"] != float64(1) || d["latest_version"] != float64(2) {
		t.Fatalf("guard_installed: %v", d)
	}

	// The delay counts from the new guard's first sight of v2.
	approveAndWait(h, "2", 20*time.Minute)
	h.pull()
	if h.ceiling(bY) != 50000 {
		t.Fatal("the delay restarts at the reinstall")
	}
	h.now = h.now.Add(15 * time.Minute)
	h.pull()
	if h.ceiling(bY) != 60000 || h.st().AppliedVersion != 2 {
		t.Fatalf("approved and delayed: ceiling %v, state %+v", h.ceiling(bY), h.st())
	}
}

func TestReinstallAfterARejectedLooseningKeepsTheOldVersion(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()
	a := h.jennah.Approval("guardrail:123456789012:msp_a:v2")
	h.jennah.Decide(a.GetApprovalId(), approvalv1.ApprovalStatus_APPROVAL_STATUS_REJECTED)
	h.pull()

	h.reinstall()
	h.pull()
	h.now = h.now.Add(time.Hour)
	h.pull()
	if h.ceiling(bY) != 50000 || h.st().AppliedVersion != 1 || h.jennah.CreatedApprovals != 1 {
		t.Fatalf("v2 stays rejected: ceiling %v, state %+v, approvals %d", h.ceiling(bY), h.st(), h.jennah.CreatedApprovals)
	}
}

func TestReinstallWithNothingPendingAppliesAtOnceAndIsRecordedAgain(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()
	approveAndWait(h, "2", 31*time.Minute)
	h.pull()
	before := eventIDs(h.events.list, schema.KindConfigApplied)

	h.reinstall()
	h.pull()
	if h.ceiling(bY) != 60000 || h.st().AppliedVersion != 2 || h.jennah.CreatedApprovals != 1 {
		t.Fatalf("v2 was in force: ceiling %v, state %+v, approvals %d", h.ceiling(bY), h.st(), h.jennah.CreatedApprovals)
	}
	if d := h.installEvent(); d["outcome"] != InstallResumed || d["previous_applied_version"] != float64(2) {
		t.Fatalf("guard_installed: %v", d)
	}
	after := eventIDs(h.events.list, schema.KindConfigApplied)
	if len(after) != 1 || len(before) == 0 || after[0] == before[len(before)-1] || !strings.HasPrefix(after[0], "config_applied:2:i") {
		t.Fatalf("the reinstall's config_applied must have its own ID: before %v, after %v", before, after)
	}
}

type deniedInbox struct{}

func (deniedInbox) LatestEvent(context.Context, string, string) (*jennah.Event, error) {
	return nil, errors.New("read latest config_applied event: PermissionDenied: dataset guardrail.msp_a.123456789012.inbox")
}

func TestReinstallWithoutInboxReachAlertsAndAppliesTheLatest(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()

	h.reinstall()
	h.g.Inbox = deniedInbox{}
	h.pull()
	h.pull()
	if h.ceiling(bY) != 60000 || h.st().AppliedVersion != 2 {
		t.Fatalf("without reach the latest applies, as before this change: ceiling %v, state %+v", h.ceiling(bY), h.st())
	}
	n := 0
	for _, a := range h.alerts.list {
		if strings.Contains(a, "reinstall check not done") {
			n++
			if !strings.Contains(a, "inbox in its reach") {
				t.Errorf("the alert should say how to fix the key: %s", a)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want one alert, got %d: %v", n, h.alerts.list)
	}
	if d := h.installEvent(); d["outcome"] != InstallNotDone {
		t.Fatalf("guard_installed: %v", d)
	}
}

func TestInstallCheckWaitsForJennah(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.jennah.FailReads = true
	h.pull()
	if h.events.count(schema.KindGuardInstalled) != 0 || h.st().InstallChecked {
		t.Fatal("the check can't run while Jennah is unreachable")
	}
	h.jennah.FailReads = false
	h.pull()
	if d := h.installEvent(); d["outcome"] != InstallFresh {
		t.Fatalf("guard_installed: %v", d)
	}
}

func TestRestartIsNotAnInstall(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.g = h.newGuard() // same state, as a function restart
	h.publish(doc("40000", "50000"))
	h.pull()
	if n := h.events.count(schema.KindGuardInstalled); n != 1 {
		t.Fatalf("want the one guard_installed from the first run, got %d", n)
	}
}

func TestStateFromBeforeInstallIDsKeepsTheOldEventIDs(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	st := h.st()
	st.InstallID, st.InstallChecked = 0, false // as saved by an older guard
	if err := h.state.Save(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	h.events = &events{}
	h.g = h.newGuard()
	h.publish(doc("40000", "50000"))
	h.pull()
	if ids := eventIDs(h.events.list, schema.KindConfigApplied); len(ids) != 1 || ids[0] != "config_applied:2" {
		t.Fatalf("an upgraded guard keeps the old IDs: %v", ids)
	}
	if h.events.count(schema.KindGuardInstalled) != 0 {
		t.Fatal("an upgrade is not an install")
	}
}

func raisedDetail(h *harness) map[string]any {
	h.t.Helper()
	for _, e := range h.events.list {
		if e.Kind == schema.KindLooseningRaised {
			var d map[string]any
			if err := json.Unmarshal(e.Detail, &d); err != nil {
				h.t.Fatal(err)
			}
			return d
		}
	}
	h.t.Fatalf("no loosening_raised event: %v", h.events.kinds())
	return nil
}

func TestReinstallAdoptsTheEarlierApproval(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()
	first := h.st().Pending.ApprovalID

	h.reinstall()
	h.pull()
	if p := h.st().Pending; p == nil || p.ApprovalID != first || h.jennah.CreatedApprovals != 1 {
		t.Fatalf("want the earlier approval %s adopted, got pending %+v and %d approvals", first, p, h.jennah.CreatedApprovals)
	}
	if d := raisedDetail(h); d["adopted"] != true || d["approval_id"] != first {
		t.Fatalf("loosening_raised should record the adoption: %v", d)
	}
	if h.alerts.has("can't apply its config") {
		t.Fatalf("adoption must not fail the config refresh: %v", h.alerts.list)
	}
}

func TestReinstallWithoutTheEarlierRecordRaisesANewApproval(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()

	// The old guard's loosening_raised never reached the inbox.
	var kept []jennah.Event
	for _, e := range h.events.list {
		if e.Kind != schema.KindLooseningRaised {
			kept = append(kept, e)
		}
	}
	h.events.list = kept
	h.reinstall()
	h.pull()
	p := h.st().Pending
	if p == nil || h.jennah.CreatedApprovals != 2 {
		t.Fatalf("want a new approval, got pending %+v and %d approvals", p, h.jennah.CreatedApprovals)
	}
	if a := h.jennah.Approval("guardrail:123456789012:msp_a:v2:i" + strconv.FormatInt(h.st().InstallID, 10)); a == nil || a.GetApprovalId() != p.ApprovalID {
		t.Fatalf("the new approval should use an install-scoped key: %v", a)
	}
	if raisedDetail(h)["adopted"] != nil {
		t.Fatal("a new approval is not an adoption")
	}
	if h.alerts.has("can't apply its config") {
		t.Fatalf("the config refresh keeps working: %v", h.alerts.list)
	}
}

func TestAnIdenticalRaiseStillReplays(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("50000", "60000"))
	h.pull()
	st := h.st()
	changes := config.Diff(st.Effective, st.Pending.Desired)
	req, err := h.g.approvalRequest(context.Background(), 2, st.Pending.Note, changes, st.Effective, st.Pending.Desired, st.Pending.FirstSeen)
	if err != nil {
		t.Fatal(err)
	}
	// A retry after a lost response sends the same request: Jennah replays it.
	a, err := h.client.RaiseApproval(context.Background(), req)
	if err != nil || a.ID != st.Pending.ApprovalID || h.jennah.CreatedApprovals != 1 {
		t.Fatalf("an identical request replays the first approval: %v, %v, %d approvals", a, err, h.jennah.CreatedApprovals)
	}
}
