// Command changelogcheck fails unless CHANGELOG.md has an entry for a version
// with non-empty "Upgrade" and "Security-relevant" sections.
//
//	go run ./tools/changelogcheck CHANGELOG.md v0.1.0
//
// With -notes, it prints the version's entry, Upgrade section first, for the
// release notes.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	notes := flag.Bool("notes", false, "print the version's entry")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: changelogcheck [-notes] CHANGELOG.md vX.Y.Z")
		os.Exit(2)
	}
	b, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	entry, err := Entry(string(b), flag.Arg(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *notes {
		fmt.Print(entry)
	}
}

// Entry returns a version's section ("## [vX.Y.Z]" or "## vX.Y.Z", up to the
// next "## ") with its "### Upgrade" subsection moved first, and fails if it
// is missing or lacks a non-empty "### Upgrade" or "### Security-relevant"
// subsection (add-guard-upgrades D1).
func Entry(changelog, version string) (string, error) {
	lines := strings.Split(changelog, "\n")
	start := -1
	for i, l := range lines {
		h := strings.TrimSpace(strings.TrimPrefix(l, "## "))
		if strings.HasPrefix(l, "## ") && (h == version || strings.HasPrefix(h, "["+version+"]") || strings.HasPrefix(h, version+" ")) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("CHANGELOG has no entry for %s", version)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	section := lines[start+1 : end]
	up, upFrom, upTo, err := subsection(section, version, "Upgrade", "write \"None.\" if upgrading needs no action")
	if err != nil {
		return "", err
	}
	if _, _, _, err := subsection(section, version, "Security-relevant", "write \"None.\" if nothing changed"); err != nil {
		return "", err
	}
	rest := append(append([]string{}, section[:upFrom]...), section[upTo:]...)
	return strings.Join(append(up, rest...), "\n") + "\n", nil
}

// subsection finds "### <name>" in an entry and returns its lines (heading
// included) and their range, failing when it is missing or empty.
func subsection(section []string, version, name, hint string) ([]string, int, int, error) {
	from := -1
	for i, l := range section {
		if strings.TrimSpace(l) == "### "+name {
			from = i
			break
		}
	}
	if from < 0 {
		return nil, 0, 0, fmt.Errorf("the %s entry has no \"### %s\" section", version, name)
	}
	to := len(section)
	body := ""
	for i := from + 1; i < len(section); i++ {
		if strings.HasPrefix(section[i], "### ") {
			to = i
			break
		}
		body += strings.TrimSpace(section[i])
	}
	if body == "" {
		return nil, 0, 0, errors.New("the " + name + " section of " + version + " is empty; " + hint)
	}
	return section[from:to], from, to, nil
}
