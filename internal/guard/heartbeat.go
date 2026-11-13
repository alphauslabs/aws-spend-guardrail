package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// StateLayout is the shape of the state a heartbeat carries
// (add-heartbeat-state). Change it when a field changes meaning or goes away.
const StateLayout = 1

// aiPack is the AI guard's pack name in the activity store.
const aiPack = "ai"

// Why a heartbeat was written.
const (
	HeartbeatDaily  = "daily"
	HeartbeatChange = "change"
)

// snapshot is the guard's state as a heartbeat carries it (design D1 of
// add-heartbeat-state), without spend figures, which addSpend adds once the
// digest has been taken so that spend moving is never a change.
func (g *Guard) snapshot(ctx context.Context, st *State) (map[string]any, error) {
	snap := map[string]any{"state_layout": StateLayout}

	frozen, err := g.State.ListFrozen(ctx)
	if err != nil {
		return nil, fmt.Errorf("list frozen scopes: %w", err)
	}
	frozenAt := map[string]Frozen{}
	for _, f := range frozen {
		frozenAt[f.Target.String()] = f
	}
	frozenDetail := func(f Frozen) map[string]any {
		d := map[string]any{"since": f.Since.UTC().Format(time.RFC3339), "policy_id": f.PolicyID}
		if f.How != "" {
			d["how"] = f.How
		}
		return d
	}

	scopes := []map[string]any{}
	if st.Effective != nil {
		for _, s := range st.Effective.Scopes {
			e := map[string]any{
				"target":        s.Target.String(),
				"mode":          s.Mode,
				"ceiling_usd":   s.CeilingUSD,
				"threshold_pct": s.ThresholdPct,
				"period":        s.Period,
				"freeze_style":  s.FreezeStyle,
			}
			if f, ok := frozenAt[s.Target.String()]; ok {
				e["frozen"] = frozenDetail(f)
				delete(frozenAt, s.Target.String())
			}
			scopes = append(scopes, e)
		}
		snap["excluded"] = nonNil(st.Effective.ExcludedAccounts)
	}
	// A freeze whose scope is no longer in the config is still a freeze in AWS.
	var orphans []string
	for t := range frozenAt {
		orphans = append(orphans, t)
	}
	sort.Strings(orphans)
	for _, t := range orphans {
		scopes = append(scopes, map[string]any{"target": t, "in_config": false, "frozen": frozenDetail(frozenAt[t])})
	}
	snap["scopes"] = scopes

	if p := st.Pending; p != nil {
		pend := map[string]any{
			"version":           p.Version,
			"approval_id":       p.ApprovalID,
			"first_seen":        p.FirstSeen.UTC().Format(time.RFC3339),
			"earliest_apply_at": p.FirstSeen.Add(g.S.LooseningDelay).UTC().Format(time.RFC3339),
		}
		if p.Closed != "" {
			pend["closed"] = string(p.Closed)
		}
		snap["pending"] = pend
	}

	reported, err := g.State.Reported(ctx, schema.KindUnprotected)
	if err != nil {
		return nil, fmt.Errorf("read unprotected accounts: %w", err)
	}
	var accounts []string
	for a := range reported {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)
	unprotected := []map[string]any{}
	for _, a := range accounts {
		unprotected = append(unprotected, map[string]any{"account": a, "first_reported": reported[a].UTC().Format(time.RFC3339)})
	}
	snap["unprotected"] = unprotected

	if st.Effective != nil && st.Effective.AI != nil && g.Fast != nil && g.Fast.Engine != nil {
		ai, err := g.aiSnapshot(ctx, st.Effective.AI)
		if err != nil {
			return nil, err
		}
		snap["ai"] = ai
	}
	return snap, nil
}

func (g *Guard) aiSnapshot(ctx context.Context, cfg *config.ResolvedAI) (map[string]any, error) {
	store := g.Fast.Engine.Store
	cuts, err := store.Cutoffs(ctx, aiPack)
	if err != nil {
		return nil, fmt.Errorf("read AI cutoffs: %w", err)
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Account < cuts[j].Account })
	cutoffs := []map[string]any{}
	for _, c := range cuts {
		cutoffs = append(cutoffs, map[string]any{
			"account":  c.Account,
			"rule":     c.Rule,
			"since":    c.At.UTC().Format(time.RFC3339),
			"attached": c.Attached,
		})
	}
	var stall struct {
		LastFile time.Time `json:"last_file"`
		Stalled  bool      `json:"stalled"`
	}
	trail := map[string]any{"stalled": false}
	if ok, err := store.GetMeta(ctx, "stall", &stall); err == nil && ok {
		trail["stalled"] = stall.Stalled
		if !stall.LastFile.IsZero() {
			// The last file's time moves every minute; it is volatile like spend.
			trail["last_file"] = stall.LastFile.UTC().Format(time.RFC3339)
		}
	}
	return map[string]any{"mode": cfg.Mode, "cutoffs": cutoffs, "trail": trail}, nil
}

// digest hashes a snapshot without its volatile parts: spend (added later) and
// the trail's last-file time.
func digest(snap map[string]any) string {
	cp := map[string]any{}
	for k, v := range snap {
		cp[k] = v
	}
	if ai, ok := snap["ai"].(map[string]any); ok {
		aiCopy := map[string]any{}
		for k, v := range ai {
			aiCopy[k] = v
		}
		if trail, ok := ai["trail"].(map[string]any); ok {
			aiCopy["trail"] = map[string]any{"stalled": trail["stalled"]}
		}
		cp["ai"] = aiCopy
	}
	b, _ := json.Marshal(cp) // maps marshal with sorted keys
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// addSpend adds each scope's actual spend as Budgets last reported it. A failed
// read leaves the figures out: the snapshot is still worth writing.
func (g *Guard) addSpend(ctx context.Context, snap map[string]any) {
	owned, err := g.Budgets.ListOwned(ctx)
	if err != nil {
		return
	}
	actual := map[string]float64{}
	for _, b := range owned {
		actual[b.Spec.Target.String()] = b.ActualUSD
	}
	scopes, _ := snap["scopes"].([]map[string]any)
	for _, s := range scopes {
		if v, ok := actual[s["target"].(string)]; ok {
			s["actual_usd"] = v
		}
	}
}

// heartbeatDetail is a heartbeat's full detail: the fields every heartbeat
// carried before add-heartbeat-state, the snapshot, and why it was written.
func (g *Guard) heartbeatDetail(ctx context.Context, st *State, snap map[string]any, reason string, keys any) map[string]any {
	g.addSpend(ctx, snap)
	hb := map[string]any{
		"applied_version":   st.AppliedVersion,
		"effective_version": st.EffectiveVersion,
		"pending":           st.Pending != nil,
		"reason":            reason,
	}
	if keys != nil {
		hb["keys"] = keys
	}
	for k, v := range snap {
		hb[k] = v
	}
	return g.selfReport(hb)
}

// keysAsChecked is what a change heartbeat says about the keys: the expiry the
// last daily check recorded (design D4).
func keysAsChecked(st *State) any {
	if len(st.KeyWarnings) == 0 {
		return nil
	}
	var names []string
	for n := range st.KeyWarnings {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []map[string]any
	for _, n := range names {
		w := st.KeyWarnings[n]
		e := map[string]any{"name": n, "key_id": w.KeyID}
		if !w.ExpiresAt.IsZero() {
			e["expires_at"] = w.ExpiresAt.UTC().Format(time.RFC3339)
		}
		out = append(out, e)
	}
	return out
}

// heartbeatIfChanged writes a change heartbeat when the guard's state differs
// from what the last heartbeat carried (design D2). It never fails the run that
// called it: a missed heartbeat is written by the next run that sees the change.
func (g *Guard) heartbeatIfChanged(ctx context.Context) {
	st, err := g.State.Load(ctx)
	if err != nil || st.Effective == nil {
		return // nothing configured yet: nothing to report
	}
	snap, err := g.snapshot(ctx, st)
	if err != nil {
		return
	}
	d := digest(snap)
	if last, err := g.State.HeartbeatDigest(ctx); err != nil || last == d {
		return
	}
	now := g.now()
	// The digest in the id keeps two different states written in the same
	// instant apart, and a retried write of the same state idempotent.
	id := fmt.Sprintf("%s:%d:%s", schema.KindHeartbeat, now.Unix(), d[:12])
	g.emit(ctx, schema.KindHeartbeat, id, "", st.AppliedVersion, g.heartbeatDetail(ctx, st, snap, HeartbeatChange, keysAsChecked(st)))
	_ = g.State.SetHeartbeatDigest(ctx, d)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
