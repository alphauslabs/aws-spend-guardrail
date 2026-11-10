package config

import "fmt"

// Direction says whether a change makes protection stricter or looser.
type Direction string

const (
	Tightening Direction = "tightening"
	Loosening  Direction = "loosening"
)

// Change is one difference between two resolved configs.
type Change struct {
	Target    Target    `json:"target"`
	Field     string    `json:"field"` // "scope" for an added or removed scope
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	Direction Direction `json:"direction"`
}

func (c Change) String() string {
	switch {
	case c.Field == "scope" && c.From == "":
		return fmt.Sprintf("%s: scope added (%s)", c.Target, c.Direction)
	case c.Field == "scope":
		return fmt.Sprintf("%s: scope removed (%s)", c.Target, c.Direction)
	default:
		return fmt.Sprintf("%s: %s %s -> %s (%s)", c.Target, c.Field, c.From, c.To, c.Direction)
	}
}

// Diff lists the differences from applied to desired, each classified. A nil
// applied config means nothing is applied yet, so every scope is an addition.
func Diff(applied, desired *Resolved) []Change {
	var out []Change
	if applied == nil {
		applied = &Resolved{}
	}
	for _, d := range desired.Scopes {
		a, ok := applied.Scope(d.Target)
		if !ok {
			out = append(out, Change{Target: d.Target, Field: "scope", To: "present", Direction: Tightening})
			continue
		}
		out = append(out, scopeChanges(a, d)...)
	}
	for _, a := range applied.Scopes {
		if _, ok := desired.Scope(a.Target); !ok {
			out = append(out, Change{Target: a.Target, Field: "scope", From: "present", Direction: Loosening})
		}
	}
	return out
}

// HasLoosening reports whether any change loosens protection.
func HasLoosening(changes []Change) bool {
	for _, c := range changes {
		if c.Direction == Loosening {
			return true
		}
	}
	return false
}

func scopeChanges(a, d ResolvedScope) []Change {
	var out []Change
	add := func(field, from, to string, dir Direction) {
		out = append(out, Change{Target: d.Target, Field: field, From: from, To: to, Direction: dir})
	}
	if a.CeilingUSD != d.CeilingUSD {
		add("ceiling_usd", money(a.CeilingUSD), money(d.CeilingUSD), dirLower(d.CeilingUSD < a.CeilingUSD))
	}
	if a.ThresholdPct != d.ThresholdPct {
		add("threshold_pct", fmt.Sprint(a.ThresholdPct), fmt.Sprint(d.ThresholdPct), dirLower(d.ThresholdPct < a.ThresholdPct))
	}
	if a.Mode != d.Mode {
		add("mode", a.Mode, d.Mode, dirLower(d.Mode == ModeEnforce))
	}
	if a.FreezeStyle != d.FreezeStyle {
		add("freeze_style", a.FreezeStyle, d.FreezeStyle, dirLower(d.FreezeStyle == StyleB))
	}
	if a.MonthEnd != d.MonthEnd {
		add("month_end", a.MonthEnd, d.MonthEnd, dirLower(d.MonthEnd == MonthEndStayFrozen))
	}
	if a.Period != d.Period {
		// Not ordered: a different period is neither clearly stricter nor
		// looser, so it is treated as a loosening and needs approval.
		add("period", a.Period, d.Period, Loosening)
	}
	return out
}

func dirLower(stricter bool) Direction {
	if stricter {
		return Tightening
	}
	return Loosening
}

func money(v float64) string { return fmt.Sprintf("%.2f", v) }

// Stricter returns, scope by scope and field by field, the stricter of the
// applied and desired configs. A scope only in desired is added; a scope only
// in applied is kept. Applying the result at once never loosens protection.
func Stricter(applied, desired *Resolved) *Resolved {
	out := &Resolved{PayerAccountID: desired.PayerAccountID}
	if applied == nil {
		applied = &Resolved{}
	}
	for _, d := range desired.Scopes {
		a, ok := applied.Scope(d.Target)
		if !ok {
			out.Scopes = append(out.Scopes, d)
			continue
		}
		m := a
		if d.CeilingUSD < m.CeilingUSD {
			m.CeilingUSD = d.CeilingUSD
		}
		if d.ThresholdPct < m.ThresholdPct {
			m.ThresholdPct = d.ThresholdPct
		}
		if d.Mode == ModeEnforce {
			m.Mode = ModeEnforce
		}
		if d.FreezeStyle == StyleB {
			m.FreezeStyle = StyleB
		}
		if d.MonthEnd == MonthEndStayFrozen {
			m.MonthEnd = MonthEndStayFrozen
		}
		// Period stays as applied until the change is approved.
		out.Scopes = append(out.Scopes, m)
	}
	for _, a := range applied.Scopes {
		if _, ok := desired.Scope(a.Target); !ok {
			out.Scopes = append(out.Scopes, a)
		}
	}
	out.sort()
	return out
}

// Equal reports whether two resolved configs are the same.
func Equal(a, b *Resolved) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.PayerAccountID != b.PayerAccountID || len(a.Scopes) != len(b.Scopes) {
		return false
	}
	for i := range a.Scopes {
		if a.Scopes[i] != b.Scopes[i] {
			return false
		}
	}
	return true
}
