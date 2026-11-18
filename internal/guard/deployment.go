package guard

import (
	"context"
	"fmt"
)

// MinTemplateRevision is the lowest stack template revision this code needs
// (add-guard-upgrades D2). Bump it, and TEMPLATE_REVISION in
// guard/template.yaml, whenever the code needs a template resource,
// permission, parameter, or environment variable older templates lack.
const MinTemplateRevision = 7

// kindDeployment keys the deployment mismatches already alerted, one per pair
// of values, in the reported store.
const kindDeployment = "deployment"

// labelMismatch reports whether the stack's GuardVersion label differs from
// the version built into the code; "dev" or an empty label on either side is
// not a mismatch.
func (g *Guard) labelMismatch() bool {
	a, b := g.S.StackGuardVersion, g.S.GuardVersion
	return a != "" && b != "" && a != "dev" && b != "dev" && a != b
}

// CheckDeployment alerts once for each stack that doesn't match its code: a
// template older than the code needs (D2), or a GuardVersion label that
// differs from the built-in version (D4). The guard keeps running either way.
// Called at cold start, after SetCodeHash.
func (g *Guard) CheckDeployment(ctx context.Context) {
	once := func(key, subject, msg string) {
		seen, err := g.State.Reported(ctx, kindDeployment)
		if err != nil {
			return // alert later rather than on every cold start while state is unreadable
		}
		if _, ok := seen[key]; ok {
			return
		}
		g.alert(ctx, subject, msg)
		_ = g.State.MarkReported(ctx, kindDeployment, key, g.now())
	}
	if have := g.S.TemplateRevision; have < MinTemplateRevision {
		haveText := fmt.Sprintf("revision %d", have)
		if have == 0 {
			haveText = "no revision (a template from before revisions were recorded)"
		}
		once(fmt.Sprintf("template:%d<%d", have, MinTemplateRevision), "guardrail: stack template is older than this guard needs",
			fmt.Sprintf("The guard's code (%s) needs stack template revision %d, and the stack has %s. Update the stack with this release's template.yaml (see docs/upgrade.md). Until then the guard keeps running, but features whose resources or permissions the old template lacks fail with their own errors.",
				g.S.GuardVersion, MinTemplateRevision, haveText))
	}
	if g.labelMismatch() {
		once(fmt.Sprintf("label:%s!=%s", g.S.StackGuardVersion, g.S.GuardVersion), "guardrail: stack GuardVersion label is stale",
			fmt.Sprintf("The stack's GuardVersion parameter says %s, but the running code is %s. Events report %s. Set GuardVersion to %s when you next update the stack.",
				g.S.StackGuardVersion, g.S.GuardVersion, g.S.GuardVersion, g.S.GuardVersion))
	}
}
