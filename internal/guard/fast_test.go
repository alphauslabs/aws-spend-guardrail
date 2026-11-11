package guard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity/activitytest"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

const trailBase = "AWSLogs/o-test/"

type fastKit struct {
	bucket   *activitytest.Bucket
	policies *activitytest.Policies
	n        int
}

func withFast(t *testing.T, h *harness) *fastKit {
	t.Helper()
	prices, err := ai.Prices()
	if err != nil {
		t.Fatal(err)
	}
	k := &fastKit{bucket: activitytest.NewBucket(), policies: activitytest.NewPolicies()}
	store := activitytest.NewStore()
	h.g.Fast = &FastPath{
		Engine:     &activity.Engine{Reader: &activity.Reader{Bucket: k.bucket, Store: store, Base: trailBase}, Store: store, Policies: k.policies, Now: func() time.Time { return h.now }},
		Prices:     prices,
		AIPolicyID: "p-ai",
	}
	return k
}

func (k *fastKit) call(h *harness, account, model string, in, out int64) {
	k.n++
	at := h.now.Add(-time.Minute)
	key := activitytest.TrailKey(trailBase, account, "us-east-1", at, string(rune('a'+k.n)))
	k.bucket.Put(key, activitytest.TrailFile(activitytest.BedrockCall(account, "us-east-1", model, "arn:aws:sts::"+account+":assumed-role/app/s", at, in, out, false)), h.now)
}

func (h *harness) trail() {
	h.t.Helper()
	if err := h.g.Trail(context.Background()); err != nil {
		h.t.Fatalf("trail: %v", err)
	}
}

func TestNoAISectionWatchesNothing(t *testing.T) {
	h := newHarness(t)
	k := withFast(t, h)
	h.publish(doc("50000", "50000"))
	h.pull()
	k.call(h, "111122223333", "us.amazon.nova-micro-v1:0", 1, 1)
	h.trail()
	if k.bucket.Gets != 0 {
		t.Fatal("the trail must not be read without an ai section")
	}
}

func TestAICapCutsOffThroughOUScope(t *testing.T) {
	h := newHarness(t)
	k := withFast(t, h)
	h.org.ous["ou-ab12-cd34ef56"] = []string{"444455556666"}
	h.publish(`{"payer_account_id":"123456789012","scopes":[],"ai":{"scopes":[{"ou_id":"ou-ab12-cd34ef56","hourly_usd":0.01,"mode":"enforce"}]}}`)
	h.pull()
	// Claude Opus 5.5: $0.0044 in and $0.022 out per 1,000 tokens.
	k.call(h, "444455556666", "us.anthropic.claude-opus-5-5", 1000, 1000)
	h.trail()
	if !k.policies.Attached["p-ai/444455556666"] {
		t.Fatalf("not cut off: %v", k.policies.Calls)
	}
	var detail map[string]any
	for _, e := range h.events.list {
		if e.Kind == schema.KindCutoff {
			_ = json.Unmarshal(e.Detail, &detail)
			if e.ConfigVersion != 1 || e.Scope != "account:444455556666" {
				t.Fatalf("event %+v", e)
			}
		}
	}
	if detail["rule"] != "hourly_cap" || !strings.Contains(detail["detail"].(string), "0.03") {
		t.Fatalf("cutoff detail %v", detail)
	}
	if !h.alerts.has("444455556666 cut off") {
		t.Fatalf("no local alert: %v", h.alerts.list)
	}
}

func TestAIScopeAccountBeatsOU(t *testing.T) {
	h := newHarness(t)
	k := withFast(t, h)
	h.org.ous["ou-ab12-cd34ef56"] = []string{"444455556666"}
	h.publish(`{"payer_account_id":"123456789012","scopes":[],"ai":{"scopes":[
		{"ou_id":"ou-ab12-cd34ef56","hourly_usd":0.01,"mode":"enforce"},
		{"account_id":"444455556666","hourly_usd":100,"mode":"enforce"}]}}`)
	h.pull()
	k.call(h, "444455556666", "us.anthropic.claude-opus-5-5", 1000, 1000)
	h.trail()
	if len(k.policies.Calls) != 0 {
		t.Fatalf("the account's own higher cap must win: %v", k.policies.Calls)
	}
}

func TestUncoveredAccountTripwireFollowsDefaultMode(t *testing.T) {
	h := newHarness(t)
	k := withFast(t, h)
	h.publish(`{"payer_account_id":"123456789012","scopes":[],"ai":{"defaults":{"mode":"enforce"}}}`)
	h.pull()
	k.call(h, "777788889999", "acme.secret-model-v1:0", 1, 1)
	h.trail()
	if !k.policies.Attached["p-ai/777788889999"] {
		t.Fatalf("unknown model on an uncovered account must cut off in enforce: %v", h.events.kinds())
	}
}

func TestTeardownDetachesAICutoffs(t *testing.T) {
	h := newHarness(t)
	k := withFast(t, h)
	h.org.attached["p-ai"] = []config.Target{{Kind: "account", ID: "444455556666"}}
	k.policies.Attached["p-ai/444455556666"] = true
	if err := h.g.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k.policies.Attached["p-ai/444455556666"] {
		t.Fatal("teardown left the AI cutoff attached")
	}
}
