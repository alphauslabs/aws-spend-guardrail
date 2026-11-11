package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// AI pack settings, as written in a document's "ai" section.

// AICaps are spend caps in USD. Zero means no cap.
type AICaps struct {
	HourlyUSD  float64 `json:"hourly_usd,omitempty"`
	DailyUSD   float64 `json:"daily_usd,omitempty"`
	MonthlyUSD float64 `json:"monthly_usd,omitempty"`
	Mode       string  `json:"mode,omitempty"`
}

// AIScope is the AI caps for one account or OU.
type AIScope struct {
	AccountID string `json:"account_id,omitempty"`
	OUID      string `json:"ou_id,omitempty"`
	AICaps
}

// Target returns what the AI scope covers.
func (s AIScope) Target() Target {
	if s.OUID != "" {
		return Target{Kind: "ou", ID: s.OUID}
	}
	return Target{Kind: "account", ID: s.AccountID}
}

// ModelPrice is a model's price in USD per 1,000 tokens.
type ModelPrice struct {
	InputPer1K  float64 `json:"input_per_1k"`
	OutputPer1K float64 `json:"output_per_1k"`
}

// AISection is the "ai" section of a document.
type AISection struct {
	Defaults *AICaps   `json:"defaults,omitempty"`
	Scopes   []AIScope `json:"scopes,omitempty"`
	// Models and Regions are allowlists; nil means any model the price table
	// knows, and any region.
	Models  []string `json:"models,omitempty"`
	Regions []string `json:"regions,omitempty"`
	// APIKeyCallers may use Bedrock API keys; nobody else may.
	APIKeyCallers []string              `json:"api_key_callers,omitempty"`
	Prices        map[string]ModelPrice `json:"prices,omitempty"`
	Lifts         []string              `json:"lifts,omitempty"`
}

// ResolvedAIScope is an AI scope with defaults applied.
type ResolvedAIScope struct {
	Target     Target  `json:"target"`
	HourlyUSD  float64 `json:"hourly_usd"`
	DailyUSD   float64 `json:"daily_usd"`
	MonthlyUSD float64 `json:"monthly_usd"`
	Mode       string  `json:"mode"`
}

// ResolvedAI is the AI section as the guard applies it: sorted, defaults
// applied, nil allowlists kept nil (any).
type ResolvedAI struct {
	// Mode is ai.defaults.mode: how tripwires act on accounts no AI scope
	// covers. Dry-run unless set.
	Mode   string            `json:"mode"`
	Scopes []ResolvedAIScope `json:"scopes,omitempty"`
	// Models and Regions are nil for "any" and empty for "none", so they keep
	// no omitempty: a saved empty list must not come back as "any".
	Models        []string              `json:"models"`
	Regions       []string              `json:"regions"`
	APIKeyCallers []string              `json:"api_key_callers,omitempty"`
	Prices        map[string]ModelPrice `json:"prices,omitempty"`
	Lifts         []string              `json:"lifts,omitempty"`
}

// Scope returns the AI scope for a target, if present.
func (r *ResolvedAI) Scope(t Target) (ResolvedAIScope, bool) {
	if r == nil {
		return ResolvedAIScope{}, false
	}
	for _, s := range r.Scopes {
		if s.Target == t {
			return s, true
		}
	}
	return ResolvedAIScope{}, false
}

func sortedCopy(in []string) []string {
	if in == nil {
		return nil
	}
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func (a *AISection) resolve() *ResolvedAI {
	if a == nil {
		return nil
	}
	r := &ResolvedAI{
		Models:        sortedCopy(a.Models),
		Regions:       sortedCopy(a.Regions),
		APIKeyCallers: sortedCopy(a.APIKeyCallers),
		Lifts:         sortedCopy(a.Lifts),
	}
	if len(a.Prices) > 0 {
		r.Prices = map[string]ModelPrice{}
		for k, v := range a.Prices {
			r.Prices[k] = v
		}
	}
	var def AICaps
	if a.Defaults != nil {
		def = *a.Defaults
	}
	r.Mode = def.Mode
	if r.Mode == "" {
		r.Mode = ModeDryRun
	}
	for _, s := range a.Scopes {
		pick := func(v, d float64) float64 {
			if v != 0 {
				return v
			}
			return d
		}
		mode := s.Mode
		if mode == "" {
			mode = def.Mode
		}
		if mode == "" {
			mode = ModeDryRun
		}
		r.Scopes = append(r.Scopes, ResolvedAIScope{
			Target:     s.Target(),
			HourlyUSD:  pick(s.HourlyUSD, def.HourlyUSD),
			DailyUSD:   pick(s.DailyUSD, def.DailyUSD),
			MonthlyUSD: pick(s.MonthlyUSD, def.MonthlyUSD),
			Mode:       mode,
		})
	}
	sort.Slice(r.Scopes, func(i, j int) bool { return r.Scopes[i].Target.String() < r.Scopes[j].Target.String() })
	return r
}

// LiftIDPattern is the shape of a cutoff id: ai:<account>:<UTC minute>.
const LiftIDPattern = `^ai:[0-9]{12}:[0-9]{8}T[0-9]{4}$`

var (
	liftIDRe   = regexp.MustCompile(LiftIDPattern)
	regionRe   = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]$`)
	iamCallRe  = regexp.MustCompile(`^arn:aws[a-z-]*:(iam|sts)::[0-9]{12}:[A-Za-z0-9+=,.@_/-]+$`)
	aiFields   = []string{"defaults", "scopes", "models", "regions", "api_key_callers", "prices", "lifts"}
	capFields  = []string{"hourly_usd", "daily_usd", "monthly_usd", "mode"}
	aiScopeFld = append([]string{"account_id", "ou_id"}, capFields...)
)

// ai checks the "ai" section's shape.
func (c *checker) ai(v json.RawMessage) {
	obj := c.object("ai", v)
	if obj == nil {
		return
	}
	c.unknown("ai", obj, aiFields)
	if d, ok := obj["defaults"]; ok {
		if o := c.object("ai.defaults", d); o != nil {
			c.unknown("ai.defaults", o, capFields)
			c.caps("ai.defaults", o)
		}
	}
	if sv, ok := obj["scopes"]; ok {
		var list []json.RawMessage
		if err := json.Unmarshal(sv, &list); err != nil || isNull(sv) {
			c.add("ai.scopes", "must be an array")
		} else {
			for i, s := range list {
				f := fmt.Sprintf("ai.scopes[%d]", i)
				o := c.object(f, s)
				if o == nil {
					continue
				}
				c.unknown(f, o, aiScopeFld)
				acct, hasAcct := o["account_id"]
				ou, hasOU := o["ou_id"]
				switch {
				case hasAcct && hasOU:
					c.add(f, "set account_id or ou_id, not both")
				case !hasAcct && !hasOU:
					c.add(f, "account_id or ou_id is required")
				case hasAcct:
					c.pattern(join(f, "account_id"), acct, accountIDRe, "a 12-digit account id")
				default:
					c.pattern(join(f, "ou_id"), ou, ouIDRe, "an OU id like ou-ab12-cd34ef56")
				}
				c.caps(f, o)
			}
		}
	}
	c.strings("ai.models", obj, nil, "")
	c.strings("ai.regions", obj, regionRe, "an AWS region like us-east-1")
	c.strings("ai.api_key_callers", obj, iamCallRe, "an IAM user or role ARN")
	c.strings("ai.lifts", obj, liftIDRe, "a cutoff id like ai:111122223333:20261111T0930")
	if pv, ok := obj["prices"]; ok {
		if o := c.object("ai.prices", pv); o != nil {
			names := make([]string, 0, len(o))
			for k := range o {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				f := "ai.prices." + k
				po := c.object(f, o[k])
				if po == nil {
					continue
				}
				c.unknown(f, po, []string{"input_per_1k", "output_per_1k"})
				for _, n := range []string{"input_per_1k", "output_per_1k"} {
					raw, ok := po[n]
					if !ok {
						c.add(join(f, n), "is required")
						continue
					}
					c.nonNegative(join(f, n), raw)
				}
			}
		}
	}
}

func (c *checker) caps(parent string, o map[string]json.RawMessage) {
	for _, n := range []string{"hourly_usd", "daily_usd", "monthly_usd"} {
		if raw, ok := o[n]; ok {
			c.nonNegative(join(parent, n), raw)
		}
	}
	if raw, ok := o["mode"]; ok {
		if s, ok := c.str(join(parent, "mode"), raw); ok && !contains(enums["mode"], s) {
			c.add(join(parent, "mode"), fmt.Sprintf("must be one of %s, got %q", strings.Join(enums["mode"], ", "), s))
		}
	}
}

func (c *checker) nonNegative(field string, raw json.RawMessage) {
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil || isNull(raw) {
		c.add(field, "must be a number")
	} else if n < 0 {
		c.add(field, fmt.Sprintf("must not be negative, got %v", n))
	}
}

// strings checks an optional array of strings, each matching re when given.
func (c *checker) strings(field string, obj map[string]json.RawMessage, re *regexp.Regexp, what string) {
	raw, ok := obj[strings.TrimPrefix(field, "ai.")]
	if !ok {
		return
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil || isNull(raw) {
		c.add(field, "must be an array of strings")
		return
	}
	seen := map[string]bool{}
	for i, v := range list {
		f := fmt.Sprintf("%s[%d]", field, i)
		s, ok := c.str(f, v)
		if !ok {
			continue
		}
		if s == "" {
			c.add(f, "must not be empty")
		} else if re != nil && !re.MatchString(s) {
			c.add(f, fmt.Sprintf("must be %s, got %q", what, s))
		}
		if seen[s] {
			c.add(f, "is listed twice")
		}
		seen[s] = true
	}
}

// aiSemantics checks rules that need the parsed section.
func aiSemantics(a *AISection) []Problem {
	if a == nil {
		return nil
	}
	var probs []Problem
	seen := map[Target]int{}
	for i, s := range a.Scopes {
		t := s.Target()
		if prev, ok := seen[t]; ok {
			probs = append(probs, Problem{Field: fmt.Sprintf("ai.scopes[%d]", i), Message: fmt.Sprintf("%s %s is already an AI scope at ai.scopes[%d]", t.Kind, t.ID, prev)})
			continue
		}
		seen[t] = i
		if t.Kind == "ou" && strings.HasPrefix(t.ID, "r-") {
			probs = append(probs, Problem{Field: fmt.Sprintf("ai.scopes[%d].ou_id", i), Message: "the organization root cannot be an AI scope; name OUs or accounts instead"})
		}
	}
	return probs
}
