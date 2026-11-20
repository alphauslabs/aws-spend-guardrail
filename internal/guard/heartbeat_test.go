package guard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// heartbeats returns every heartbeat's detail, oldest first.
func (h *harness) heartbeats() []map[string]any {
	h.t.Helper()
	var out []map[string]any
	for _, e := range h.events.list {
		if e.Kind != schema.KindHeartbeat {
			continue
		}
		var d map[string]any
		if err := json.Unmarshal(e.Detail, &d); err != nil {
			h.t.Fatal(err)
		}
		d["_id"] = e.ID
		out = append(out, d)
	}
	return out
}

func (h *harness) lastHeartbeat() map[string]any {
	h.t.Helper()
	hb := h.heartbeats()
	if len(hb) == 0 {
		h.t.Fatalf("no heartbeat: %v", h.events.kinds())
	}
	return hb[len(hb)-1]
}

func scopeIn(hb map[string]any, target string) map[string]any {
	for _, s := range hb["scopes"].([]any) {
		m := s.(map[string]any)
		if m["target"] == target {
			return m
		}
	}
	return nil
}

func TestHeartbeatListsAFrozenScopeAndItsLift(t *testing.T) {
	h := newHarness(t)
	freezeX(h, 46000)
	hb := h.lastHeartbeat()
	if hb["reason"] != HeartbeatChange || hb["state_layout"] != float64(StateLayout) || hb["ai"] != nil {
		t.Fatalf("heartbeat: %v", hb)
	}
	s := scopeIn(hb, tX.String())
	if s == nil || s["ceiling_usd"] != float64(50000) || s["threshold_pct"] != float64(90) || s["mode"] != "enforce" || s["actual_usd"] != float64(46000) {
		t.Fatalf("scope: %v", s)
	}
	frozen, _ := s["frozen"].(map[string]any)
	if frozen == nil || frozen["since"] != h.now.UTC().Format(time.RFC3339) || frozen["how"] == "" || frozen["policy_id"] != "p-styleB" {
		t.Fatalf("frozen: %v", frozen)
	}

	// A raise above spend lifts it, and the next heartbeat says so.
	h.publish(enforceDoc("60000", "stay_frozen"))
	h.pull()
	approveAndWait(h, "2", time.Hour)
	h.pull()
	if s := scopeIn(h.lastHeartbeat(), tX.String()); s["frozen"] != nil || s["ceiling_usd"] != float64(60000) {
		t.Fatalf("after the lift: %v", s)
	}
}

func TestHeartbeatNamesThePendingLooseningAndAccountsLeftOut(t *testing.T) {
	h := newHarness(t)
	h.publish(`{"payer_account_id":"123456789012","excluded_accounts":["999988887777"],"scopes":[{"account_id":"111122223333","ceiling_usd":50000}]}`)
	h.pull()
	if err := h.state.MarkReported(context.Background(), schema.KindUnprotected, "555566667777", h.now); err != nil {
		t.Fatal(err)
	}
	h.publish(`{"payer_account_id":"123456789012","excluded_accounts":["999988887777"],"scopes":[{"account_id":"111122223333","ceiling_usd":60000}]}`)
	h.pull()
	hb := h.lastHeartbeat()
	p, _ := hb["pending"].(map[string]any)
	id := h.st().Pending.ApprovalID
	if p == nil || p["version"] != float64(2) || p["approval_id"] != id || p["earliest_apply_at"] != h.now.Add(30*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("pending: %v", hb["pending"])
	}
	if ex := hb["excluded"].([]any); len(ex) != 1 || ex[0] != "999988887777" {
		t.Fatalf("excluded: %v", hb["excluded"])
	}
	un := hb["unprotected"].([]any)
	if len(un) != 1 || un[0].(map[string]any)["account"] != "555566667777" {
		t.Fatalf("unprotected: %v", hb["unprotected"])
	}
}

func TestHeartbeatCarriesTheAIGuardsCutoffs(t *testing.T) {
	h := newHarness(t)
	k := withFast(t, h)
	h.org.ous["ou-ab12-cd34ef56"] = []string{"444455556666"}
	h.publish(`{"payer_account_id":"123456789012","scopes":[],"ai":{"scopes":[{"ou_id":"ou-ab12-cd34ef56","hourly_usd":0.01,"mode":"enforce"}]}}`)
	h.pull()
	k.call(h, "444455556666", "us.anthropic.claude-opus-5-5", 1000, 1000)
	h.trail()
	ai, _ := h.lastHeartbeat()["ai"].(map[string]any)
	if ai == nil {
		t.Fatalf("no AI section: %v", h.lastHeartbeat())
	}
	cuts := ai["cutoffs"].([]any)
	if len(cuts) != 1 || cuts[0].(map[string]any)["account"] != "444455556666" || cuts[0].(map[string]any)["rule"] != "hourly_cap" {
		t.Fatalf("cutoffs: %v", ai["cutoffs"])
	}
	if trail := ai["trail"].(map[string]any); trail["stalled"] != false {
		t.Fatalf("trail: %v", trail)
	}

	// The trail stalling is a change, and the next heartbeat says so.
	store := h.g.Fast.Engine.Store
	if err := store.PutMeta(context.Background(), "stall", map[string]any{"last_file": h.now.Add(-time.Hour), "stalled": true}); err != nil {
		t.Fatal(err)
	}
	before := len(h.heartbeats())
	h.g.heartbeatIfChanged(context.Background())
	if len(h.heartbeats()) != before+1 {
		t.Fatal("a stalled trail should write a heartbeat")
	}
	if trail := h.lastHeartbeat()["ai"].(map[string]any)["trail"].(map[string]any); trail["stalled"] != true {
		t.Fatalf("trail: %v", trail)
	}
}

func TestOnlyStateChangesWriteHeartbeats(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull() // the first run after an upgrade, or a first install: no digest yet
	if n := len(h.heartbeats()); n != 1 {
		t.Fatalf("want one heartbeat after the first run, got %d", n)
	}
	h.budgets.setActual(bX, 1234)
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	if n := len(h.heartbeats()); n != 1 {
		t.Fatalf("spend alone must not write a heartbeat, got %d", n)
	}
	// One version applied that also changes two scopes: one heartbeat.
	h.publish(doc("40000", "45000"))
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	hb := h.heartbeats()
	if len(hb) != 2 || hb[1]["applied_version"] != float64(2) || !strings.HasPrefix(hb[1]["_id"].(string), "heartbeat:") || hb[1]["_id"] == hb[0]["_id"] {
		t.Fatalf("want a second heartbeat for version 2 with its own id: %v", hb)
	}
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	if n := len(h.heartbeats()); n != 2 {
		t.Fatalf("a quiet run writes none, got %d", n)
	}
}

func TestDailyHeartbeatKeepsItsFieldsAndAddsTheState(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.daily()
	hb := h.lastHeartbeat()
	if !strings.HasPrefix(hb["_id"].(string), "heartbeat:"+h.now.Format("2006-01-02")+":") || hb["reason"] != HeartbeatDaily {
		t.Fatalf("daily heartbeat: %v", hb)
	}
	for _, f := range []string{"applied_version", "effective_version", "pending", "guard_version", "code_sha256", "scopes", "state_layout"} {
		if _, ok := hb[f]; !ok {
			t.Errorf("daily heartbeat lacks %s: %v", f, hb)
		}
	}
	// The daily one stored the digest, so the next quiet run writes nothing.
	n := len(h.heartbeats())
	h.now = h.now.Add(5 * time.Minute)
	h.pull()
	if len(h.heartbeats()) != n {
		t.Fatal("a quiet run after the daily heartbeat writes none")
	}
}

// The heartbeat's AI section carries blocks, mantle status, and the
// unknown-model policy, and a change to them writes a heartbeat
// (add-heartbeat-ai-status D1, D2).
func TestHeartbeatAIStatus(t *testing.T) {
	h, _ := blockedHarness(t)
	h.g.Fast.MantleRegions = []string{"us-east-1", "us-west-2"}
	h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56"}]}}`)
	h.pull()
	ai, _ := h.lastHeartbeat()["ai"].(map[string]any)
	if ai == nil || ai["unknown_models"] != "cutoff" {
		t.Fatalf("ai %v", ai)
	}
	blocked, _ := ai["blocked"].(map[string]any)
	accts, _ := blocked["accounts"].(map[string]any)
	if blocked["attached"] != true || accts["444455556666"] == nil || accts["555566667777"] == nil {
		t.Fatalf("blocked %v", blocked)
	}
	mantle, _ := ai["mantle"].(map[string]any)
	if regions, _ := mantle["regions"].([]any); len(regions) != 2 || regions[0].(map[string]any)["read"] != true {
		t.Fatalf("mantle %v", mantle)
	}

	// A region becomes unreadable: one change heartbeat, then none while it
	// stays so.
	n := len(h.heartbeats())
	if err := h.g.Fast.Engine.Store.PutMeta(context.Background(), "mantle.status", map[string]any{"failing": map[string]string{"us-west-2": "no CloudWatch observability sink"}}); err != nil {
		t.Fatal(err)
	}
	h.trail()
	if got := len(h.heartbeats()); got != n+1 {
		t.Fatalf("heartbeats %d, want %d", got, n+1)
	}
	west := h.lastHeartbeat()["ai"].(map[string]any)["mantle"].(map[string]any)["regions"].([]any)[1].(map[string]any)
	if west["region"] != "us-west-2" || west["read"] != false || west["error"] != "no CloudWatch observability sink" {
		t.Fatalf("us-west-2 %v", west)
	}
	h.trail()
	if got := len(h.heartbeats()); got != n+1 {
		t.Fatalf("an unchanged state wrote a heartbeat: %d", got)
	}

	// Without a block policy or watched regions, those fields are absent.
	h.g.Blocks, h.g.Fast.MantleRegions = nil, nil
	h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56"}],"unknown_models":"cutoff"}}`)
	h.pull()
	ai = h.lastHeartbeat()["ai"].(map[string]any)
	if _, ok := ai["blocked"]; ok {
		t.Fatalf("blocked without a block policy: %v", ai)
	}
	if _, ok := ai["mantle"]; ok {
		t.Fatalf("mantle without watched regions: %v", ai)
	}
}

func TestHeartbeatNamesTheAccountsItMentions(t *testing.T) {
	h := newHarness(t)
	names := map[string]string{
		"111122223333": "prod",
		"999988887777": "sandbox",
		"555566667777": "orphan",
		"444455556666": "not mentioned",
	}
	h.g.Names = func(context.Context) map[string]string { return names }
	h.publish(`{"payer_account_id":"123456789012","excluded_accounts":["999988887777"],"scopes":[{"account_id":"111122223333","ceiling_usd":50000}]}`)
	h.pull()
	if err := h.state.MarkReported(context.Background(), schema.KindUnprotected, "555566667777", h.now); err != nil {
		t.Fatal(err)
	}
	h.g.heartbeatIfChanged(context.Background())
	got, _ := h.lastHeartbeat()["account_names"].(map[string]any)
	want := map[string]any{"111122223333": "prod", "999988887777": "sandbox", "555566667777": "orphan"}
	if len(got) != len(want) {
		t.Fatalf("account_names %v, want %v", got, want)
	}
	for id, n := range want {
		if got[id] != n {
			t.Fatalf("account_names %v, want %v", got, want)
		}
	}

	// A rename alone writes no heartbeat.
	n := len(h.heartbeats())
	names["111122223333"] = "production"
	h.g.heartbeatIfChanged(context.Background())
	if len(h.heartbeats()) != n {
		t.Fatal("a rename alone wrote a heartbeat")
	}

	// With no names known, the next heartbeat is written with ids only.
	h.g.Names = func(context.Context) map[string]string { return nil }
	h.publish(`{"payer_account_id":"123456789012","excluded_accounts":["999988887777"],"scopes":[{"account_id":"111122223333","ceiling_usd":40000}]}`)
	h.pull()
	hb := h.lastHeartbeat()
	if len(h.heartbeats()) != n+1 {
		t.Fatal("want a heartbeat for the new version")
	}
	if _, ok := hb["account_names"]; ok {
		t.Fatalf("account_names without names: %v", hb["account_names"])
	}
}

func TestHeartbeatNamesAIAccounts(t *testing.T) {
	h, _ := blockedHarness(t)
	h.g.Fast.MantleRegions = []string{"us-east-1"}
	h.g.Names = func(context.Context) map[string]string {
		return map[string]string{"444455556666": "blocked one", "777788889999": "unlinked one"}
	}
	if err := h.state.MarkReported(context.Background(), schema.KindMantleUnlinked, "777788889999@us-east-1", h.now); err != nil {
		t.Fatal(err)
	}
	h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56"}]}}`)
	h.pull()
	got, _ := h.lastHeartbeat()["account_names"].(map[string]any)
	if got["444455556666"] != "blocked one" || got["777788889999"] != "unlinked one" {
		t.Fatalf("account_names %v", got)
	}
}

// A second daily run on a day writes its own heartbeat, under its own id; the
// same detail always gives the same id (fix-outbox-delivery D3).
func TestSecondDailyRunWritesItsOwnHeartbeat(t *testing.T) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.daily()
	first := h.lastHeartbeat()["_id"]
	h.publish(doc("40000", "50000")) // a tightening: applies at once
	h.pull()
	h.now = h.now.Add(time.Hour)
	h.daily()
	second := h.lastHeartbeat()["_id"]
	if first == second {
		t.Fatalf("two daily heartbeats with different state share the id %v", first)
	}
	day := "heartbeat:" + h.now.Format("2006-01-02") + ":"
	for _, id := range []any{first, second} {
		if s, _ := id.(string); !strings.HasPrefix(s, day) || len(s) != len(day)+12 {
			t.Fatalf("id %v", id)
		}
	}
	d := map[string]any{"reason": HeartbeatDaily, "applied_version": 3}
	if dailyHeartbeatID("2026-11-20", d) != dailyHeartbeatID("2026-11-20", map[string]any{"applied_version": 3, "reason": HeartbeatDaily}) {
		t.Error("the same detail must give the same id")
	}
}
