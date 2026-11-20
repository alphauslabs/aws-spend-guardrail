package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	// What runs, so a new code hash or template changes the digest and the
	// first run after an upgrade writes a heartbeat (add-guard-upgrades).
	snap["deployment"] = map[string]any{"guard_version": g.S.GuardVersion, "code_sha256": g.S.CodeSHA256, "template_revision": g.S.TemplateRevision}

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
	ai := map[string]any{"mode": cfg.Mode, "cutoffs": cutoffs, "trail": trail, "unknown_models": unknownModels(cfg)}
	// What the block policy denies, as the guard last wrote it
	// (add-heartbeat-ai-status D1).
	if g.Blocks != nil {
		var bs blockedState
		if _, err := g.State.Meta(ctx, metaBlocked, &bs); err != nil {
			return nil, fmt.Errorf("read the block policy's state: %w", err)
		}
		accounts := map[string][]string{}
		for a, kinds := range bs.Sets {
			accounts[a] = kinds
		}
		ai["blocked"] = map[string]any{"accounts": accounts, "attached": bs.Attached, "failing": bs.Failing}
	}
	// Mantle regions and whether their usage can be read, and the accounts
	// reported as not linked.
	if len(g.Fast.MantleRegions) > 0 {
		var ms struct {
			Failing map[string]string `json:"failing"`
		}
		if _, err := store.GetMeta(ctx, "mantle.status", &ms); err != nil {
			return nil, fmt.Errorf("read mantle status: %w", err)
		}
		regions := []map[string]any{}
		for _, r := range g.Fast.MantleRegions {
			entry := map[string]any{"region": r, "read": true}
			if e, failing := ms.Failing[r]; failing {
				entry["read"], entry["error"] = false, e
			}
			regions = append(regions, entry)
		}
		reported, err := g.State.Reported(ctx, schema.KindMantleUnlinked)
		if err != nil {
			return nil, fmt.Errorf("read unlinked accounts: %w", err)
		}
		unlinked := map[string][]string{}
		for id := range reported {
			if acct, region, ok := strings.Cut(id, "@"); ok {
				unlinked[region] = append(unlinked[region], acct)
			}
		}
		for r := range unlinked {
			sort.Strings(unlinked[r])
		}
		ai["mantle"] = map[string]any{"regions": regions, "unlinked": unlinked}
	}
	return ai, nil
}

// unknownModels is how the config treats unpriced models; state saved before
// add-unknown-model-policy has none, which is cutoff.
func unknownModels(cfg *config.ResolvedAI) string {
	if cfg.CountsUnknown() {
		return config.UnknownCount
	}
	return config.UnknownCutoff
}

// digest hashes a snapshot without its volatile parts: spend (added later) and
// the trail's last-file time.
// dailyHeartbeatID is the daily heartbeat's id: its date and a digest of its
// detail, so a second daily run on a day writes its own event instead of one
// Jennah refuses as a reused id (fix-outbox-delivery D3).
func dailyHeartbeatID(day string, detail map[string]any) string {
	raw, _ := json.Marshal(detail)
	sum := sha256.Sum256(raw)
	return schema.KindHeartbeat + ":" + day + ":" + hex.EncodeToString(sum[:])[:12]
}

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

// addAccountNames adds `account_names`, id to name, for the accounts the
// snapshot mentions (add-heartbeat-account-names D1). Like spend it comes after
// the digest, so a rename or a failed listing is never a change (D2). With no
// names known the field is left out, so "couldn't list" isn't "no names".
func (g *Guard) addAccountNames(ctx context.Context, snap map[string]any) {
	if g.Names == nil {
		return
	}
	all := g.Names(ctx)
	if len(all) == 0 {
		return
	}
	names := map[string]string{}
	for _, id := range mentionedAccounts(snap) {
		if n := all[id]; n != "" {
			names[id] = n
		}
	}
	if len(names) > 0 {
		snap["account_names"] = names
	}
}

// mentionedAccounts lists the account ids a snapshot carries: account scope
// targets, excluded and unprotected accounts, and the AI section's cutoffs,
// blocks, and accounts not linked for mantle.
func mentionedAccounts(snap map[string]any) []string {
	var ids []string
	scopes, _ := snap["scopes"].([]map[string]any)
	for _, s := range scopes {
		if t, _ := s["target"].(string); strings.HasPrefix(t, "account:") {
			ids = append(ids, strings.TrimPrefix(t, "account:"))
		}
	}
	excluded, _ := snap["excluded"].([]string)
	ids = append(ids, excluded...)
	unprotected, _ := snap["unprotected"].([]map[string]any)
	for _, u := range unprotected {
		if a, _ := u["account"].(string); a != "" {
			ids = append(ids, a)
		}
	}
	if ai, ok := snap["ai"].(map[string]any); ok {
		cutoffs, _ := ai["cutoffs"].([]map[string]any)
		for _, c := range cutoffs {
			if a, _ := c["account"].(string); a != "" {
				ids = append(ids, a)
			}
		}
		if b, ok := ai["blocked"].(map[string]any); ok {
			accounts, _ := b["accounts"].(map[string][]string)
			for a := range accounts {
				ids = append(ids, a)
			}
		}
		if m, ok := ai["mantle"].(map[string]any); ok {
			unlinked, _ := m["unlinked"].(map[string][]string)
			for _, accts := range unlinked {
				ids = append(ids, accts...)
			}
		}
	}
	return ids
}

// heartbeatDetail is a heartbeat's full detail: the fields every heartbeat
// carried before add-heartbeat-state, the snapshot, and why it was written.
func (g *Guard) heartbeatDetail(ctx context.Context, st *State, snap map[string]any, reason string, keys any) map[string]any {
	g.addSpend(ctx, snap)
	g.addAccountNames(ctx, snap)
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
