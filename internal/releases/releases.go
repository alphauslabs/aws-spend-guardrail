// Package releases finds the latest guard release and compares versions
// (add-guard-upgrades D5). Only the CLI uses it; guards never contact GitHub.
package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// LatestURL is GitHub's public API for the repository's latest release.
const LatestURL = "https://api.github.com/repos/alphauslabs/aws-spend-guardrail/releases/latest"

// Latest returns the latest release's tag, such as "v0.2.0". url is LatestURL
// outside tests.
func Latest(ctx context.Context, client *http.Client, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "guardrail-cli")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("read the latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read the latest release: GitHub answered %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("read the latest release: %w", err)
	}
	if _, ok := parse(r.Tag); !ok {
		return "", errors.New("read the latest release: GitHub gave no vX.Y.Z tag")
	}
	return r.Tag, nil
}

var semver = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

func parse(v string) ([3]int, bool) {
	m := semver.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out, true
}

// Compare orders two vX.Y.Z versions: -1, 0, or 1. ok is false when either
// isn't vX.Y.Z (such as "dev" or a commit hash).
func Compare(a, b string) (n int, ok bool) {
	pa, oka := parse(a)
	pb, okb := parse(b)
	if !oka || !okb {
		return 0, false
	}
	for i := range pa {
		switch {
		case pa[i] < pb[i]:
			return -1, true
		case pa[i] > pb[i]:
			return 1, true
		}
	}
	return 0, true
}

// Valid reports whether v is a vX.Y.Z version.
func Valid(v string) bool { _, ok := parse(v); return ok }
