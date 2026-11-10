// Command pincheck fails when a GitHub Actions workflow uses a third-party
// action by anything other than a full commit hash.
//
//	go run ./tools/pincheck .github/workflows/*.yml
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

var (
	usesRe   = regexp.MustCompile(`^\s*-?\s*uses:\s*["']?([^"'\s#]+)`)
	pinnedRe = regexp.MustCompile(`@[0-9a-f]{40}$`)
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pincheck workflow.yml ...")
		os.Exit(2)
	}
	bad := 0
	for _, path := range os.Args[1:] {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, p := range Unpinned(f) {
			fmt.Printf("%s:%d: %s is not pinned to a full commit hash\n", path, p.Line, p.Uses)
			bad++
		}
		f.Close()
	}
	if bad > 0 {
		os.Exit(1)
	}
}

// Problem is one unpinned action reference.
type Problem struct {
	Line int
	Uses string
}

// Unpinned lists uses: references that aren't local actions and aren't pinned
// by a 40-character commit hash.
func Unpinned(r io.Reader) []Problem {
	var out []Problem
	s := bufio.NewScanner(r)
	for n := 1; s.Scan(); n++ {
		m := usesRe.FindStringSubmatch(s.Text())
		if m == nil {
			continue
		}
		ref := m[1]
		if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "docker://") && strings.Contains(ref, "@sha256:") {
			continue
		}
		if !pinnedRe.MatchString(ref) {
			out = append(out, Problem{Line: n, Uses: ref})
		}
	}
	return out
}
