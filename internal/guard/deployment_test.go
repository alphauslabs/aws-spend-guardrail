package guard

import (
	"context"
	"testing"
)

func TestTemplateRevisionCheck(t *testing.T) {
	type tcase struct {
		have  int
		alert bool
		text  string
	}
	cases := []tcase{
		{0, true, "no revision (a template from before revisions were recorded)"},
		{MinTemplateRevision, false, ""},
		{MinTemplateRevision + 1, false, ""}, // a rollback onto a newer template is fine
	}
	if MinTemplateRevision > 1 {
		cases = append(cases, tcase{MinTemplateRevision - 1, true, ""})
	}
	for _, tc := range cases {
		h := newHarness(t)
		h.g.S.GuardVersion, h.g.S.TemplateRevision = "v0.2.0", tc.have
		h.g.CheckDeployment(context.Background())
		h.g.CheckDeployment(context.Background()) // a second cold start: no repeat
		if n := countAlerts(h, "stack template is older than this guard needs"); (n == 1) != tc.alert || n > 1 {
			t.Errorf("revision %d: %d alerts: %v", tc.have, n, h.alerts.list)
		}
		if tc.text != "" && !h.alerts.has(tc.text) {
			t.Errorf("revision %d: alert lacks %q: %v", tc.have, tc.text, h.alerts.list)
		}
		d := h.g.selfReport(nil)
		if _, needed := d["template_revision_needed"]; needed != tc.alert || d["template_revision"] != tc.have {
			t.Errorf("revision %d: self report %v", tc.have, d)
		}
	}
}

func TestStaleGuardVersionLabel(t *testing.T) {
	for _, tc := range []struct {
		stack, code string
		alert       bool
	}{
		{"v0.2.0", "v0.2.0", false},
		{"v0.1.0", "v0.2.0", true},
		{"dev", "v0.2.0", false},
		{"v0.1.0", "dev", false},
		{"", "v0.2.0", false},
	} {
		h := newHarness(t)
		h.g.S.TemplateRevision = MinTemplateRevision
		h.g.S.StackGuardVersion, h.g.S.GuardVersion = tc.stack, tc.code
		h.g.CheckDeployment(context.Background())
		h.g.CheckDeployment(context.Background())
		if n := countAlerts(h, "GuardVersion label is stale"); (n == 1) != tc.alert || n > 1 {
			t.Errorf("%q vs %q: %d alerts", tc.stack, tc.code, n)
		}
		d := h.g.selfReport(nil)
		if _, has := d["stack_guard_version"]; has != tc.alert || d["guard_version"] != tc.code {
			t.Errorf("%q vs %q: self report %v", tc.stack, tc.code, d)
		}
	}
	// A new pair after an update alerts again.
	h := newHarness(t)
	h.g.S.TemplateRevision = MinTemplateRevision
	h.g.S.StackGuardVersion, h.g.S.GuardVersion = "v0.1.0", "v0.2.0"
	h.g.CheckDeployment(context.Background())
	h.g.S.GuardVersion = "v0.3.0"
	h.g.CheckDeployment(context.Background())
	if n := countAlerts(h, "GuardVersion label is stale"); n != 2 {
		t.Errorf("%d alerts for two pairs", n)
	}
}

// The first run after the code changes writes a heartbeat, so an upgrade shows
// in Jennah at once (add-guard-upgrades).
func TestNewCodeWritesAHeartbeat(t *testing.T) {
	h := newHarness(t)
	h.g.S.CodeSHA256 = "old="
	freezeX(h, 46000)
	before := len(h.heartbeats())
	h.g.heartbeatIfChanged(context.Background())
	if len(h.heartbeats()) != before {
		t.Fatal("no change, yet a heartbeat was written")
	}
	h.g.S.CodeSHA256, h.g.S.GuardVersion = "new=", "v0.2.0"
	h.g.heartbeatIfChanged(context.Background())
	hb := h.lastHeartbeat()
	if len(h.heartbeats()) != before+1 || hb["code_sha256"] != "new=" || hb["guard_version"] != "v0.2.0" || hb["reason"] != HeartbeatChange {
		t.Fatalf("heartbeat after new code: %v", hb)
	}
}
