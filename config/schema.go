package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"regexp"
	"sort"
	"strings"
)

// SchemaJSON is the config JSON schema, the published contract for editors and
// other tools. The guard and the CLI validate with the plain Go checks below;
// schema_test.go keeps the two in agreement.
//
//go:embed schema.json
var SchemaJSON []byte

// The shapes and values the schema allows, shared with schema_test.go.
const (
	accountIDPattern = `^[0-9]{12}$`
	ouIDPattern      = `^(ou-[0-9a-z]{4,32}-[0-9a-z]{8,32}|r-[0-9a-z]{4,32})$`
	minThreshold     = 1
	maxThreshold     = 100
	maxAlertEmails   = 20
)

var (
	accountIDRe = regexp.MustCompile(accountIDPattern)
	ouIDRe      = regexp.MustCompile(ouIDPattern)

	enums = map[string][]string{
		"period":       {PeriodMonthly, PeriodQuarterly, PeriodAnnually},
		"freeze_style": {StyleA, StyleB},
		"mode":         {ModeDryRun, ModeEnforce},
		"month_end":    {MonthEndAutoLift, MonthEndStayFrozen},
	}

	documentFields = []string{"payer_account_id", "defaults", "scopes", "excluded_accounts", "alert_emails"}
	settingsFields = []string{"period", "threshold_pct", "freeze_style", "mode", "month_end"}
	scopeFields    = append([]string{"account_id", "ou_id", "ceiling_usd"}, settingsFields...)
)

// checkSchema checks a raw document's shape: JSON object, known fields only,
// required fields present, values of the right type and range. Semantic rules
// (payer account, duplicates, payer or root as a scope) are in config.go.
func checkSchema(raw []byte) []Problem {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		if json.Valid(raw) {
			return []Problem{{Message: "the document must be a JSON object"}}
		}
		return []Problem{{Message: "not valid JSON: " + err.Error()}}
	}
	c := &checker{}
	c.unknown("", top, documentFields)
	if v, ok := top["payer_account_id"]; ok {
		c.pattern("payer_account_id", v, accountIDRe, "a 12-digit account id")
	} else {
		c.add("payer_account_id", "is required")
	}
	if v, ok := top["defaults"]; ok {
		if obj := c.object("defaults", v); obj != nil {
			c.unknown("defaults", obj, settingsFields)
			c.settings("defaults", obj)
		}
	}
	if v, ok := top["excluded_accounts"]; ok {
		var ids []json.RawMessage
		if err := json.Unmarshal(v, &ids); err != nil || isNull(v) {
			c.add("excluded_accounts", "must be an array of account ids")
		} else {
			seen := map[string]bool{}
			for i, id := range ids {
				field := fmt.Sprintf("excluded_accounts[%d]", i)
				c.pattern(field, id, accountIDRe, "a 12-digit account id")
				if s := string(id); seen[s] {
					c.add(field, "is listed twice")
				} else {
					seen[s] = true
				}
			}
		}
	}
	if v, ok := top["alert_emails"]; ok {
		c.alertEmails(v)
	}
	v, ok := top["scopes"]
	if !ok {
		c.add("scopes", "is required")
		return c.probs
	}
	var scopes []json.RawMessage
	if err := json.Unmarshal(v, &scopes); err != nil || isNull(v) {
		c.add("scopes", "must be an array")
		return c.probs
	}
	for i, s := range scopes {
		c.scope(fmt.Sprintf("scopes[%d]", i), s)
	}
	return c.probs
}

type checker struct{ probs []Problem }

func (c *checker) add(field, msg string) {
	c.probs = append(c.probs, Problem{Field: field, Message: msg})
}

func join(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}

func isNull(v json.RawMessage) bool { return strings.TrimSpace(string(v)) == "null" }

func (c *checker) object(field string, v json.RawMessage) map[string]json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(v, &obj); err != nil || isNull(v) {
		c.add(field, "must be an object")
		return nil
	}
	return obj
}

func (c *checker) unknown(parent string, obj map[string]json.RawMessage, allowed []string) {
	var names []string
	for k := range obj {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if !contains(allowed, k) {
			c.add(join(parent, k), "unknown field")
		}
	}
}

func (c *checker) str(field string, v json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(v, &s); err != nil || isNull(v) {
		c.add(field, "must be a string")
		return "", false
	}
	return s, true
}

func (c *checker) pattern(field string, v json.RawMessage, re *regexp.Regexp, what string) {
	if s, ok := c.str(field, v); ok && !re.MatchString(s) {
		c.add(field, fmt.Sprintf("must be %s, got %q", what, s))
	}
}

func (c *checker) settings(parent string, obj map[string]json.RawMessage) {
	for _, name := range settingsFields {
		v, ok := obj[name]
		if !ok {
			continue
		}
		field := join(parent, name)
		if name == "threshold_pct" {
			var n float64
			if err := json.Unmarshal(v, &n); err != nil || isNull(v) || n != math.Trunc(n) {
				c.add(field, "must be a whole number")
				continue
			}
			if n < minThreshold || n > maxThreshold {
				c.add(field, fmt.Sprintf("must be between %d and %d, got %v", minThreshold, maxThreshold, n))
			}
			continue
		}
		if s, ok := c.str(field, v); ok && !contains(enums[name], s) {
			c.add(field, fmt.Sprintf("must be one of %s, got %q", strings.Join(enums[name], ", "), s))
		}
	}
}

func (c *checker) scope(field string, v json.RawMessage) {
	obj := c.object(field, v)
	if obj == nil {
		return
	}
	c.unknown(field, obj, scopeFields)
	acct, hasAcct := obj["account_id"]
	ou, hasOU := obj["ou_id"]
	switch {
	case hasAcct && hasOU:
		c.add(field, "set account_id or ou_id, not both")
	case !hasAcct && !hasOU:
		c.add(field, "account_id or ou_id is required")
	case hasAcct:
		c.pattern(join(field, "account_id"), acct, accountIDRe, "a 12-digit account id")
	default:
		c.pattern(join(field, "ou_id"), ou, ouIDRe, "an OU id like ou-ab12-cd34ef56")
	}
	ceiling, ok := obj["ceiling_usd"]
	if !ok {
		c.add(join(field, "ceiling_usd"), "is required")
	} else {
		var n float64
		if err := json.Unmarshal(ceiling, &n); err != nil || isNull(ceiling) {
			c.add(join(field, "ceiling_usd"), "must be a number")
		} else if n <= 0 {
			c.add(join(field, "ceiling_usd"), fmt.Sprintf("must be greater than 0, got %v", n))
		}
	}
	c.settings(field, obj)
}

func (c *checker) alertEmails(v json.RawMessage) {
	var list []json.RawMessage
	if err := json.Unmarshal(v, &list); err != nil || isNull(v) {
		c.add("alert_emails", "must be an array of email addresses")
		return
	}
	if len(list) > maxAlertEmails {
		c.add("alert_emails", fmt.Sprintf("lists %d addresses; at most %d are allowed (use a distribution list for more)", len(list), maxAlertEmails))
	}
	for i, raw := range list {
		field := fmt.Sprintf("alert_emails[%d]", i)
		if s, ok := c.str(field, raw); ok && !IsEmail(s) {
			c.add(field, fmt.Sprintf("must be a plain email address like oncall@example.com, got %q", s))
		}
	}
}

// IsEmail reports whether s is a bare email address, with no display name
// or angle brackets.
func IsEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && strings.Contains(s, "@")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
