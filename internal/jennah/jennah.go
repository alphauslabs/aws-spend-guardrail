// Package jennah is the guard's and the CLI's narrow view of Jennah: config
// versions, inbox events and loosening approvals, over jennah-sdk-go.
package jennah

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	sdk "github.com/alphauslabs/jennah-sdk-go"
	approvalv1 "github.com/alphauslabs/jennah-sdk-go/jennah/approval/v1"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// ErrVersionExists means a config version with that number is already written.
var ErrVersionExists = errors.New("config version already exists")

// Version is one stored config version.
type Version struct {
	Number      int64
	Document    json.RawMessage
	PublishedAt time.Time
	Note        string
}

// Event is one inbox record.
type Event struct {
	ID            string          `json:"event_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Kind          string          `json:"kind"`
	Scope         string          `json:"scope,omitempty"`
	ConfigVersion int64           `json:"config_version,omitempty"`
	GuardVersion  string          `json:"guard_version"`
	Detail        json.RawMessage `json:"detail,omitempty"`
}

// ApprovalStatus is the state of a loosening approval.
type ApprovalStatus string

const (
	ApprovalPending   ApprovalStatus = "pending"
	ApprovalApproved  ApprovalStatus = "approved"
	ApprovalRejected  ApprovalStatus = "rejected"
	ApprovalExpired   ApprovalStatus = "expired"
	ApprovalCancelled ApprovalStatus = "cancelled"
)

// ApprovalRequest is what the guard asks humans to approve.
type ApprovalRequest struct {
	RequestKey string
	Title      string
	Summary    string
	Payload    map[string]any
	Approvers  []string
	Required   int
	TTL        time.Duration
}

// Approval is a raised approval.
type Approval struct {
	ID     string
	Status ApprovalStatus
}

// Client wraps an SDK client.
type Client struct {
	c *sdk.Client
}

// New wraps an SDK client.
func New(c *sdk.Client) *Client { return &Client{c: c} }

// Dial connects with an API key.
func Dial(endpoint, apiKey string, opts ...func(*sdk.Config)) (*Client, error) {
	cfg := sdk.Config{Endpoint: endpoint, APIKey: apiKey}
	for _, o := range opts {
		o(&cfg)
	}
	c, err := sdk.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return New(c), nil
}

// SDK returns the underlying SDK client, for setup work the guard never does.
func (c *Client) SDK() *sdk.Client { return c.c }

// LatestVersion returns the highest config version, or nil when none exists.
func (c *Client) LatestVersion(ctx context.Context, datasetID string) (*Version, error) {
	resp, err := c.c.Dataset(datasetID).Data.Query(ctx, &datastorev1.QueryDataRequest{
		Relational: &datastorev1.RelationalQuery{
			Table:   schema.ConfigTable,
			OrderBy: []*datastorev1.OrderBy{{Column: schema.ColVersion, Descending: true}},
			Limit:   1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read latest config version: %w", err)
	}
	rows := resp.GetRelational().GetRows()
	if len(rows) == 0 {
		return nil, nil
	}
	return versionFromRow(rows[0])
}

// GetVersion returns one config version, or nil when it doesn't exist.
func (c *Client) GetVersion(ctx context.Context, datasetID string, number int64) (*Version, error) {
	resp, err := c.c.Dataset(datasetID).Data.Query(ctx, &datastorev1.QueryDataRequest{
		Relational: &datastorev1.RelationalQuery{
			Table: schema.ConfigTable,
			Where: []*datastorev1.Predicate{{
				Column:   schema.ColVersion,
				Operator: datastorev1.Predicate_OPERATOR_EQUALS,
				Value:    int64Val(number),
			}},
			Limit: 1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read config version %d: %w", number, err)
	}
	rows := resp.GetRelational().GetRows()
	if len(rows) == 0 {
		return nil, nil
	}
	return versionFromRow(rows[0])
}

// InsertVersion writes a new config version. It returns ErrVersionExists when
// another publisher wrote that version first. The idempotency key makes a
// retried insert of the same document safe.
func (c *Client) InsertVersion(ctx context.Context, datasetID string, v Version, idempotencyKey string) error {
	row := &datastorev1.Row{Columns: map[string]*datastorev1.Value{
		schema.ColVersion:       int64Val(v.Number),
		schema.ColDocument:      {Kind: &datastorev1.Value_JsonValue{JsonValue: string(v.Document)}},
		schema.ColPublishedAt:   {Kind: &datastorev1.Value_TimestampValue{TimestampValue: timestamppb.New(v.PublishedAt)}},
		schema.ColLayoutVersion: int64Val(schema.LayoutVersion),
	}}
	if v.Note != "" {
		row.Columns[schema.ColNote] = strVal(v.Note)
	}
	_, err := c.c.Dataset(datasetID).Data.Commit(ctx, &datastorev1.CommitDataRequest{
		Operations: []*datastorev1.RowOperation{{
			Table: schema.ConfigTable,
			Type:  datastorev1.OperationType_OPERATION_TYPE_INSERT,
			Row:   row,
		}},
		IdempotencyKey: idempotencyKey,
	})
	if sdk.IsAlreadyCommitted(err) {
		return ErrVersionExists
	}
	if err != nil {
		return fmt.Errorf("insert config version %d: %w", v.Number, err)
	}
	return nil
}

// CommitEvent writes one event, using its id as the idempotency key so a
// retried write never produces a second event. An event already present counts
// as written.
func (c *Client) CommitEvent(ctx context.Context, datasetID string, e Event) error {
	row := &datastorev1.Row{Columns: map[string]*datastorev1.Value{
		schema.ColEventID:       strVal(e.ID),
		schema.ColOccurredAt:    {Kind: &datastorev1.Value_TimestampValue{TimestampValue: timestamppb.New(e.OccurredAt)}},
		schema.ColKind:          strVal(e.Kind),
		schema.ColGuardVersion:  strVal(e.GuardVersion),
		schema.ColLayoutVersion: int64Val(schema.LayoutVersion),
	}}
	if e.Scope != "" {
		row.Columns[schema.ColScope] = strVal(e.Scope)
	}
	if e.ConfigVersion != 0 {
		row.Columns[schema.ColConfigVersion] = int64Val(e.ConfigVersion)
	}
	if len(e.Detail) > 0 {
		row.Columns[schema.ColDetail] = &datastorev1.Value{Kind: &datastorev1.Value_JsonValue{JsonValue: string(e.Detail)}}
	}
	_, err := c.c.Dataset(datasetID).Data.Commit(ctx, &datastorev1.CommitDataRequest{
		Operations: []*datastorev1.RowOperation{{
			Table: schema.EventsTable,
			Type:  datastorev1.OperationType_OPERATION_TYPE_INSERT,
			Row:   row,
		}},
		IdempotencyKey: "event:" + e.ID,
	})
	if err != nil && !sdk.IsAlreadyCommitted(err) {
		return fmt.Errorf("write event %s: %w", e.ID, err)
	}
	return nil
}

// LatestEvent returns the newest event of a kind, or nil when none exists.
func (c *Client) LatestEvent(ctx context.Context, datasetID, kind string) (*Event, error) {
	resp, err := c.c.Dataset(datasetID).Data.Query(ctx, &datastorev1.QueryDataRequest{
		Relational: &datastorev1.RelationalQuery{
			Table:   schema.EventsTable,
			Where:   []*datastorev1.Predicate{{Column: schema.ColKind, Operator: datastorev1.Predicate_OPERATOR_EQUALS, Value: strVal(kind)}},
			OrderBy: []*datastorev1.OrderBy{{Column: schema.ColOccurredAt, Descending: true}},
			Limit:   1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read latest %s event: %w", kind, err)
	}
	rows := resp.GetRelational().GetRows()
	if len(rows) == 0 {
		return nil, nil
	}
	return eventFromRow(rows[0]), nil
}

// RaiseApproval creates an approval, or returns the existing one for the same
// request key.
func (c *Client) RaiseApproval(ctx context.Context, r ApprovalRequest) (*Approval, error) {
	payload, err := structpb.NewStruct(r.Payload)
	if err != nil {
		return nil, fmt.Errorf("approval payload: %w", err)
	}
	var approvers []*approvalv1.ApproverSpec
	for _, a := range r.Approvers {
		approvers = append(approvers, &approvalv1.ApproverSpec{Email: a})
	}
	resp, err := c.c.Approvals.Create(ctx, &approvalv1.CreateApprovalRequest{
		RequestKey:        r.RequestKey,
		Title:             r.Title,
		Summary:           r.Summary,
		Payload:           payload,
		Approvers:         approvers,
		RequiredApprovals: int32(r.Required),
		TtlSeconds:        int32(r.TTL / time.Second),
		OnExpire:          approvalv1.OnExpire_ON_EXPIRE_EXPIRE,
	})
	if err != nil {
		return nil, fmt.Errorf("raise approval: %w", err)
	}
	return toApproval(resp.GetApproval()), nil
}

// GetApproval reads an approval's status.
func (c *Client) GetApproval(ctx context.Context, id string) (*Approval, error) {
	resp, err := c.c.Approvals.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read approval %s: %w", id, err)
	}
	return toApproval(resp.GetApproval()), nil
}

// CancelApproval cancels a pending approval. Cancelling one already decided is
// not an error.
func (c *Client) CancelApproval(ctx context.Context, id string) error {
	_, err := c.c.Approvals.Cancel(ctx, id)
	if err != nil && sdk.Code(err) != codes.FailedPrecondition {
		return fmt.Errorf("cancel approval %s: %w", id, err)
	}
	return nil
}

func toApproval(a *approvalv1.Approval) *Approval {
	out := &Approval{ID: a.GetApprovalId()}
	switch a.GetStatus() {
	case approvalv1.ApprovalStatus_APPROVAL_STATUS_APPROVED:
		out.Status = ApprovalApproved
	case approvalv1.ApprovalStatus_APPROVAL_STATUS_REJECTED:
		out.Status = ApprovalRejected
	case approvalv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED:
		out.Status = ApprovalExpired
	case approvalv1.ApprovalStatus_APPROVAL_STATUS_CANCELLED:
		out.Status = ApprovalCancelled
	default:
		out.Status = ApprovalPending
	}
	return out
}

func strVal(s string) *datastorev1.Value {
	return &datastorev1.Value{Kind: &datastorev1.Value_StringValue{StringValue: s}}
}

func int64Val(n int64) *datastorev1.Value {
	return &datastorev1.Value{Kind: &datastorev1.Value_Int64Value{Int64Value: n}}
}

func versionFromRow(r *datastorev1.Row) (*Version, error) {
	cols := r.GetColumns()
	v := &Version{
		Number:   cols[schema.ColVersion].GetInt64Value(),
		Document: json.RawMessage(cols[schema.ColDocument].GetJsonValue()),
		Note:     cols[schema.ColNote].GetStringValue(),
	}
	if ts := cols[schema.ColPublishedAt].GetTimestampValue(); ts != nil {
		v.PublishedAt = ts.AsTime()
	}
	if v.Number == 0 {
		return nil, errors.New("config row has no version")
	}
	return v, nil
}

func eventFromRow(r *datastorev1.Row) *Event {
	cols := r.GetColumns()
	e := &Event{
		ID:            cols[schema.ColEventID].GetStringValue(),
		Kind:          cols[schema.ColKind].GetStringValue(),
		Scope:         cols[schema.ColScope].GetStringValue(),
		ConfigVersion: cols[schema.ColConfigVersion].GetInt64Value(),
		GuardVersion:  cols[schema.ColGuardVersion].GetStringValue(),
	}
	if ts := cols[schema.ColOccurredAt].GetTimestampValue(); ts != nil {
		e.OccurredAt = ts.AsTime()
	}
	if d := cols[schema.ColDetail].GetJsonValue(); d != "" {
		e.Detail = json.RawMessage(d)
	}
	return e
}

// VersionIdempotencyKey derives a publish's idempotency key from the version
// number and document, so a retried publish of the same document writes one
// version.
func VersionIdempotencyKey(number int64, docSHA256 string) string {
	return "config:" + strconv.FormatInt(number, 10) + ":" + docSHA256
}
