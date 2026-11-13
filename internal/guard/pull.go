package guard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// Pull checks Jennah for a newer config version, applies what may be applied,
// and reconciles budgets with the effective config. When Jennah is unreachable
// or the newest version is invalid, the last valid config stays in force.
func (g *Guard) Pull(ctx context.Context) error {
	st, err := g.State.Load(ctx)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	g.startInstall(st)
	g.trackRefresh(ctx, st, g.evaluate(ctx, st))
	g.pullPrices(ctx)
	// Recipients follow the config in force, so this runs even when Jennah is
	// unreachable (and then changes nothing).
	syncErr := g.syncRecipients(ctx, st)
	if err := g.State.Save(ctx, st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	if st.Effective == nil {
		return syncErr // nothing configured yet
	}
	return errors.Join(syncErr, g.Reconcile(ctx, st.Effective))
}

// OutageReminder is how often an ongoing config refresh failure is alerted
// again.
const OutageReminder = 6 * time.Hour

// trackRefresh alerts when config refresh starts failing, reminds at most
// every OutageReminder while it keeps failing, and alerts once on recovery.
func (g *Guard) trackRefresh(ctx context.Context, st *State, err error) {
	now := g.now()
	if err == nil {
		if !st.FailingSince.IsZero() {
			g.alert(ctx, "guardrail: config refresh recovered", fmt.Sprintf(
				"The guard reads its config from Jennah again, after failing for %s (since %s).",
				durationWords(now.Sub(st.FailingSince).Round(time.Minute)), utcText(st.FailingSince)))
		}
		st.FailingSince, st.LastFailureAlert = time.Time{}, time.Time{}
		return
	}
	switch {
	case st.FailingSince.IsZero():
		st.FailingSince, st.LastFailureAlert = now, now
		g.alert(ctx, "guardrail: config not refreshed", withError(
			"The guard can't apply its config from Jennah, so the last valid config stays in force. You'll get a reminder every 6 hours while this lasts, and an alert when it recovers.", err))
	case now.Sub(st.LastFailureAlert) >= OutageReminder:
		st.LastFailureAlert = now
		g.alert(ctx, "guardrail: config still not refreshed", withError(fmt.Sprintf(
			"The guard still can't apply its config from Jennah, and has been failing since %s (%s). The last valid config stays in force.",
			utcText(st.FailingSince), durationWords(now.Sub(st.FailingSince).Round(time.Minute))), err))
	}
}

func (g *Guard) evaluate(ctx context.Context, st *State) error {
	latest, err := g.Config.LatestVersion(ctx, schema.ConfigDatasetID(g.S.MSPID, g.S.PayerAccountID))
	if err != nil {
		return fmt.Errorf("cannot read config from Jennah: %w", err)
	}
	if latest == nil {
		return nil
	}
	if err := g.checkInstall(ctx, st, latest); err != nil {
		return err
	}

	// A newer version supersedes a pending one.
	if p := st.Pending; p != nil && p.Version != latest.Number && latest.Number > p.Version {
		if p.Closed == "" {
			if err := g.Config.CancelApproval(ctx, p.ApprovalID); err != nil {
				return err
			}
			g.emit(ctx, schema.KindLooseningVoid, versionEventID(st, schema.KindLooseningVoid, p.Version), "", p.Version,
				map[string]any{"approval_id": p.ApprovalID, "superseded_by": latest.Number})
		}
		st.Pending = nil
	}

	if latest.Number <= st.AppliedVersion || latest.Number == st.RejectedVersion {
		return nil
	}

	doc, err := config.Parse(latest.Document, config.Options{PayerAccountID: g.S.PayerAccountID})
	if err != nil {
		st.RejectedVersion = latest.Number
		var ie *config.InvalidError
		var probs []string
		if errors.As(err, &ie) {
			for _, p := range ie.Problems {
				probs = append(probs, p.String())
			}
		} else {
			probs = []string{err.Error()}
		}
		g.emit(ctx, schema.KindConfigRejected, versionEventID(st, schema.KindConfigRejected, latest.Number), "", latest.Number,
			map[string]any{"problems": probs, "kept_version": st.AppliedVersion})
		g.alert(ctx, "guardrail: config version rejected",
			fmt.Sprintf("Config version %d is invalid and was not applied, so version %d stays in force. Fix these problems and publish a new version:\n%s",
				latest.Number, st.AppliedVersion, "- "+strings.Join(probs, "\n- ")))
		return nil
	}
	desired := doc.Resolve()
	changes := config.Diff(st.Effective, desired)

	if !config.HasLoosening(changes) {
		g.applyFull(ctx, st, latest.Number, desired, changes)
		return nil
	}
	return g.handleLoosening(ctx, st, latest.Number, latest.Note, desired, changes)
}

func (g *Guard) applyFull(ctx context.Context, st *State, version int64, desired *config.Resolved, changes []config.Change) {
	st.Effective = desired
	st.AppliedVersion = version
	st.EffectiveVersion = version
	st.Pending = nil
	g.emit(ctx, schema.KindConfigApplied, versionEventID(st, schema.KindConfigApplied, version), "", version,
		g.selfReport(map[string]any{"changes": changeStrings(changes)}))
}

func (g *Guard) handleLoosening(ctx context.Context, st *State, version int64, note string, desired *config.Resolved, changes []config.Change) error {
	// Tightening parts apply at once.
	if st.EffectiveVersion < version {
		merged := config.Stricter(st.Effective, desired)
		if !config.Equal(merged, st.Effective) {
			st.Effective = merged
			g.emit(ctx, schema.KindConfigPartial, versionEventID(st, schema.KindConfigPartial, version), "", version,
				map[string]any{"applied": changeStrings(only(changes, config.Tightening)), "awaiting_approval": changeStrings(only(changes, config.Loosening))})
		}
		st.EffectiveVersion = version
	}

	p := st.Pending
	if p == nil || p.Version != version {
		seen := g.now()
		req, err := g.approvalRequest(ctx, version, note, changes, st.Effective, desired, seen)
		if err != nil {
			return err
		}
		a, err := g.Config.RaiseApproval(ctx, req)
		if err != nil {
			return err
		}
		p = &Pending{Version: version, ApprovalID: a.ID, FirstSeen: seen, Desired: desired, Note: note}
		st.Pending = p
		g.emit(ctx, schema.KindLooseningRaised, versionEventID(st, schema.KindLooseningRaised, version), "", version,
			map[string]any{"approval_id": a.ID, "loosening": changeStrings(only(changes, config.Loosening)), "reason": note})
	}
	if p.Closed != "" {
		return nil
	}

	a, err := g.Config.GetApproval(ctx, p.ApprovalID)
	if err != nil {
		return err
	}
	switch a.Status {
	case jennah.ApprovalApproved:
		if g.now().Sub(p.FirstSeen) < g.S.LooseningDelay {
			return nil // approved, waiting out the delay
		}
		g.emit(ctx, schema.KindLooseningOK, versionEventID(st, schema.KindLooseningOK, version), "", version,
			map[string]any{"approval_id": p.ApprovalID})
		g.applyFull(ctx, st, version, p.Desired, config.Diff(st.Effective, p.Desired))
	case jennah.ApprovalRejected, jennah.ApprovalExpired, jennah.ApprovalCancelled:
		p.Closed = a.Status
		kind := map[jennah.ApprovalStatus]string{
			jennah.ApprovalRejected:  schema.KindLooseningDenied,
			jennah.ApprovalExpired:   schema.KindLooseningExpired,
			jennah.ApprovalCancelled: schema.KindLooseningVoid,
		}[a.Status]
		g.emit(ctx, kind, versionEventID(st, kind, version), "", version,
			map[string]any{"approval_id": p.ApprovalID, "kept": "stricter config"})
	}
	return nil
}

// MaxReasonLen is how much of a version's note an approval shows. Jennah allows
// 16,000 characters in a summary; this leaves room for the list of changes.
const MaxReasonLen = 4000

// reasonText is the "Reason given" part of a loosening approval.
func reasonText(version int64, note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return "No reason was given."
	}
	if r := []rune(note); len(r) > MaxReasonLen {
		return string(r[:MaxReasonLen]) + fmt.Sprintf("\n[shortened; the full note is on config version %d]", version)
	}
	return note
}

// approvalRequest builds the loosening approval an approver decides from
// (improve-approval-wording): a title naming the kind of change, one sentence
// per loosening, what it does to any freeze, the reason given, and exact times.
// effective is the config in force (it still has a removed scope), desired the
// new version, and seen when the guard first saw it.
func (g *Guard) approvalRequest(ctx context.Context, version int64, note string, changes []config.Change,
	effective, desired *config.Resolved, seen time.Time) (jennah.ApprovalRequest, error) {
	approvers := cleanApprovers(g.S.LooseningApprovers)
	quorum, err := approvalQuorum(g.S.LooseningRequired, len(approvers))
	if err != nil {
		return jennah.ApprovalRequest{}, err
	}
	loosening := only(changes, config.Loosening)
	what, kept := "loosens protection", "the stricter config stays in force"
	if config.OnlyRecipients(changes) {
		what, kept = "changes who receives the guard's alerts", "the current recipients stay"
	}
	earliest, expires := seen.Add(g.S.LooseningDelay), seen.Add(g.S.LooseningWindow)

	var b strings.Builder
	intro, applies := "these changes", "the changes apply"
	if len(loosening) == 1 {
		intro, applies = "this change", "the change applies"
	}
	fmt.Fprintf(&b, "Config version %d for payer account %s (MSP %s) %s. If approved, it makes %s:\n\n",
		version, g.S.PayerAccountID, g.S.MSPID, what, intro)
	for _, c := range loosening {
		fmt.Fprintf(&b, "- %s\n", c.Sentence())
	}
	if lines := g.freezeLines(ctx, loosening, effective, desired); len(lines) > 0 {
		b.WriteString("\n" + strings.Join(lines, "\n") + "\n")
	}
	fmt.Fprintf(&b, "\nReason given:\n%s\n", reasonText(version, note))
	if g.S.LooseningDelay > 0 {
		fmt.Fprintf(&b, "\nIf you approve, %s %s after the guard first saw this version, at %s at the earliest.",
			applies, durationWords(g.S.LooseningDelay), utcText(earliest))
	} else {
		fmt.Fprintf(&b, "\nIf you approve, %s on the guard's next run.", applies)
	}
	fmt.Fprintf(&b, " If you reject it, or nobody answers by %s, %s.", utcText(expires), kept)

	return jennah.ApprovalRequest{
		RequestKey: fmt.Sprintf("guardrail:%s:%s:v%d", g.S.PayerAccountID, g.S.MSPID, version),
		Title:      approvalTitle(g.S.PayerAccountID, version, loosening),
		Summary:    b.String(),
		Payload: map[string]any{
			"payer_account_id":  g.S.PayerAccountID,
			"msp_id":            g.S.MSPID,
			"config_version":    float64(version),
			"loosening":         toAny(changeStrings(loosening)),
			"reason":            strings.TrimSpace(note),
			"earliest_apply_at": earliest.UTC().Format(time.RFC3339),
			"expires_at":        expires.UTC().Format(time.RFC3339),
		},
		Approvers: approvers,
		Quorum:    quorum,
		TTL:       g.S.LooseningWindow,
	}, nil
}

// cleanApprovers trims the stack's approver list, drops blanks, and drops
// repeats (ignoring case, keeping the first spelling), so the count used for the
// quorum is the count Jennah sees.
func cleanApprovers(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		k := strings.ToLower(a)
		if a == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, a)
	}
	return out
}

// approvalQuorum maps LooseningRequiredApprovals to one of the two rules Jennah
// supports. Any other count is a setup error: no approval is raised, and the
// stricter config stays in force until the stack is fixed.
func approvalQuorum(required, approvers int) (jennah.Quorum, error) {
	switch {
	case approvers == 0:
		return "", fmt.Errorf("setup error: LooseningApprovers lists no approvers, so no loosening can be approved. Update the stack")
	case required <= 1:
		return jennah.QuorumAnyOne, nil
	case required == approvers:
		return jennah.QuorumAll, nil
	case approvers == 1:
		return "", fmt.Errorf("setup error: LooseningRequiredApprovals is %d but LooseningApprovers lists 1 approver. Jennah needs 1. Update the stack", required)
	default:
		return "", fmt.Errorf("setup error: LooseningRequiredApprovals is %d but LooseningApprovers lists %d approvers. Jennah needs 1 (any one approver) or %d (all of them). Update the stack",
			required, approvers, approvers)
	}
}

func only(changes []config.Change, d config.Direction) []config.Change {
	var out []config.Change
	for _, c := range changes {
		if c.Direction == d {
			out = append(out, c)
		}
	}
	return out
}

func changeStrings(changes []config.Change) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.String())
	}
	return out
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
