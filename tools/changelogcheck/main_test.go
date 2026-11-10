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

## v0.1.0

### Security-relevant

### Added
- First release.
`

func TestEntry(t *testing.T) {
	e, err := Entry(cl, "v0.2.0")
	if err != nil || !strings.Contains(e, "list accounts") || strings.Contains(e, "First release") {
		t.Fatalf("v0.2.0: %q %v", e, err)
	}
	if _, err := Entry(cl, "v0.3.0"); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("missing version: %v", err)
	}
	if _, err := Entry(cl, "v0.1.0"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty security section: %v", err)
	}
	noSec := "## v0.4.0\n\n### Added\n- x\n"
	if _, err := Entry(noSec, "v0.4.0"); err == nil || !strings.Contains(err.Error(), "no \"### Security-relevant\"") {
		t.Fatalf("no security section: %v", err)
	}
}
