package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

var vNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func TestVersionRows(t *testing.T) {
	recent := vNow.Add(-time.Hour)
	seen := []jennah.GuardSeen{
		{Payer: "111111111111", Version: "v0.2.0", CodeSHA256: "a=", HeartbeatAt: recent},
		{Payer: "222222222222", Version: "v0.1.0", CodeSHA256: "b=", HeartbeatAt: recent},
		{Payer: "333333333333", Version: "v0.2.0", HeartbeatAt: vNow.Add(-3 * 24 * time.Hour)},
		{Payer: "444444444444"},
		{Payer: "555555555555", Err: errors.New("PermissionDenied")},
		{Payer: "666666666666", Version: "9c6aa3c", HeartbeatAt: recent},
		{Payer: "777777777777", Version: "v0.3.0", HeartbeatAt: recent}, // a release newer than "latest" (pre-release testing)
	}
	got := VersionRows(seen, "v0.2.0", vNow)
	want := []struct{ payer, status, note string }{
		{"222222222222", StatusBehind, "latest is v0.2.0"},
		{"333333333333", StatusSilent, "no heartbeat for 3 days"},
		{"444444444444", StatusUnknown, "no heartbeat yet"},
		{"555555555555", StatusUnknown, "couldn't read the inbox: PermissionDenied"},
		{"666666666666", StatusUnknown, `"9c6aa3c" is not a release version`},
		{"111111111111", StatusCurrent, ""},
		{"777777777777", StatusCurrent, ""},
	}
	if len(got) != len(want) {
		t.Fatalf("rows %+v", got)
	}
	for i, w := range want {
		if got[i].Payer != w.payer || got[i].Status != w.status || got[i].Note != w.note {
			t.Errorf("row %d: %+v, want %+v", i, got[i], w)
		}
	}
	// Without the latest release, nothing is graded current or behind.
	for _, r := range VersionRows(seen[:2], "", vNow) {
		if r.Status != StatusNotCompared {
			t.Errorf("no latest: %+v", r)
		}
	}
}

func TestVersionsCommand(t *testing.T) {
	ctx := context.Background()
	c, f, out, status := newCLI(t)
	c.Payer, c.Now = "", func() time.Time { return vNow }
	for _, p := range []string{"111111111111", "222222222222"} {
		if _, err := f.CreateDataset(ctx, &datastorev1.CreateDatasetRequest{DatasetId: schema.InboxDatasetID("msp_a", p)}); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(payer, version string) {
		d, _ := json.Marshal(map[string]any{"guard_version": version, "code_sha256": "h-" + version})
		if err := c.J.CommitEvent(ctx, schema.InboxDatasetID("msp_a", payer), jennah.Event{ID: "hb-" + payer, OccurredAt: vNow.Add(-time.Hour), Kind: schema.KindHeartbeat, GuardVersion: version, Detail: d}); err != nil {
			t.Fatal(err)
		}
	}
	commit("111111111111", "v0.2.0")
	commit("222222222222", "v0.1.0")

	if err := c.Versions(ctx, "v0.2.0", nil, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || lines[0] != "latest release v0.2.0" || !strings.HasPrefix(lines[2], "222222222222  v0.1.0   behind") || !strings.Contains(lines[3], "current") {
		t.Fatalf("table:\n%s", out)
	}

	out.Reset()
	if err := c.Versions(ctx, "", errors.New("GitHub answered 403"), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.String(), "couldn't check for a newer release (GitHub answered 403)") || strings.Count(out.String(), StatusNotCompared) != 2 {
		t.Fatalf("no latest:\n%s\n%s", out, status)
	}

	out.Reset()
	c.Payer = "222222222222"
	if err := c.Versions(ctx, "v0.2.0", nil, true); err != nil {
		t.Fatal(err)
	}
	var js struct {
		Latest string       `json:"latest_release"`
		Guards []VersionRow `json:"guards"`
	}
	if err := json.Unmarshal(out.Bytes(), &js); err != nil || js.Latest != "v0.2.0" || len(js.Guards) != 1 || js.Guards[0].Status != StatusBehind || js.Guards[0].CodeSHA256 != "h-v0.1.0" {
		t.Fatalf("json %v:\n%s", err, out)
	}
}
