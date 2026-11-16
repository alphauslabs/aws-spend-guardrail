package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/releases"
)

// Guard statuses in a versions listing (add-guard-upgrades D6).
const (
	StatusBehind      = "behind"
	StatusSilent      = "silent"
	StatusUnknown     = "unknown"
	StatusNotCompared = "not compared"
	StatusCurrent     = "current"
)

// SilentAfter is how old a newest heartbeat can be before a guard is silent.
// The guard writes one every day.
const SilentAfter = 48 * time.Hour

// VersionRow is one payer org in a versions listing.
type VersionRow struct {
	Payer         string    `json:"payer"`
	Version       string    `json:"version"`
	CodeSHA256    string    `json:"code_sha256"`
	LastHeartbeat time.Time `json:"last_heartbeat,omitzero"`
	Status        string    `json:"status"`
	Note          string    `json:"note,omitempty"`
}

var statusOrder = map[string]int{StatusBehind: 0, StatusSilent: 1, StatusUnknown: 2, StatusNotCompared: 3, StatusCurrent: 4}

// VersionRows grades what each payer org's guard runs against the latest
// release; latest is "" when it couldn't be read. Behind guards come first.
func VersionRows(seen []jennah.GuardSeen, latest string, now time.Time) []VersionRow {
	rows := make([]VersionRow, 0, len(seen))
	for _, g := range seen {
		r := VersionRow{Payer: g.Payer, Version: g.Version, CodeSHA256: g.CodeSHA256, LastHeartbeat: g.HeartbeatAt}
		n, comparable := releases.Compare(g.Version, latest)
		switch {
		case g.Err != nil:
			r.Status, r.Note = StatusUnknown, "couldn't read the inbox: "+g.Err.Error()
		case g.HeartbeatAt.IsZero():
			r.Status, r.Note = StatusUnknown, "no heartbeat yet"
		case now.Sub(g.HeartbeatAt) > SilentAfter:
			r.Status, r.Note = StatusSilent, fmt.Sprintf("no heartbeat for %d days", int(now.Sub(g.HeartbeatAt).Hours()/24))
		case latest == "":
			r.Status = StatusNotCompared
		case !releases.Valid(g.Version):
			r.Status, r.Note = StatusUnknown, fmt.Sprintf("%q is not a release version", g.Version)
		case comparable && n < 0:
			r.Status, r.Note = StatusBehind, "latest is "+latest
		default:
			r.Status = StatusCurrent
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if a, b := statusOrder[rows[i].Status], statusOrder[rows[j].Status]; a != b {
			return a < b
		}
		return rows[i].Payer < rows[j].Payer
	})
	return rows
}

// Versions lists the guard version each payer org of the MSP runs, or only
// c.Payer's when set. latest is the latest release, or "" with latestErr
// saying why it couldn't be read.
func (c *CLI) Versions(ctx context.Context, latest string, latestErr error, asJSON bool) error {
	payers := []string{c.Payer}
	if c.Payer == "" {
		var err error
		if payers, err = c.J.Payers(ctx, c.MSP); err != nil {
			return err
		}
	}
	rows := VersionRows(c.J.GuardVersions(ctx, c.MSP, payers), latest, c.now())
	if asJSON {
		out := struct {
			MSP    string       `json:"msp"`
			Latest string       `json:"latest_release"`
			Guards []VersionRow `json:"guards"`
		}{c.MSP, latest, rows}
		enc := json.NewEncoder(c.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	if latestErr != nil {
		fmt.Fprintf(c.Status, "couldn't check for a newer release (%v); versions are listed without comparison. Pass --latest vX.Y.Z to compare.\n", latestErr)
	} else if latest != "" {
		fmt.Fprintf(c.Out, "latest release %s\n", latest)
	}
	if len(rows) == 0 {
		fmt.Fprintf(c.Out, "no payer orgs found for MSP %s\n", c.MSP)
		return nil
	}
	w := tabwriter.NewWriter(c.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PAYER\tVERSION\tSTATUS\tLAST HEARTBEAT\tCODE SHA256\tNOTE")
	for _, r := range rows {
		hb := "-"
		if !r.LastHeartbeat.IsZero() {
			hb = r.LastHeartbeat.UTC().Format("2006-01-02 15:04 UTC")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Payer, dash(r.Version), r.Status, hb, dash(r.CodeSHA256), r.Note)
	}
	return w.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
