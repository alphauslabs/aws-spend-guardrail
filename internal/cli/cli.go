// Package cli implements the guardrail command's subcommands against a Jennah
// client, so they are tested without a real Jennah.
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	sdk "github.com/alphauslabs/jennah-sdk-go"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

// CLI holds what every subcommand needs.
type CLI struct {
	J      *jennah.Client
	MSP    string
	Out    io.Writer // documents and results
	Status io.Writer // status lines, kept apart so pulled documents stay clean
	Now    func() time.Time
}

func (c *CLI) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Setup creates an MSP's datasets and declares their tables. Running it again
// changes nothing.
func (c *CLI) Setup(ctx context.Context) error {
	if err := schema.ValidateMSPID(c.MSP); err != nil {
		return err
	}
	changed := false
	for _, d := range []struct {
		id     string
		tables []*datastorev1.TableDeclaration
	}{
		{schema.ConfigDatasetID(c.MSP), schema.ConfigTables()},
		{schema.InboxDatasetID(c.MSP), schema.InboxTables()},
	} {
		_, _, err := c.J.SDK().Datasets.Create(ctx, &datastorev1.CreateDatasetRequest{DatasetId: d.id, DisplayName: "aws-spend-guardrail " + d.id})
		switch {
		case err == nil:
			changed = true
			fmt.Fprintf(c.Out, "created dataset %s\n", d.id)
		case sdk.IsAlreadyCommitted(err):
			fmt.Fprintf(c.Out, "dataset %s exists\n", d.id)
		default:
			return fmt.Errorf("create dataset %s: %w", d.id, err)
		}
		ds := c.J.SDK().Dataset(d.id)
		have, err := ds.Schema.Get(ctx)
		if err != nil {
			return fmt.Errorf("read schema of %s: %w", d.id, err)
		}
		if !hasTables(have, d.tables) {
			if _, err := ds.Schema.Declare(ctx, &datastorev1.DeclareTablesRequest{Tables: d.tables}); err != nil {
				return fmt.Errorf("declare tables in %s: %w", d.id, err)
			}
			changed = true
			fmt.Fprintf(c.Out, "declared tables in %s (layout version %d)\n", d.id, schema.LayoutVersion)
		}
	}
	if !changed {
		fmt.Fprintln(c.Out, "nothing changed")
	}
	fmt.Fprintln(c.Out, "\nGuard credentials (store each in the payer account's Secrets Manager):")
	for _, n := range schema.GuardCredentials(c.MSP) {
		fmt.Fprintf(c.Out, "  %-6s dataset selector %s, permissions %s\n", n.Name, n.DatasetID, strings.Join(n.Permissions, ", "))
	}
	fmt.Fprintf(c.Out, "Operators for this MSP need dataset selector %s.\n", schema.MSPSelector(c.MSP))
	return nil
}

func hasTables(have *datastorev1.GetSchemaResponse, want []*datastorev1.TableDeclaration) bool {
	cols := map[string]map[string]bool{}
	for _, t := range have.GetTables() {
		cols[t.GetName()] = map[string]bool{}
		for _, c := range t.GetColumns() {
			cols[t.GetName()][c.GetName()] = true
		}
	}
	for _, t := range want {
		for _, c := range t.GetColumns() {
			if !cols[t.GetName()][c.GetName()] {
				return false
			}
		}
	}
	return true
}

// Pull writes a config version's document. Version 0 means the newest. The
// applied and pending status goes to Status, never into the document.
func (c *CLI) Pull(ctx context.Context, version int64) error {
	ds := schema.ConfigDatasetID(c.MSP)
	var v *jennah.Version
	var err error
	if version == 0 {
		v, err = c.J.LatestVersion(ctx, ds)
	} else {
		v, err = c.J.GetVersion(ctx, ds, version)
	}
	if err != nil {
		return err
	}
	if v == nil {
		if version == 0 {
			return errors.New("no config version has been published yet")
		}
		return fmt.Errorf("config version %d does not exist", version)
	}
	if _, err := c.Out.Write(append(pretty(v.Document), '\n')); err != nil {
		return err
	}
	applied, err := c.appliedVersion(ctx)
	if err != nil {
		fmt.Fprintf(c.Status, "version %d (could not read which version is applied: %v)\n", v.Number, err)
		return nil
	}
	latest, _ := c.J.LatestVersion(ctx, ds)
	switch {
	case applied == 0:
		fmt.Fprintf(c.Status, "version %d; the guard has not applied any version yet\n", v.Number)
	case latest != nil && latest.Number > applied:
		fmt.Fprintf(c.Status, "version %d; version %d is applied, version %d is pending\n", v.Number, applied, latest.Number)
	default:
		fmt.Fprintf(c.Status, "version %d; version %d is applied\n", v.Number, applied)
	}
	return nil
}

func (c *CLI) appliedVersion(ctx context.Context) (int64, error) {
	e, err := c.J.LatestEvent(ctx, schema.InboxDatasetID(c.MSP), schema.KindConfigApplied)
	if err != nil || e == nil {
		return 0, err
	}
	return e.ConfigVersion, nil
}

func pretty(raw []byte) []byte {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return b
}

// Validate checks a document and prints every problem.
func (c *CLI) Validate(raw []byte, payer string) error {
	if _, err := config.Parse(raw, config.Options{PayerAccountID: payer}); err != nil {
		return err
	}
	fmt.Fprintln(c.Out, "valid")
	return nil
}

// Diff shows how a document differs from the applied version, each change
// classified as tightening or loosening.
func (c *CLI) Diff(ctx context.Context, raw []byte) error {
	_, _, err := c.diff(ctx, raw)
	return err
}

func (c *CLI) diff(ctx context.Context, raw []byte) (*config.Document, []config.Change, error) {
	base, baseVersion, err := c.baseline(ctx)
	if err != nil {
		return nil, nil, err
	}
	opts := config.Options{}
	if base != nil {
		opts.PayerAccountID = base.PayerAccountID
	}
	doc, err := config.Parse(raw, opts)
	if err != nil {
		return nil, nil, err
	}
	var baseResolved *config.Resolved
	if base != nil {
		baseResolved = base.Resolve()
	}
	changes := config.Diff(baseResolved, doc.Resolve())
	if baseVersion == 0 {
		fmt.Fprintln(c.Out, "compared with: nothing published yet")
	} else {
		fmt.Fprintf(c.Out, "compared with: version %d\n", baseVersion)
	}
	if len(changes) == 0 {
		fmt.Fprintln(c.Out, "no differences")
		return doc, nil, nil
	}
	for _, ch := range changes {
		fmt.Fprintf(c.Out, "  %s\n", ch)
	}
	if config.HasLoosening(changes) {
		fmt.Fprintln(c.Out, "This version loosens protection: its tightening parts apply at once, the rest after approval and the delay.")
	} else {
		fmt.Fprintln(c.Out, "This version only tightens: it applies on the guard's next pull.")
	}
	return doc, changes, nil
}

// baseline is the applied version, or the newest one when the guard hasn't
// reported an applied version.
func (c *CLI) baseline(ctx context.Context) (*config.Document, int64, error) {
	ds := schema.ConfigDatasetID(c.MSP)
	applied, err := c.appliedVersion(ctx)
	if err != nil {
		applied = 0
	}
	var v *jennah.Version
	if applied > 0 {
		v, err = c.J.GetVersion(ctx, ds, applied)
	} else {
		v, err = c.J.LatestVersion(ctx, ds)
	}
	if err != nil || v == nil {
		return nil, 0, err
	}
	doc, err := config.Parse(v.Document, config.Options{})
	if err != nil {
		return nil, v.Number, nil // an invalid baseline compares as nothing
	}
	return doc, v.Number, nil
}

// Publish validates a document, shows its classified diff, and writes it as
// the next version. Publishing the same document as the newest version again
// writes nothing.
func (c *CLI) Publish(ctx context.Context, raw []byte, note string) error {
	if _, _, err := c.diff(ctx, raw); err != nil {
		return err
	}
	canon, err := canonical(raw)
	if err != nil {
		return err
	}
	ds := schema.ConfigDatasetID(c.MSP)
	latest, err := c.J.LatestVersion(ctx, ds)
	if err != nil {
		return err
	}
	if latest != nil {
		if same, _ := canonical(latest.Document); string(same) == string(canon) {
			fmt.Fprintf(c.Out, "already published as version %d; nothing written\n", latest.Number)
			return nil
		}
	}
	next := int64(1)
	if latest != nil {
		next = latest.Number + 1
	}
	sum := sha256.Sum256(canon)
	err = c.J.InsertVersion(ctx, ds, jennah.Version{Number: next, Document: canon, PublishedAt: c.now().UTC(), Note: note},
		jennah.VersionIdempotencyKey(next, hex.EncodeToString(sum[:])))
	if errors.Is(err, jennah.ErrVersionExists) {
		return fmt.Errorf("version %d was published by someone else first; pull, review and publish again", next)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "published version %d\n", next)
	return nil
}

// canonical re-encodes JSON compactly with sorted keys, so formatting doesn't
// make the same document look new.
func canonical(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
