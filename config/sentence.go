package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Sentence describes the change for a person deciding on it, as one full
// sentence (improve-approval-wording). Approvals use it; `guardrail diff` and
// inbox events keep String(), whose readers are operators and machines.
func (c Change) Sentence() string {
	t := targetRef(c.Target)
	switch c.Field {
	case "scope":
		if c.From == "" {
			return fmt.Sprintf("Start protecting %s.", t)
		}
		return fmt.Sprintf("Stop protecting %s.", t)
	case "ceiling_usd":
		return fmt.Sprintf("%s the ceiling of %s from %s USD to %s USD.", raiseOrLower(c.From, c.To), t, c.From, c.To)
	case "threshold_pct":
		return fmt.Sprintf("%s the freeze threshold of %s from %s%% to %s%% of its ceiling.", raiseOrLower(c.From, c.To), t, c.From, c.To)
	case "mode":
		if c.To == ModeDryRun {
			return fmt.Sprintf("Switch %s from enforce to dry-run, so it is watched but not frozen.", t)
		}
		return fmt.Sprintf("Switch %s from dry-run to enforce, so it freezes at its threshold.", t)
	case "freeze_style":
		if c.To == StyleA {
			return fmt.Sprintf("Switch %s from freeze style B to the narrower style A.", t)
		}
		return fmt.Sprintf("Switch %s from freeze style A to the broader style B.", t)
	case "month_end":
		if c.To == MonthEndAutoLift {
			return fmt.Sprintf("Lift the freeze on %s when a new budget period starts (auto_lift).", t)
		}
		return fmt.Sprintf("Keep %s frozen into a new budget period until a raise is approved (stay_frozen).", t)
	case "period":
		return fmt.Sprintf("Change the budget period of %s from %s to %s.", t, c.From, c.To)
	case "excluded":
		if c.To != "" {
			return fmt.Sprintf("Mark %s as left out of protection on purpose, so it isn't reported as unprotected.", t)
		}
		return fmt.Sprintf("Stop treating %s as left out on purpose, so it is reported if unprotected.", t)
	case FieldAlertEmail:
		if c.To != "" {
			return fmt.Sprintf("Add %s as an alert recipient.", c.To)
		}
		return fmt.Sprintf("Stop sending alerts to %s.", c.From)
	case FieldAIScope:
		if c.From == "" {
			return fmt.Sprintf("Start the AI guard on %s.", t)
		}
		return fmt.Sprintf("Stop the AI guard on %s.", t)
	case "ai.hourly_usd", "ai.daily_usd", "ai.monthly_usd":
		return aiCapSentence(strings.TrimSuffix(strings.TrimPrefix(c.Field, "ai."), "_usd"), t, c.From, c.To)
	case "ai.mode":
		who := "the AI guard"
		if c.Target.Kind != "" {
			who = "the AI guard on " + t
		}
		if c.To == ModeDryRun {
			return fmt.Sprintf("Switch %s from enforce to dry-run, so it reports but doesn't cut off AI access.", who)
		}
		return fmt.Sprintf("Switch %s from dry-run to enforce, so it cuts off AI access.", who)
	case FieldAIModels:
		return allowlistSentence("model", "models", c.From, c.To)
	case FieldAIRegions:
		return allowlistSentence("region", "regions", c.From, c.To)
	case FieldAIAPIKey:
		if c.To != "" {
			return fmt.Sprintf("Allow %s to call Bedrock with an API key.", c.To)
		}
		return fmt.Sprintf("Stop allowing %s to call Bedrock with an API key.", c.From)
	case FieldAIPrice:
		return priceSentence(c.From, c.To)
	case FieldAILift:
		if c.To != "" {
			return fmt.Sprintf("Lift the AI cutoff %s.", c.To)
		}
		return fmt.Sprintf("Remove the spent lift for AI cutoff %s from the config.", c.From)
	}
	from, to := c.From, c.To
	if from == "" {
		from = "none"
	}
	if to == "" {
		to = "none"
	}
	if t != "" {
		return fmt.Sprintf("Change %s of %s from %s to %s.", c.Field, t, from, to)
	}
	return fmt.Sprintf("Change %s from %s to %s.", c.Field, from, to)
}

// targetRef names a target inside a sentence: "account 111122223333" or
// "OU ou-ab12-cdef3456".
func targetRef(t Target) string {
	switch t.Kind {
	case "account":
		return "account " + t.ID
	case "ou":
		return "OU " + t.ID
	case "":
		return ""
	}
	return t.String()
}

func raiseOrLower(from, to string) string {
	f, err1 := strconv.ParseFloat(from, 64)
	n, err2 := strconv.ParseFloat(to, 64)
	if err1 == nil && err2 == nil && n < f {
		return "Lower"
	}
	return "Raise"
}

func aiCapSentence(period, t, from, to string) string {
	switch {
	case from == "none":
		return fmt.Sprintf("Set a %s AI spend cap of %s USD on %s.", period, to, t)
	case to == "none":
		return fmt.Sprintf("Remove the %s AI spend cap of %s, which was %s USD.", period, t, from)
	}
	return fmt.Sprintf("%s the %s AI spend cap of %s from %s USD to %s USD.", raiseOrLower(from, to), period, t, from, to)
}

func allowlistSentence(one, many, from, to string) string {
	switch {
	case to == allowlistAnyText:
		return fmt.Sprintf("Allow any %s, where only %s were allowed.", one, listText(from))
	case from == allowlistAnyText:
		return fmt.Sprintf("Allow only these %s: %s.", many, listText(to))
	case to != "":
		return fmt.Sprintf("Also allow the %s %s.", one, to)
	}
	return fmt.Sprintf("Stop allowing the %s %s.", one, from)
}

// listText turns the diff's "[a b]" list form into "a, b".
func listText(s string) string {
	return strings.Join(strings.Fields(strings.Trim(s, "[]")), ", ")
}

// priceSentence reads the "<model> in <x>/out <y> per 1k" form the diff uses.
func priceSentence(from, to string) string {
	model, was := splitPrice(from)
	m2, now := splitPrice(to)
	if model == "" {
		model = m2
	}
	switch {
	case from == "":
		return fmt.Sprintf("Set a price override for %s of %s.", model, now)
	case to == "":
		return fmt.Sprintf("Remove the price override for %s, which was %s.", model, was)
	}
	return fmt.Sprintf("Change the price override for %s from %s to %s.", model, was, now)
}

func splitPrice(s string) (model, price string) {
	if s == "" {
		return "", ""
	}
	model, rest, _ := strings.Cut(s, " ")
	var in, out float64
	if _, err := fmt.Sscanf(rest, "in %g/out %g per 1k", &in, &out); err != nil {
		return model, rest
	}
	return model, fmt.Sprintf("%g USD input and %g USD output per 1,000 tokens", in, out)
}
