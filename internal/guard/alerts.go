package guard

import (
	"fmt"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
)

// Alert bodies are read by people, so they name targets and reasons in words
// (fix-alert-wording). Events keep the raw forms.

// targetName names a target at the start of a sentence: "Account
// 111122223333" or "OU ou-ab12-cdef3456".
func targetName(t config.Target) string {
	switch t.Kind {
	case "account":
		return "Account " + t.ID
	case "ou":
		return "OU " + t.ID
	default:
		return "Target " + t.String()
	}
}

// targetRef names a target inside a sentence: "account 111122223333" or
// "OU ou-ab12-cdef3456".
func targetRef(t config.Target) string {
	if t.Kind == "account" {
		return "account " + t.ID
	}
	return targetName(t)
}

// liftReason says in a sentence why a freeze was lifted.
func liftReason(code string) string {
	switch code {
	case "budget_raised":
		return "Its ceiling was raised above its spend."
	case "scope_removed":
		return "Its scope was removed from the config."
	case "period_start":
		return "A new budget period started, and its scope is set to lift then (auto_lift)."
	case "stack_deleted":
		return "The guard's stack is being deleted."
	default:
		return fmt.Sprintf("Reason: %s.", code)
	}
}

// withError sets error detail apart on the body's last line.
func withError(body string, err error) string { return alerttext.WithError(body, err) }
