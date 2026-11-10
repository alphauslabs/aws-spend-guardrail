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
}

// Pending is a loosening version and its approval.
type Pending struct {
	Version    int64                 `json:"version"`
	ApprovalID string                `json:"approval_id"`
	FirstSeen  time.Time             `json:"first_seen"`
	Desired    *config.Resolved      `json:"desired"`
	Closed     jennah.ApprovalStatus `json:"closed,omitempty"` // set once rejected, expired or cancelled
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
}

// BudgetSpec is one budget the guard owns, as it should exist in AWS.
type BudgetSpec struct {
	Name         string
	Target       config.Target
	Accounts     []string // linked accounts the budget's cost filter covers
	LimitUSD     float64
	Period       string // config period
	ThresholdPct int
	PolicyID     string // freeze policy the action attaches
	Enforce      bool   // false: dry-run (manual-approval action)
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

// Org reads the organization: OU membership and freeze policy attachments.
type Org interface {
	AccountsInOU(ctx context.Context, ouID string) ([]string, error)
	PolicyTargets(ctx context.Context, policyID string) ([]config.Target, error)
}

// Settings are the guard's stack parameters.
type Settings struct {
	PayerAccountID     string
	MSPID              string
	GuardVersion       string
	PolicyA            string
	PolicyB            string
	LooseningApprovers []string
	LooseningRequired  int
	LooseningWindow    time.Duration
	LooseningDelay     time.Duration
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
	Now     func() time.Time
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
