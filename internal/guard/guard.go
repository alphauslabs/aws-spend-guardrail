// Package guard is the logic of the guard function: pull config from Jennah,
// apply it so that tightening is immediate and loosening waits for approval,
// keep AWS Budgets matching the applied config, and record what happens.
//
// Everything AWS- or Jennah-specific sits behind the interfaces in this file,
// so the logic is tested without either.
package guard

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
)

// ConfigSource reads config versions and manages loosening approvals. It is
// the guard's reader credential.
type ConfigSource interface {
	LatestVersion(ctx context.Context, datasetID string) (*jennah.Version, error)
	RaiseApproval(ctx context.Context, r jennah.ApprovalRequest) (*jennah.Approval, error)
	GetApproval(ctx context.Context, id string) (*jennah.Approval, error)
	CancelApproval(ctx context.Context, id string) error
}

// EventSink accepts events for delivery to Jennah. The production sink is the
// SQS outbox; delivery happens later and is retried there.
type EventSink interface {
	Emit(ctx context.Context, e jennah.Event) error
}

// Alerter publishes to the stack's local alert topic. It never depends on
// Jennah.
type Alerter interface {
	Alert(ctx context.Context, subject, message string) error
}

// State is the guard's durable config state.
type State struct {
	// AppliedVersion is the last version applied in full. 0 means none.
	AppliedVersion int64 `json:"applied_version"`
	// Effective is the config in force: the applied version, plus the
	// tightening parts of a pending loosening version.
	Effective *config.Resolved `json:"effective,omitempty"`
	// EffectiveVersion is the newest version whose tightening parts are in
	// Effective.
	EffectiveVersion int64 `json:"effective_version"`
	// RejectedVersion is the newest version found invalid, so it is reported
	// once.
	RejectedVersion int64 `json:"rejected_version,omitempty"`
	// Pending is the loosening version awaiting approval, if any.
	Pending *Pending `json:"pending,omitempty"`
	// FailingSince is when config refresh from Jennah started failing; zero
	// while it works. LastFailureAlert is when the last alert about it went
	// out. Together they keep an outage to one alert, a reminder every few
	// hours, and one recovery alert.
	FailingSince     time.Time `json:"failing_since,omitempty"`
	LastFailureAlert time.Time `json:"last_failure_alert,omitempty"`
	// OwnSubscriptions are the alert topic subscriptions the guard asked
	// for, by lowercased address. Only these are ever unsubscribed.
	OwnSubscriptions map[string]OwnSubscription `json:"own_subscriptions,omitempty"`
	// KeyWarnings are the expiry warnings sent per guard key, by key name.
	KeyWarnings map[string]KeyWarning `json:"key_warnings,omitempty"`
}

// Pending is a loosening version and its approval.
type Pending struct {
	Version    int64                 `json:"version"`
	ApprovalID string                `json:"approval_id"`
	FirstSeen  time.Time             `json:"first_seen"`
	Desired    *config.Resolved      `json:"desired"`
	Closed     jennah.ApprovalStatus `json:"closed,omitempty"` // set once rejected, expired or cancelled
	// Note is the version's note, shown to approvers as the reason given.
	Note string `json:"note,omitempty"`
}

// Frozen records that a scope was frozen by the guard's budget action.
type Frozen struct {
	Target      config.Target `json:"target"`
	Since       time.Time     `json:"since"`
	PeriodStart time.Time     `json:"period_start"`
	PolicyID    string        `json:"policy_id"`
	// ThresholdUSD is the threshold in force when the freeze fired. Only a
	// higher threshold can lift the freeze as a raise; a new period resets
	// actual spend, which must not count as a raise.
	ThresholdUSD float64 `json:"threshold_usd"`
}

// StateStore persists State and frozen records in the payer account.
type StateStore interface {
	Load(ctx context.Context) (*State, error)
	Save(ctx context.Context, s *State) error
	MarkFrozen(ctx context.Context, f Frozen) error
	ClearFrozen(ctx context.Context, t config.Target) error
	ListFrozen(ctx context.Context) ([]Frozen, error)
	// Reported returns the accounts already reported for a kind
	// (unprotected_account, stale_scope), with when each was first reported.
	Reported(ctx context.Context, kind string) (map[string]time.Time, error)
	MarkReported(ctx context.Context, kind, account string, at time.Time) error
	ClearReported(ctx context.Context, kind, account string) error
}

// BudgetSpec is one budget the guard owns, as it should exist in AWS.
type BudgetSpec struct {
	Name         string
	Target       config.Target
	Accounts     []string // linked accounts the budget's cost filter covers
	LimitUSD     float64
	Period       string // config period
	ThresholdPct int
	PolicyID     string // freeze policy the action attaches; empty for dry-run
	Enforce      bool   // false: dry-run, a plain budget with no action
}

// BudgetStatus is a guard-owned budget as it exists in AWS.
type BudgetStatus struct {
	Spec        BudgetSpec
	ActualUSD   float64
	PeriodStart time.Time
}

// Budgets manages guard-owned budgets, their freeze actions and lifts.
type Budgets interface {
	// ListOwned returns every budget the guard owns. Budgets it doesn't own are
	// never returned.
	ListOwned(ctx context.Context) ([]BudgetStatus, error)
	// Put creates or updates a budget, its action and its notification.
	Put(ctx context.Context, b BudgetSpec) error
	// Delete removes an owned budget and its action.
	Delete(ctx context.Context, name string) error
	// Release lifts a freeze for a budget's target and reports how: by
	// reversing the Budgets action, or by detaching the policy directly.
	Release(ctx context.Context, b BudgetSpec) (how string, err error)
}

// Account is one member account of the organization.
type Account struct {
	ID    string
	Name  string
	State string // ACTIVE, SUSPENDED, PENDING_CLOSURE, ...
}

// Org reads the organization: accounts, OU membership and freeze policy
// attachments.
type Org interface {
	AccountsInOU(ctx context.Context, ouID string) ([]string, error)
	PolicyTargets(ctx context.Context, policyID string) ([]config.Target, error)
	ListAccounts(ctx context.Context) ([]Account, error)
	// ParentOf names where an account sits: "root" or an OU id.
	ParentOf(ctx context.Context, account string) (string, error)
}

// Settings are the guard's stack parameters.
type Settings struct {
	PayerAccountID string
	MSPID          string
	GuardVersion   string
	// CodeSHA256 is the deployed package's hash as AWS Lambda reports it
	// (base64 SHA-256), or "unknown". Set once at cold start.
	CodeSHA256         string
	PolicyA            string
	PolicyB            string
	LooseningApprovers []string
	LooseningRequired  int
	LooseningWindow    time.Duration
	LooseningDelay     time.Duration
	// AlertEmail is the stack's own alert recipient. Config can never
	// unsubscribe it.
	AlertEmail string
}

// Guard ties the pieces together.
type Guard struct {
	S       Settings
	Config  ConfigSource
	Events  EventSink
	Alerts  Alerter
	State   StateStore
	Budgets Budgets
	Org     Org
	// Subs manages the alert topic's email subscriptions. Nil skips
	// recipient sync.
	Subs AlertSubscriptions
	// Keys are the guard's Jennah keys, checked daily for expiry. Empty skips
	// the check.
	Keys []GuardKey
	// Fast is the trail-reading fast path. Nil disables it.
	Fast *FastPath
	// Prices reads the enterprise's shared prices. Nil: release prices only.
	Prices PriceSource
	Now    func() time.Time
}

func (g *Guard) now() time.Time {
	if g.Now != nil {
		return g.Now().UTC()
	}
	return time.Now().UTC()
}

// emit records an event. An emit failure is alerted, never fatal: the freeze
// path doesn't depend on it.
func (g *Guard) emit(ctx context.Context, kind, id, scope string, version int64, detail any) {
	var raw json.RawMessage
	if detail != nil {
		raw, _ = json.Marshal(detail)
	}
	e := jennah.Event{
		ID:            id,
		OccurredAt:    g.now(),
		Kind:          kind,
		Scope:         scope,
		ConfigVersion: version,
		GuardVersion:  g.S.GuardVersion,
		Detail:        raw,
	}
	if err := g.Events.Emit(ctx, e); err != nil {
		g.alert(ctx, "guardrail: event not queued", "event "+id+" could not be queued: "+err.Error())
	}
}

func (g *Guard) alert(ctx context.Context, subject, msg string) {
	_ = g.Alerts.Alert(ctx, subject, msg)
}

func (g *Guard) policyFor(style string) string {
	if style == config.StyleA {
		return g.S.PolicyA
	}
	return g.S.PolicyB
}

// SetCodeHash records the deployed package's hash read at cold start. When it
// can't be read, the guard reports "unknown" and alerts locally, so a
// reviewer knows to read the hash from Lambda directly.
func (g *Guard) SetCodeHash(ctx context.Context, hash string, err error) {
	if err != nil || hash == "" {
		g.S.CodeSHA256 = "unknown"
		msg := "the guard could not read its own code hash, so events report it as unknown"
		if err != nil {
			msg += ": " + err.Error()
		}
		g.alert(ctx, "guardrail: code hash unknown", msg)
		return
	}
	g.S.CodeSHA256 = hash
}

// selfReport is what the guard says about itself in heartbeat and
// config_applied events, so a reviewer can compare the running function with
// a release from the record in Jennah.
func (g *Guard) selfReport(detail map[string]any) map[string]any {
	if detail == nil {
		detail = map[string]any{}
	}
	detail["guard_version"] = g.S.GuardVersion
	code := g.S.CodeSHA256
	if code == "" {
		code = "unknown"
	}
	detail["code_sha256"] = code
	return detail
}
