// Command changelogcheck fails unless CHANGELOG.md has an entry for a version
// with a non-empty "Security-relevant" section.
//
//	go run ./tools/changelogcheck CHANGELOG.md v0.1.0
//
// With -notes, it prints the version's entry, for the release notes.
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
// next "## "), and fails if it is missing or has no non-empty
// "### Security-relevant" subsection.
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
	sec := -1
	for i, l := range section {
		if strings.TrimSpace(l) == "### Security-relevant" {
			sec = i
			break
		}
	}
	if sec < 0 {
		return "", fmt.Errorf("the %s entry has no \"### Security-relevant\" section", version)
	}
	body := ""
	for _, l := range section[sec+1:] {
		if strings.HasPrefix(l, "### ") {
			break
		}
		body += strings.TrimSpace(l)
	}
	if body == "" {
		return "", errors.New("the Security-relevant section of " + version + " is empty; write \"None.\" if nothing changed")
	}
	return strings.Join(section, "\n") + "\n", nil
}
