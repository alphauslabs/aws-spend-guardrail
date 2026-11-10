// Package config defines the guard's config document, its validation, and the
// rules for telling a tightening change from a loosening one.
//
// The CLI and the guard both validate through this package, so a document gets
// the same verdict, with the same messages, everywhere.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Field values.
const (
	PeriodMonthly   = "monthly"
	PeriodQuarterly = "quarterly"
	PeriodAnnually  = "annually"

	StyleA = "A"
	StyleB = "B"

	ModeDryRun  = "dry-run"
	ModeEnforce = "enforce"

	MonthEndAutoLift   = "auto_lift"
	MonthEndStayFrozen = "stay_frozen"
)

// Built-in defaults, used when neither the scope nor the document's defaults
// set a field.
var builtinDefaults = Settings{
	Period:       PeriodMonthly,
	ThresholdPct: 100,
	FreezeStyle:  StyleB,
	Mode:         ModeDryRun,
	MonthEnd:     MonthEndStayFrozen,
}

// Settings are the per-scope values that can also be set as defaults. A zero
// value means "not set".
type Settings struct {
	Period       string `json:"period,omitempty"`
	ThresholdPct int    `json:"threshold_pct,omitempty"`
	FreezeStyle  string `json:"freeze_style,omitempty"`
	Mode         string `json:"mode,omitempty"`
	MonthEnd     string `json:"month_end,omitempty"`
}

// Scope is one protected account or OU as written in a document.
type Scope struct {
	AccountID  string  `json:"account_id,omitempty"`
	OUID       string  `json:"ou_id,omitempty"`
	CeilingUSD float64 `json:"ceiling_usd"`
	Settings
}

// Document is one config version as written.
type Document struct {
	PayerAccountID string    `json:"payer_account_id"`
	Defaults       *Settings `json:"defaults,omitempty"`
	Scopes         []Scope   `json:"scopes"`
}

// Target is what a scope protects: one account or one OU.
type Target struct {
	Kind string `json:"kind"` // "account" or "ou"
	ID   string `json:"id"`
}

func (t Target) String() string { return t.Kind + ":" + t.ID }

// Target returns what the scope protects.
func (s Scope) Target() Target {
	if s.OUID != "" {
		return Target{Kind: "ou", ID: s.OUID}
	}
	return Target{Kind: "account", ID: s.AccountID}
}

// ResolvedScope is a scope with every field filled in.
type ResolvedScope struct {
	Target       Target  `json:"target"`
	CeilingUSD   float64 `json:"ceiling_usd"`
	Period       string  `json:"period"`
	ThresholdPct int     `json:"threshold_pct"`
	FreezeStyle  string  `json:"freeze_style"`
	Mode         string  `json:"mode"`
	MonthEnd     string  `json:"month_end"`
}

// Resolved is a document with defaults applied, ordered by target. It is what
// the guard applies and what the diff compares.
type Resolved struct {
	PayerAccountID string          `json:"payer_account_id"`
	Scopes         []ResolvedScope `json:"scopes"`
}

// Scope returns the scope for a target, if present.
func (r *Resolved) Scope(t Target) (ResolvedScope, bool) {
	for _, s := range r.Scopes {
		if s.Target == t {
			return s, true
		}
	}
	return ResolvedScope{}, false
}

// Resolve applies document defaults, then built-in defaults, to every scope.
func (d *Document) Resolve() *Resolved {
	r := &Resolved{PayerAccountID: d.PayerAccountID}
	var docDefaults Settings
	if d.Defaults != nil {
		docDefaults = *d.Defaults
	}
	for _, s := range d.Scopes {
		eff := pick(s.Settings, docDefaults, builtinDefaults)
		r.Scopes = append(r.Scopes, ResolvedScope{
			Target:       s.Target(),
			CeilingUSD:   s.CeilingUSD,
			Period:       eff.Period,
			ThresholdPct: eff.ThresholdPct,
			FreezeStyle:  eff.FreezeStyle,
			Mode:         eff.Mode,
			MonthEnd:     eff.MonthEnd,
		})
	}
	r.sort()
	return r
}

func (r *Resolved) sort() {
	sort.Slice(r.Scopes, func(i, j int) bool {
		return r.Scopes[i].Target.String() < r.Scopes[j].Target.String()
	})
}

func pick(layers ...Settings) Settings {
	var out Settings
	for i := len(layers) - 1; i >= 0; i-- {
		l := layers[i]
		if l.Period != "" {
			out.Period = l.Period
		}
		if l.ThresholdPct != 0 {
			out.ThresholdPct = l.ThresholdPct
		}
		if l.FreezeStyle != "" {
			out.FreezeStyle = l.FreezeStyle
		}
		if l.Mode != "" {
			out.Mode = l.Mode
		}
		if l.MonthEnd != "" {
			out.MonthEnd = l.MonthEnd
		}
	}
	return out
}

// Problem is one reason a document is invalid.
type Problem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	if p.Field == "" {
		return p.Message
	}
	return p.Field + ": " + p.Message
}

// InvalidError lists every problem found in a document.
type InvalidError struct {
	Problems []Problem
}

func (e *InvalidError) Error() string {
	var b strings.Builder
	b.WriteString("invalid config")
	for _, p := range e.Problems {
		b.WriteString("\n  - ")
		b.WriteString(p.String())
	}
	return b.String()
}

// Options adjust validation.
type Options struct {
	// PayerAccountID, when set, is the payer account the document must be for.
	// The guard sets it to the account it runs in.
	PayerAccountID string
}

// Parse validates raw document bytes and returns the parsed document. On any
// problem it returns an *InvalidError listing all of them.
func Parse(raw []byte, opts Options) (*Document, error) {
	if probs := checkSchema(raw); len(probs) > 0 {
		return nil, &InvalidError{Problems: probs}
	}
	var d Document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return nil, &InvalidError{Problems: []Problem{{Message: err.Error()}}}
	}
	if probs := checkSemantics(&d, opts); len(probs) > 0 {
		return nil, &InvalidError{Problems: probs}
	}
	return &d, nil
}

func checkSemantics(d *Document, opts Options) []Problem {
	var probs []Problem
	if opts.PayerAccountID != "" && d.PayerAccountID != opts.PayerAccountID {
		probs = append(probs, Problem{
			Field:   "payer_account_id",
			Message: fmt.Sprintf("document is for payer account %s, expected %s", d.PayerAccountID, opts.PayerAccountID),
		})
	}
	seen := map[Target]int{}
	for i, s := range d.Scopes {
		field := fmt.Sprintf("scopes[%d]", i)
		t := s.Target()
		if t.Kind == "account" && t.ID == d.PayerAccountID {
			probs = append(probs, Problem{
				Field:   field + ".account_id",
				Message: "the payer account cannot be a scope: a freeze policy does not apply to the management account",
			})
		}
		if t.Kind == "ou" && strings.HasPrefix(t.ID, "r-") {
			probs = append(probs, Problem{
				Field:   field + ".ou_id",
				Message: "the organization root cannot be a scope: a freeze there would cover every member account at once; name OUs or accounts instead",
			})
		}
		if prev, ok := seen[t]; ok {
			probs = append(probs, Problem{
				Field:   field,
				Message: fmt.Sprintf("%s %s is already a scope at scopes[%d]", t.Kind, t.ID, prev),
			})
			continue
		}
		seen[t] = i
	}
	return probs
}
