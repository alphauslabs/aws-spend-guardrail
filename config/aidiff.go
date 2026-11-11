package config

import (
	"fmt"
	"reflect"
	"sort"
)

// AI change fields. An AI change has an empty Target unless it is about one
// AI scope.
const (
	FieldAIScope     = "ai.scope"
	FieldAIModels    = "ai.models"
	FieldAIRegions   = "ai.regions"
	FieldAIAPIKey    = "ai.api_key_callers"
	FieldAIPrice     = "ai.prices"
	FieldAILift      = "ai.lifts"
	allowlistAnyText = "any"
)

func emptyAI(r *ResolvedAI) *ResolvedAI {
	if r == nil {
		return &ResolvedAI{}
	}
	return r
}

// aiChanges classifies the differences between two AI sections. Anything that
// lets more AI spend through, or lifts a cutoff, is a loosening.
func aiChanges(applied, desired *ResolvedAI) []Change {
	a, d := emptyAI(applied), emptyAI(desired)
	var out []Change
	add := func(t Target, field, from, to string, dir Direction) {
		out = append(out, Change{Target: t, Field: field, From: from, To: to, Direction: dir})
	}

	for _, ds := range d.Scopes {
		as, ok := a.Scope(ds.Target)
		if !ok {
			add(ds.Target, FieldAIScope, "", "present", Tightening)
			continue
		}
		for _, c := range []struct {
			name     string
			old, new float64
		}{{"ai.hourly_usd", as.HourlyUSD, ds.HourlyUSD}, {"ai.daily_usd", as.DailyUSD, ds.DailyUSD}, {"ai.monthly_usd", as.MonthlyUSD, ds.MonthlyUSD}} {
			if c.old != c.new {
				add(ds.Target, c.name, capText(c.old), capText(c.new), dirLower(c.old == 0 || (c.new != 0 && c.new < c.old)))
			}
		}
		if as.Mode != ds.Mode {
			add(ds.Target, "ai.mode", as.Mode, ds.Mode, dirLower(ds.Mode == ModeEnforce))
		}
	}
	for _, as := range a.Scopes {
		if _, ok := d.Scope(as.Target); !ok {
			add(as.Target, FieldAIScope, "present", "", Loosening)
		}
	}

	if am, dm := modeOf(a), modeOf(d); am != dm {
		add(Target{}, "ai.mode", am, dm, dirLower(dm == ModeEnforce))
	}
	out = append(out, allowlistChanges(FieldAIModels, a.Models, d.Models)...)
	out = append(out, allowlistChanges(FieldAIRegions, a.Regions, d.Regions)...)

	// API-key callers: absent means nobody, so additions loosen.
	was, now := set(a.APIKeyCallers), set(d.APIKeyCallers)
	for _, c := range d.APIKeyCallers {
		if !was[c] {
			add(Target{}, FieldAIAPIKey, "", c, Loosening)
		}
	}
	for _, c := range a.APIKeyCallers {
		if !now[c] {
			add(Target{}, FieldAIAPIKey, c, "", Tightening)
		}
	}

	// Price overrides: a higher price makes estimated spend higher, so it
	// tightens; a lower, new or removed override may let more through.
	for _, m := range unionKeys(a.Prices, d.Prices) {
		ap, inA := a.Prices[m]
		dp, inD := d.Prices[m]
		switch {
		case inA && inD && ap == dp:
		case inA && inD && dp.InputPer1K >= ap.InputPer1K && dp.OutputPer1K >= ap.OutputPer1K:
			add(Target{}, FieldAIPrice, m+" "+priceText(ap), m+" "+priceText(dp), Tightening)
		case inA && inD:
			add(Target{}, FieldAIPrice, m+" "+priceText(ap), m+" "+priceText(dp), Loosening)
		case inD:
			add(Target{}, FieldAIPrice, "", m+" "+priceText(dp), Loosening)
		default:
			add(Target{}, FieldAIPrice, m+" "+priceText(ap), "", Loosening)
		}
	}

	wasLift, nowLift := set(a.Lifts), set(d.Lifts)
	for _, l := range d.Lifts {
		if !wasLift[l] {
			add(Target{}, FieldAILift, "", l, Loosening)
		}
	}
	for _, l := range a.Lifts {
		if !nowLift[l] {
			add(Target{}, FieldAILift, l, "", Notice)
		}
	}
	return out
}

// allowlistChanges classifies an allowlist where nil means "any".
func allowlistChanges(field string, a, d []string) []Change {
	switch {
	case a == nil && d == nil:
		return nil
	case a == nil:
		return []Change{{Field: field, From: allowlistAnyText, To: fmt.Sprint(d), Direction: Tightening}}
	case d == nil:
		return []Change{{Field: field, From: fmt.Sprint(a), To: allowlistAnyText, Direction: Loosening}}
	}
	var out []Change
	was, now := set(a), set(d)
	for _, v := range d {
		if !was[v] {
			out = append(out, Change{Field: field, To: v, Direction: Loosening})
		}
	}
	for _, v := range a {
		if !now[v] {
			out = append(out, Change{Field: field, From: v, Direction: Tightening})
		}
	}
	return out
}

// stricterAI keeps, field by field, whichever of applied and desired lets less
// AI spend through. New lifts, new price overrides and wider allowlists wait for
// approval.
func stricterAI(applied, desired *ResolvedAI) *ResolvedAI {
	if applied == nil && desired == nil {
		return nil
	}
	a, d := emptyAI(applied), emptyAI(desired)
	mode := modeOf(a)
	if modeOf(d) == ModeEnforce {
		mode = ModeEnforce
	}
	out := &ResolvedAI{
		Mode:          mode,
		Models:        tighterAllowlist(a.Models, d.Models),
		Regions:       tighterAllowlist(a.Regions, d.Regions),
		APIKeyCallers: intersect(a.APIKeyCallers, d.APIKeyCallers),
		Lifts:         a.Lifts,
	}
	for _, ds := range d.Scopes {
		as, ok := a.Scope(ds.Target)
		if !ok {
			out.Scopes = append(out.Scopes, ds)
			continue
		}
		m := as
		m.HourlyUSD = tighterCap(as.HourlyUSD, ds.HourlyUSD)
		m.DailyUSD = tighterCap(as.DailyUSD, ds.DailyUSD)
		m.MonthlyUSD = tighterCap(as.MonthlyUSD, ds.MonthlyUSD)
		if ds.Mode == ModeEnforce {
			m.Mode = ModeEnforce
		}
		out.Scopes = append(out.Scopes, m)
	}
	for _, as := range a.Scopes {
		if _, ok := d.Scope(as.Target); !ok {
			out.Scopes = append(out.Scopes, as)
		}
	}
	sort.Slice(out.Scopes, func(i, j int) bool { return out.Scopes[i].Target.String() < out.Scopes[j].Target.String() })
	for _, m := range unionKeys(a.Prices, d.Prices) {
		ap, inA := a.Prices[m]
		dp, inD := d.Prices[m]
		if !inA {
			continue // a new override waits for approval
		}
		if out.Prices == nil {
			out.Prices = map[string]ModelPrice{}
		}
		if inD {
			ap = ModelPrice{InputPer1K: max(ap.InputPer1K, dp.InputPer1K), OutputPer1K: max(ap.OutputPer1K, dp.OutputPer1K)}
		}
		out.Prices[m] = ap
	}
	return out
}

// modeOf is how tripwires act on uncovered accounts; an absent AI section is
// dry-run.
func modeOf(r *ResolvedAI) string {
	if r == nil || r.Mode == "" {
		return ModeDryRun
	}
	return r.Mode
}

func tighterCap(a, d float64) float64 {
	switch {
	case a == 0:
		return d
	case d == 0:
		return a
	}
	return min(a, d)
}

func tighterAllowlist(a, d []string) []string {
	switch {
	case a == nil:
		return d
	case d == nil:
		return a
	}
	return intersect(a, d)
}

func intersect(a, d []string) []string {
	if a == nil || d == nil {
		return nil
	}
	in := set(d)
	out := []string{}
	for _, v := range a {
		if in[v] {
			out = append(out, v)
		}
	}
	return out
}

func equalAI(a, b *ResolvedAI) bool { return reflect.DeepEqual(a, b) }

func set(list []string) map[string]bool {
	m := map[string]bool{}
	for _, v := range list {
		m[v] = true
	}
	return m
}

func unionKeys(a, b map[string]ModelPrice) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]ModelPrice{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func capText(v float64) string {
	if v == 0 {
		return "none"
	}
	return money(v)
}

func priceText(p ModelPrice) string {
	return fmt.Sprintf("in %g/out %g per 1k", p.InputPer1K, p.OutputPer1K)
}
