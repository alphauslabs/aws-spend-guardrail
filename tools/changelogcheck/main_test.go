package main

import (
	"strings"
	"testing"
)

const cl = `# Changelog

## [Unreleased]

## [v0.2.0] - 2026-12-01

### Added
- Something.

### Security-relevant
- The guard's role can now list accounts.

### Upgrade
1. Rerun guardrail setup.

## v0.1.0

### Upgrade
None.

### Security-relevant

### Added
- First release.
`

func TestEntry(t *testing.T) {
	e, err := Entry(cl, "v0.2.0")
	if err != nil || !strings.Contains(e, "list accounts") || strings.Contains(e, "First release") {
		t.Fatalf("v0.2.0: %q %v", e, err)
	}
	if !strings.HasPrefix(strings.TrimSpace(e), "### Upgrade\n1. Rerun guardrail setup.") || strings.Count(e, "### Upgrade") != 1 || !strings.Contains(e, "### Added") {
		t.Fatalf("the Upgrade section must come first, once:\n%s", e)
	}
	if _, err := Entry(cl, "v0.3.0"); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("missing version: %v", err)
	}
	if _, err := Entry(cl, "v0.1.0"); err == nil || !strings.Contains(err.Error(), "Security-relevant section of v0.1.0 is empty") {
		t.Fatalf("empty security section: %v", err)
	}
	noSec := "## v0.4.0\n\n### Upgrade\nNone.\n\n### Added\n- x\n"
	if _, err := Entry(noSec, "v0.4.0"); err == nil || !strings.Contains(err.Error(), "no \"### Security-relevant\"") {
		t.Fatalf("no security section: %v", err)
	}
	noUp := "## v0.4.0\n\n### Security-relevant\nNone.\n"
	if _, err := Entry(noUp, "v0.4.0"); err == nil || !strings.Contains(err.Error(), "no \"### Upgrade\"") {
		t.Fatalf("no upgrade section: %v", err)
	}
	emptyUp := "## v0.4.0\n\n### Upgrade\n\n### Security-relevant\nNone.\n"
	if _, err := Entry(emptyUp, "v0.4.0"); err == nil || !strings.Contains(err.Error(), "Upgrade section of v0.4.0 is empty") {
		t.Fatalf("empty upgrade section: %v", err)
	}
}
