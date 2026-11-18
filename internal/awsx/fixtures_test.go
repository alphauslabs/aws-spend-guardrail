package awsx

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// State fixtures (add-guard-upgrades D3): each release commits the items its
// guard stores in DynamoDB, written through the real store code, as
// testdata/state/<version>.json. TestFixtures loads every one into the current
// code, so a release that can't read an earlier release's state fails here.
// The release workflow also runs the previous tag's TestFixtures against the
// new release's fixture, which checks a rollback.
//
// To write the fixture for a release:
//
//	GUARDRAIL_WRITE_STATE_FIXTURE=v0.1.0 go test ./internal/awsx -run TestWriteStateFixture

// The values every fixture holds and TestFixtures checks.
const (
	fixtureApproval = "apr_fixture"
	fixtureFrozen   = "account:111122223333"
	fixtureCutoff   = "ai:444455556666:20261116T0000"
	fixtureVersion  = 7
)

// fixtureItem is one stored item: its string and number attributes.
type fixtureItem struct {
	S map[string]string `json:"S"`
	N map[string]string `json:"N,omitempty"`
}

func writeFixtureState(t *testing.T, db *fakeDDB) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	doc, err := config.Parse([]byte(`{"payer_account_id": "123456789012",
		"scopes": [{"account_id": "111122223333", "ceiling_usd": 100, "mode": "enforce"}],
		"ai": {"unknown_models": "count", "scopes": [{"account_id": "444455556666", "hourly_usd": 1, "mode": "enforce", "blocked": ["agents"]}]}}`), config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	eff := doc.Resolve()
	desired := doc.Resolve()
	desired.Scopes[0].CeilingUSD = 200
	st := &State{API: db, Table: "t"}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.Save(ctx, &guard.State{
		AppliedVersion: fixtureVersion - 1, EffectiveVersion: fixtureVersion, Effective: eff, InstallID: at.Unix(), InstallChecked: true,
		Pending: &guard.Pending{Version: fixtureVersion, ApprovalID: fixtureApproval, FirstSeen: at, Desired: desired, Note: "raise the ceiling"},
	}))
	must(st.MarkFrozen(ctx, guard.Frozen{Target: config.Target{Kind: "account", ID: "111122223333"}, Since: at, PeriodStart: at, PolicyID: "p-b", ThresholdUSD: 90, How: "budget action"}))
	must(st.MarkReported(ctx, schema.KindUnprotected, "777788889999", at))
	must(st.SetHeartbeatDigest(ctx, "digest"))
	// Since v0.2.0: the scheduled price publish's alert state.
	must(st.SetMeta(ctx, "prices.publish", map[string]any{"unpriced": []string{"xai.grok-5"}}))
	act := &ActivityStore{API: db, Table: "t"}
	must(act.PutCutoff(ctx, activity.Cutoff{ID: fixtureCutoff, Pack: "ai", Account: "444455556666", At: at, Rule: "hourly_cap", CapUSD: 1, Detail: "estimated hour 1.20 USD, cap 1.00 USD", Attached: true}))
	must(act.AddUsage(ctx, "ai", activity.Usage{Account: "444455556666", Time: at, Model: "amazon.nova-micro-v1:0", Calls: 1, In: 10, Out: 20, USD: 1.2}))
	must(act.PutMeta(ctx, "prices.status", map[string]string{}))
	// Since v0.2.2: unpriced models already reported.
	must(act.PutMeta(ctx, "unpriced.ai", map[string]any{"moonshotai.kimi-k3.1": map[string]string{"account": "444455556666", "at": "2026-11-16T00:00:00Z"}}))
	// Since add-regional-prices: shared prices held per region, and the
	// index of the regions held.
	for region, in := range map[string]float64{"us-east-1": 0.0008, "eu-west-2": 0.00124} {
		must(act.PutMeta(ctx, "prices.ai."+region, map[string]any{"version": 20, "sha256": "fixture-" + region,
			"doc": map[string]any{"source": "fixture", "models": map[string]any{"amazon.nova-pro-v1:0": map[string]float64{"input_per_1k": in, "output_per_1k": in * 4}}}}))
	}
	must(act.PutMeta(ctx, "prices.ai.regions", map[string]any{"regions": []string{"eu-west-2", "us-east-1"}}))
	// Since add-mantle-usage: what the mantle reader counted, its read status,
	// and (from v0.2.7, once the previous release's fake applies begins_with)
	// an account reported as not sharing mantle usage.
	must(act.PutMeta(ctx, "mantle.us-east-1", map[string]any{"counted": map[string]any{
		"2026-11-16T00:29:00Z|444455556666|openai.gpt-oss-20b": map[string]int64{"calls": 2, "in": 140, "out": 117}}}))
	must(act.PutMeta(ctx, "mantle.status", map[string]any{"failing": map[string]string{"eu-west-2": "no CloudWatch observability sink"}}))
	must(st.MarkReported(ctx, schema.KindMantleUnlinked, "444455556666@us-east-1", at))
	// Since add-ai-blocked: what the guard last wrote to the block policy.
	must(st.SetMeta(ctx, "ai.blocked", map[string]any{"hash": "fixture", "attached": true, "sets": map[string][]string{"444455556666": {"agents"}}}))
}

func dumpFixture(db *fakeDDB) []fixtureItem {
	var out []fixtureItem
	for _, it := range db.items {
		fi := fixtureItem{S: map[string]string{}}
		for name, v := range it {
			switch x := v.(type) {
			case *ddbtypes.AttributeValueMemberS:
				fi.S[name] = x.Value
			case *ddbtypes.AttributeValueMemberN:
				if fi.N == nil {
					fi.N = map[string]string{}
				}
				fi.N[name] = x.Value
			}
		}
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].S["pk"]+"|"+out[i].S["sk"] < out[j].S["pk"]+"|"+out[j].S["sk"] })
	return out
}

func loadFixture(t *testing.T, path string) *fakeDDB {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var items []fixtureItem
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	db := newFakeDDB()
	for _, fi := range items {
		it := map[string]ddbtypes.AttributeValue{}
		for k, v := range fi.S {
			it[k] = &ddbtypes.AttributeValueMemberS{Value: v}
		}
		for k, v := range fi.N {
			it[k] = &ddbtypes.AttributeValueMemberN{Value: v}
		}
		db.items[idOf(it)] = it
	}
	return db
}

// TestWriteStateFixture writes testdata/state/<version>.json when
// GUARDRAIL_WRITE_STATE_FIXTURE names a version; otherwise it is skipped.
func TestWriteStateFixture(t *testing.T) {
	v := os.Getenv("GUARDRAIL_WRITE_STATE_FIXTURE")
	if v == "" {
		t.Skip("set GUARDRAIL_WRITE_STATE_FIXTURE=vX.Y.Z to write a fixture")
	}
	db := newFakeDDB()
	writeFixtureState(t, db)
	raw, err := json.MarshalIndent(dumpFixture(db), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("testdata/state", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("testdata/state", v+".json"), append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFixtures loads every release's stored state into this code and checks
// that the pending approval, the freeze, the AI cutoff, and the usage totals
// survive.
func TestFixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/state/*.json")
	if len(files) == 0 {
		t.Fatal("no state fixtures in testdata/state")
	}
	ctx := context.Background()
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			db := loadFixture(t, f)
			st := &State{API: db, Table: "t"}
			s, err := st.Load(ctx)
			if err != nil || s.Effective == nil || len(s.Effective.Scopes) != 1 || s.Effective.AI == nil {
				t.Fatalf("state: %+v %v", s, err)
			}
			if s.Pending == nil || s.Pending.ApprovalID != fixtureApproval || s.Pending.Version != fixtureVersion || s.Pending.Desired == nil {
				t.Fatalf("pending approval lost: %+v", s.Pending)
			}
			frozen, err := st.ListFrozen(ctx)
			if err != nil || len(frozen) != 1 || frozen[0].Target.String() != fixtureFrozen {
				t.Fatalf("freeze lost: %+v %v", frozen, err)
			}
			if r, err := st.Reported(ctx, schema.KindUnprotected); err != nil || len(r) != 1 {
				t.Fatalf("reported: %v %v", r, err)
			}
			act := &ActivityStore{API: db, Table: "t"}
			cuts, err := act.Cutoffs(ctx, "ai")
			if err != nil || len(cuts) != 1 || cuts[0].ID != fixtureCutoff || !cuts[0].Attached {
				t.Fatalf("cutoff lost: %+v %v", cuts, err)
			}
			if _, err := act.Totals(ctx, "ai", "444455556666", time.Date(2026, 11, 16, 0, 30, 0, 0, time.UTC)); err != nil {
				t.Fatalf("totals: %v", err)
			}
			// Shared prices held per region, in fixtures that have them, parse as
			// chunks (add-regional-prices D3).
			for _, region := range []string{"us-east-1", "eu-west-2"} {
				var held struct {
					Doc struct {
						Models map[string]struct {
							In float64 `json:"input_per_1k"`
						} `json:"models"`
					} `json:"doc"`
				}
				ok, err := act.GetMeta(ctx, "prices.ai."+region, &held)
				if err != nil || (ok && held.Doc.Models["amazon.nova-pro-v1:0"].In == 0) {
					t.Fatalf("held prices for %s: %+v %v", region, held, err)
				}
			}
		})
	}
}
