package guard

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// Outcomes of a new install's check, as the guard_installed event reports
// them (fix-reinstall-loosening).
const (
	InstallResumed = "resumed"  // an earlier install's applied version is back in force
	InstallFresh   = "fresh"    // no version was ever applied: a first install
	InstallNotDone = "not_done" // the earlier applied version couldn't be read
)

var errNoInbox = errors.New("the guard has no inbox reader")

// startInstall gives a guard that starts with no stored state an install ID,
// so its first run checks what was in force before it.
func (g *Guard) startInstall(st *State) {
	if st.InstallID == 0 && st.AppliedVersion == 0 && st.Effective == nil {
		st.InstallID = g.now().Unix()
	}
}

// versionEventID is the ID of an event about a config version: "<kind>:<N>",
// with the install appended when the state has one, so two installs that
// apply the same version both get their event recorded (design D4).
func versionEventID(st *State, kind string, version int64) string {
	if st.InstallID == 0 {
		return fmt.Sprintf("%s:%d", kind, version)
	}
	return fmt.Sprintf("%s:%d:i%d", kind, version, st.InstallID)
}

// checkInstall runs once on a new install's first run that reaches Jennah. It
// puts back in force the last version an earlier install applied in full, so
// the latest version is then handled as a change from it and its loosening
// waits for approval (design D1). An error means Jennah couldn't be reached
// and the check runs again on the next run.
func (g *Guard) checkInstall(ctx context.Context, st *State, latest *jennah.Version) error {
	if st.InstallID == 0 || st.InstallChecked {
		return nil
	}
	report := func(outcome string, previous int64) {
		st.InstallChecked = true
		g.emit(ctx, schema.KindGuardInstalled, fmt.Sprintf("%s:i%d", schema.KindGuardInstalled, st.InstallID), "", previous,
			map[string]any{"outcome": outcome, "previous_applied_version": previous, "latest_version": latest.Number, "install_id": st.InstallID})
	}
	notDone := func(why string, err error) {
		g.alert(ctx, "guardrail: reinstall check not done", withError(fmt.Sprintf(
			"This guard started with no stored state, so it is a new install. %s So the guard applies the latest valid config version as it is, without checking it against the version in force before. Mint the reader key with the payer org's inbox in its reach (see docs/operating.md), so the next reinstall can check.", why), err))
		report(InstallNotDone, 0)
	}

	if g.Inbox == nil {
		notDone("It has no way to read the inbox.", errNoInbox)
		return nil
	}
	prev, err := g.Inbox.LatestEvent(ctx, schema.InboxDatasetID(g.S.MSPID, g.S.PayerAccountID), schema.KindConfigApplied)
	if err != nil {
		if !isNoAccess(err) {
			return fmt.Errorf("cannot read the inbox for the version in force before this install: %w", err)
		}
		notDone("Its reader key can't read the payer org's inbox, where the version in force before is recorded.", err)
		return nil
	}
	if prev == nil || prev.ConfigVersion == 0 {
		report(InstallFresh, 0)
		return nil
	}
	v, err := g.Config.GetVersion(ctx, schema.ConfigDatasetID(g.S.MSPID, g.S.PayerAccountID), prev.ConfigVersion)
	if err != nil && !isNoAccess(err) {
		return err
	}
	if err == nil && v == nil {
		err = fmt.Errorf("config version %d does not exist", prev.ConfigVersion)
	}
	var doc *config.Document
	if err == nil {
		doc, err = config.Parse(v.Document, config.Options{PayerAccountID: g.S.PayerAccountID})
	}
	if err != nil {
		notDone(fmt.Sprintf("Config version %d was in force before, but the guard can't read it.", prev.ConfigVersion), err)
		return nil
	}
	desired := doc.Resolve()
	report(InstallResumed, prev.ConfigVersion)
	g.applyFull(ctx, st, prev.ConfigVersion, desired, config.Diff(st.Effective, desired))
	return nil
}

// isNoAccess is true for a Jennah error that a key's reach causes, rather
// than an outage.
func isNoAccess(err error) bool {
	s := err.Error()
	return strings.Contains(s, "PermissionDenied") || strings.Contains(s, "NotFound")
}
