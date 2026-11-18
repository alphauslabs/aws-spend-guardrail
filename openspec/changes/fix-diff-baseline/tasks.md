# Tasks

## 1. CLI

- [ ] 1.1 `baseline` returns the resolved baseline of design D1 and a description for the "compared with" line (D2); `diff` uses both
- [ ] 1.2 Tests with the Jennah test server: no applied report (newest used); newest is the applied version; newest invalid (applied used); newest only tightens and its report hasn't arrived (newest used, so undoing part of it is loosening); newest loosens (stricter merge used, and the line says so)

## 2. Docs and build

- [ ] 2.1 Docs where `diff` is described say what it compares with; CHANGELOG `Fixed` entry (update the CLI; no stack change); docs style (no em-dashes, Oxford comma)
- [ ] 2.2 Run `goimports`, `make`, `go vet ./...`, and `go test ./...`; verify all pass
