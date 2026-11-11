// Package jennahtest is an in-memory Jennah for tests: just enough of the data
// and approval services, served over an in-process gRPC connection to the real
// SDK client.
package jennahtest

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	sdk "github.com/alphauslabs/jennah-sdk-go"
	approvalv1 "github.com/alphauslabs/jennah-sdk-go/jennah/approval/v1"
	authv1 "github.com/alphauslabs/jennah-sdk-go/jennah/auth/v1"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// Server is the fake.
type Server struct {
	datastorev1.UnimplementedDataServiceServer
	datastorev1.UnimplementedDatasetServiceServer
	datastorev1.UnimplementedSchemaServiceServer
	approvalv1.UnimplementedApprovalServiceServer
	authv1.UnimplementedAuthServiceServer

	// CallingKey is what WhoAmI reports as the calling API key; nil reports
	// none, as an older Jennah does.
	CallingKey *authv1.ApiKey

	datasets map[string]bool
	tables   map[string]map[string]*datastorev1.TableDeclaration
	// Declares counts table declarations that changed something.
	Declares int

	mu sync.Mutex
	// rows[dataset][table][primary key value] = row
	rows map[string]map[string]map[string]*datastorev1.Row
	// committed idempotency keys per dataset
	idem      map[string]map[string]*datastorev1.CommitDataResponse
	approvals map[string]*approvalv1.Approval
	byKey     map[string]string

	// FailCommits makes every commit fail with Unavailable.
	FailCommits bool
	// FailReads makes every query fail with Unavailable.
	FailReads bool
	// LoseCommitResponse commits, then reports a timeout, this many times.
	LoseCommitResponse int
	// Commits counts commit calls that reached the store.
	Commits int
	// CreatedApprovals counts approvals actually created (not replays).
	CreatedApprovals int
}

// Start runs the fake and returns a jennah.Client connected to it.
func Start(t testing.TB) (*Server, *jennah.Client) {
	t.Helper()
	s := &Server{
		rows:      map[string]map[string]map[string]*datastorev1.Row{},
		idem:      map[string]map[string]*datastorev1.CommitDataResponse{},
		approvals: map[string]*approvalv1.Approval{},
		byKey:     map[string]string{},
		datasets:  map[string]bool{},
		tables:    map[string]map[string]*datastorev1.TableDeclaration{},
	}
	lis := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	datastorev1.RegisterDataServiceServer(g, s)
	datastorev1.RegisterDatasetServiceServer(g, s)
	datastorev1.RegisterSchemaServiceServer(g, s)
	approvalv1.RegisterApprovalServiceServer(g, s)
	authv1.RegisterAuthServiceServer(g, s)
	go func() { _ = g.Serve(lis) }()
	t.Cleanup(g.Stop)

	c, err := jennah.Dial("passthrough:///bufnet", "jennah_sk_test", func(cfg *sdk.Config) {
		cfg.Insecure = true
		cfg.TransportCredentials = insecure.NewCredentials()
		cfg.Retry = sdk.RetryPolicy{MaxAttempts: 1}
		cfg.DialOptions = append(cfg.DialOptions, grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}))
	})
	if err != nil {
		t.Fatalf("dial fake jennah: %v", err)
	}
	return s, c
}

// keyOf is a row's primary key: the table's declared key columns, or for an
// undeclared table, "event_id" for events and "version" otherwise.
func (s *Server) keyOf(ds string, r *datastorev1.Row, table string) string {
	cols := []string{"version"}
	if table == "events" {
		cols = []string{"event_id"}
	}
	if t, ok := s.tables[ds][table]; ok && len(t.GetPrimaryKey()) > 0 {
		cols = t.GetPrimaryKey()
	}
	parts := make([]string, 0, len(cols))
	for _, col := range cols {
		v := r.GetColumns()[col]
		switch k := v.GetKind().(type) {
		case *datastorev1.Value_Int64Value:
			parts = append(parts, fmt.Sprintf("%020d", k.Int64Value))
		case *datastorev1.Value_StringValue:
			parts = append(parts, k.StringValue)
		default:
			parts = append(parts, "")
		}
	}
	return strings.Join(parts, "\x00")
}

// CommitData applies inserts and upserts, honoring idempotency keys and
// reporting a duplicate primary key as AlreadyExists.
func (s *Server) CommitData(_ context.Context, in *datastorev1.CommitDataRequest) (*datastorev1.CommitDataResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailCommits {
		return nil, status.Error(codes.Unavailable, "fake: unavailable")
	}
	ds := in.GetDatasetId()
	if k := in.GetIdempotencyKey(); k != "" {
		if r, ok := s.idem[ds][k]; ok {
			return r, nil
		}
	}
	if s.rows[ds] == nil {
		s.rows[ds] = map[string]map[string]*datastorev1.Row{}
	}
	for _, op := range in.GetOperations() {
		tbl := s.rows[ds][op.GetTable()]
		if tbl != nil {
			if _, exists := tbl[s.keyOf(ds, op.GetRow(), op.GetTable())]; exists && op.GetType() == datastorev1.OperationType_OPERATION_TYPE_INSERT {
				return nil, status.Error(codes.AlreadyExists, "fake: row exists")
			}
		}
	}
	for _, op := range in.GetOperations() {
		if s.rows[ds][op.GetTable()] == nil {
			s.rows[ds][op.GetTable()] = map[string]*datastorev1.Row{}
		}
		s.rows[ds][op.GetTable()][s.keyOf(ds, op.GetRow(), op.GetTable())] = proto.Clone(op.GetRow()).(*datastorev1.Row)
	}
	s.Commits++
	resp := &datastorev1.CommitDataResponse{}
	if k := in.GetIdempotencyKey(); k != "" {
		if s.idem[ds] == nil {
			s.idem[ds] = map[string]*datastorev1.CommitDataResponse{}
		}
		s.idem[ds][k] = resp
	}
	if s.LoseCommitResponse > 0 {
		s.LoseCommitResponse--
		return nil, status.Error(codes.DeadlineExceeded, "fake: response lost")
	}
	return resp, nil
}

// QueryData supports one equality predicate, one order-by and a limit.
func (s *Server) QueryData(_ context.Context, in *datastorev1.QueryDataRequest) (*datastorev1.QueryDataResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailReads {
		return nil, status.Error(codes.Unavailable, "fake: unavailable")
	}
	q := in.GetRelational()
	var rows []*datastorev1.Row
	for _, r := range s.rows[in.GetDatasetId()][q.GetTable()] {
		if matches(r, q.GetWhere()) {
			rows = append(rows, r)
		}
	}
	if ob := q.GetOrderBy(); len(ob) > 0 {
		col, desc := ob[0].GetColumn(), ob[0].GetDescending()
		sort.Slice(rows, func(i, j int) bool {
			a, b := sortKey(rows[i], col), sortKey(rows[j], col)
			if desc {
				return a > b
			}
			return a < b
		})
	}
	if l := int(q.GetLimit()); l > 0 && len(rows) > l {
		rows = rows[:l]
	}
	return &datastorev1.QueryDataResponse{Relational: &datastorev1.RelationalResult{Rows: rows}}, nil
}

func matches(r *datastorev1.Row, where []*datastorev1.Predicate) bool {
	for _, p := range where {
		if !proto.Equal(r.GetColumns()[p.GetColumn()], p.GetValue()) {
			return false
		}
	}
	return true
}

func sortKey(r *datastorev1.Row, col string) string {
	v := r.GetColumns()[col]
	switch k := v.GetKind().(type) {
	case *datastorev1.Value_Int64Value:
		return fmt.Sprintf("%020d", k.Int64Value)
	case *datastorev1.Value_TimestampValue:
		return k.TimestampValue.AsTime().Format("2006-01-02T15:04:05.000000000Z07:00")
	case *datastorev1.Value_StringValue:
		return k.StringValue
	}
	return ""
}

// Rows returns a table's rows.
func (s *Server) Rows(dataset, table string) []*datastorev1.Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*datastorev1.Row
	for _, r := range s.rows[dataset][table] {
		out = append(out, r)
	}
	return out
}

// CreateApproval is idempotent on request key. Like Jennah, it refuses an
// approval with no quorum rule (quorumFromProto in Jennah's approvalconv.go) and
// a required count that disagrees with the rule (approvals.ErrThresholdMismatch).
func (s *Server) CreateApproval(_ context.Context, in *approvalv1.CreateApprovalRequest) (*approvalv1.CreateApprovalResponse, error) {
	var required int32
	switch in.GetQuorum() {
	case approvalv1.Quorum_QUORUM_ANY_ONE:
		required = 1
	case approvalv1.Quorum_QUORUM_ALL:
		required = int32(len(in.GetApprovers()))
	case approvalv1.Quorum_QUORUM_UNSPECIFIED:
		return nil, status.Error(codes.InvalidArgument, "A quorum rule is required: ANY_ONE or ALL.")
	default:
		return nil, status.Errorf(codes.InvalidArgument, "Quorum rule %v is not one this platform accepts.", in.GetQuorum())
	}
	if r := in.GetRequiredApprovals(); r != 0 && r != required {
		return nil, status.Errorf(codes.InvalidArgument, "required_approvals disagrees with the quorum rule: %v with %d approvers needs %d, got %d",
			in.GetQuorum(), len(in.GetApprovers()), required, r)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byKey[in.GetRequestKey()]; ok {
		return &approvalv1.CreateApprovalResponse{Approval: s.approvals[id], AlreadyExisted: true}, nil
	}
	id := fmt.Sprintf("apr_%d", len(s.approvals)+1)
	a := &approvalv1.Approval{
		ApprovalId:        id,
		RequestKey:        in.GetRequestKey(),
		Title:             in.GetTitle(),
		Summary:           in.GetSummary(),
		Payload:           in.GetPayload(),
		Quorum:            in.GetQuorum(),
		RequiredApprovals: required,
		Status:            approvalv1.ApprovalStatus_APPROVAL_STATUS_PENDING,
	}
	s.approvals[id] = a
	s.byKey[in.GetRequestKey()] = id
	s.CreatedApprovals++
	return &approvalv1.CreateApprovalResponse{Approval: a}, nil
}

// GetApproval reads one.
func (s *Server) GetApproval(_ context.Context, in *approvalv1.GetApprovalRequest) (*approvalv1.GetApprovalResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[in.GetApprovalId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "fake: no approval")
	}
	return &approvalv1.GetApprovalResponse{Approval: a}, nil
}

// CancelApproval cancels a pending one.
func (s *Server) CancelApproval(_ context.Context, in *approvalv1.CancelApprovalRequest) (*approvalv1.CancelApprovalResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[in.GetApprovalId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "fake: no approval")
	}
	if a.Status != approvalv1.ApprovalStatus_APPROVAL_STATUS_PENDING {
		return nil, status.Error(codes.FailedPrecondition, "fake: already decided")
	}
	a.Status = approvalv1.ApprovalStatus_APPROVAL_STATUS_CANCELLED
	return &approvalv1.CancelApprovalResponse{Approval: a}, nil
}

// Decide sets an approval's status, as a human would.
func (s *Server) Decide(id string, st approvalv1.ApprovalStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approvals[id].Status = st
}

// Approval returns a raised approval by request key.
func (s *Server) Approval(requestKey string) *approvalv1.Approval {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.approvals[s.byKey[requestKey]]
}

// CreateDataset creates a dataset; an existing id is AlreadyExists.
func (s *Server) CreateDataset(_ context.Context, in *datastorev1.CreateDatasetRequest) (*datastorev1.CreateDatasetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.datasets[in.GetDatasetId()] {
		return nil, status.Error(codes.AlreadyExists, "fake: dataset exists")
	}
	s.datasets[in.GetDatasetId()] = true
	return &datastorev1.CreateDatasetResponse{Dataset: &datastorev1.Dataset{DatasetId: in.GetDatasetId()}}, nil
}

// DeclareTables records declarations; redeclaring the same table changes
// nothing.
func (s *Server) DeclareTables(_ context.Context, in *datastorev1.DeclareTablesRequest) (*datastorev1.DeclareTablesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.datasets[in.GetDatasetId()] {
		return nil, status.Error(codes.NotFound, "fake: no dataset")
	}
	if s.tables[in.GetDatasetId()] == nil {
		s.tables[in.GetDatasetId()] = map[string]*datastorev1.TableDeclaration{}
	}
	resp := &datastorev1.DeclareTablesResponse{}
	for _, t := range in.GetTables() {
		old, ok := s.tables[in.GetDatasetId()][t.GetName()]
		t = proto.Clone(t).(*datastorev1.TableDeclaration)
		// Retention, as Jennah treats it: unset keeps what the table has,
		// days 0 removes it, anything else sets it.
		var had *datastorev1.RowRetention
		if ok {
			had = old.GetRetention()
		}
		switch r := t.GetRetention(); {
		case r == nil:
			t.Retention = had
		case r.GetDays() == 0:
			t.Retention = nil
		}
		ts := &datastorev1.TableStatus{Name: t.GetName(), Status: datastorev1.SchemaStatus_SCHEMA_STATUS_READY}
		if !proto.Equal(had, t.GetRetention()) {
			ts.RetentionChange = &datastorev1.RetentionChange{From: had, To: t.GetRetention(),
				RemovesRows: t.GetRetention() != nil && (had == nil || t.GetRetention().GetDays() < had.GetDays())}
		}
		if !ok || !proto.Equal(old, t) {
			s.Declares++
		}
		s.tables[in.GetDatasetId()][t.GetName()] = t
		resp.Tables = append(resp.Tables, ts)
	}
	return resp, nil
}

// GetSchema lists declared tables, all READY.
func (s *Server) GetSchema(_ context.Context, in *datastorev1.GetSchemaRequest) (*datastorev1.GetSchemaResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.datasets[in.GetDatasetId()] {
		return nil, status.Error(codes.NotFound, "fake: no dataset")
	}
	resp := &datastorev1.GetSchemaResponse{}
	for _, t := range s.tables[in.GetDatasetId()] {
		var cols []*datastorev1.ColumnSchema
		for _, c := range t.GetColumns() {
			cols = append(cols, &datastorev1.ColumnSchema{Name: c.GetName(), Type: c.GetType(), Nullable: c.GetNullable()})
		}
		resp.Tables = append(resp.Tables, &datastorev1.TableSchema{Name: t.GetName(), Columns: cols, PrimaryKey: t.GetPrimaryKey(), Status: datastorev1.SchemaStatus_SCHEMA_STATUS_READY, Retention: t.GetRetention()})
	}
	return resp, nil
}

// Datasets lists created datasets.
func (s *Server) Datasets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for d := range s.datasets {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// WhoAmI reports the configured calling key, failing like a read when
// FailReads is set.
func (s *Server) WhoAmI(context.Context, *authv1.WhoAmIRequest) (*authv1.WhoAmIResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailReads {
		return nil, status.Error(codes.Unavailable, "fake: reads failing")
	}
	return &authv1.WhoAmIResponse{ApiKey: s.CallingKey}, nil
}
