package guard

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// CheckCoverage runs the coverage check on its own, as on an account event.
func (g *Guard) CheckCoverage(ctx context.Context) error {
	// Write a heartbeat if this run changed the guard's state (add-heartbeat-state).
	defer g.heartbeatIfChanged(ctx)
	st, err := g.State.Load(ctx)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if st.Effective == nil {
		return nil // nothing configured yet, so nothing to compare with
	}
	return g.coverage(ctx, st.Effective, st.AppliedVersion)
}

// coverage reports active accounts that no scope covers and no exclusion
// names, and account scopes whose account isn't in the organization. Each
// account is reported once until it is covered or excluded again.
func (g *Guard) coverage(ctx context.Context, eff *config.Resolved, version int64) error {
	accounts, err := g.Org.ListAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	active := map[string]Account{}
	for _, a := range accounts {
		if a.State == "ACTIVE" {
			active[a.ID] = a
		}
	}
	covered := map[string]bool{}
	var stale []string
	for _, s := range eff.Scopes {
		switch s.Target.Kind {
		case "account":
			covered[s.Target.ID] = true
			if _, ok := active[s.Target.ID]; !ok {
				stale = append(stale, s.Target.ID)
			}
		case "ou":
			members, err := g.Org.AccountsInOU(ctx, s.Target.ID)
			if err != nil {
				return fmt.Errorf("list accounts in %s: %w", s.Target.ID, err)
			}
			for _, m := range members {
				covered[m] = true
			}
		}
	}
	var unprotected []string
	for id := range active {
		if !covered[id] && !eff.Excluded(id) {
			unprotected = append(unprotected, id)
		}
	}
	sort.Strings(unprotected)
	sort.Strings(stale)

	var errs []error
	newUnprotected, err := g.reportOnce(ctx, schema.KindUnprotected, unprotected, version, func(id string) map[string]any {
		parent, perr := g.Org.ParentOf(ctx, id)
		if perr != nil {
			parent = "unknown"
		}
		return map[string]any{"account_id": id, "name": active[id].Name, "parent": parent}
	})
	if err != nil {
		errs = append(errs, err)
	}
	newStale, err := g.reportOnce(ctx, schema.KindStaleScope, stale, version, func(id string) map[string]any {
		return map[string]any{"account_id": id, "what": "an account scope names an account that is not an active member of the organization"}
	})
	if err != nil {
		errs = append(errs, err)
	}

	if len(newUnprotected) > 0 {
		var lines []string
		for _, d := range newUnprotected {
			lines = append(lines, fmt.Sprintf("- %s (%s, under %s)", d["account_id"], d["name"], d["parent"]))
		}
		g.alert(ctx, fmt.Sprintf("guardrail: %d unprotected account(s)", len(newUnprotected)),
			"These active accounts have no budget: no scope covers them and they aren't excluded. Add each to the config as a scope, or to excluded_accounts if leaving it out is intended.\n"+strings.Join(lines, "\n"))
	}
	if len(newStale) > 0 {
		var ids []string
		for _, d := range newStale {
			ids = append(ids, fmt.Sprint(d["account_id"]))
		}
		g.alert(ctx, fmt.Sprintf("guardrail: %d stale scope(s)", len(newStale)),
			"These account scopes name accounts that are not active members of the organization: "+strings.Join(ids, ", ")+". Remove them from the config.")
	}
	return errors.Join(errs...)
}

// reportOnce records an event for each account in current that wasn't
// already reported for this kind, and forgets accounts that no longer are,
// so they're reported again if they lapse later. It returns the details of
// the accounts reported now.
func (g *Guard) reportOnce(ctx context.Context, kind string, current []string, version int64, detail func(string) map[string]any) ([]map[string]any, error) {
	reported, err := g.State.Reported(ctx, kind)
	if err != nil {
		return nil, fmt.Errorf("read reported %s: %w", kind, err)
	}
	now := g.now()
	day := now.Format("2006-01-02")
	in := map[string]bool{}
	var out []map[string]any
	var errs []error
	for _, id := range current {
		in[id] = true
		if _, ok := reported[id]; ok {
			continue
		}
		d := detail(id)
		g.emit(ctx, kind, kind+":"+id+":"+day, "account:"+id, version, d)
		if err := g.State.MarkReported(ctx, kind, id, now); err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, d)
	}
	for id := range reported {
		if !in[id] {
			if err := g.State.ClearReported(ctx, kind, id); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return out, errors.Join(errs...)
}
