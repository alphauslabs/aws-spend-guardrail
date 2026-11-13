package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

const payer = "123456789012"
const msp = "msp_a"

type events struct {
	mu   sync.Mutex
	list []jennah.Event
}

func (e *events) Emit(_ context.Context, ev jennah.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, x := range e.list {
		if x.ID == ev.ID {
			return nil // the outbox dedupes by id downstream; mirror that here
		}
	}
	e.list = append(e.list, ev)
	return nil
}

func (e *events) kinds() []string {
	var out []string
	for _, x := range e.list {
		out = append(out, x.Kind)
	}
	return out
}

func (e *events) count(kind string) int {
	n := 0
	for _, x := range e.list {
		if x.Kind == kind {
			n++
		}
	}
	return n
}

type alerts struct {
	t    *testing.T
	list []string
}

// Alert records an alert and fails the test when its body breaks the alert
// wording rules (fix-alert-wording).
func (a *alerts) Alert(_ context.Context, subject, msg string) error {
	if a.t != nil {
		if err := alerttext.Check(msg); err != nil {
			a.t.Errorf("alert %q: %v\n%s", subject, err, msg)
		}
	}
	a.list = append(a.list, subject+": "+msg)
	return nil
}

func (a *alerts) has(substr string) bool {
	for _, x := range a.list {
		if strings.Contains(x, substr) {
			return true
		}
	}
	return false
}

// memState round-trips through JSON, like the real store would.
type memState struct {
	raw      []byte
	frozen   map[config.Target]Frozen
	reported map[string]map[string]time.Time
}

func newMemState() *memState {
	return &memState{frozen: map[config.Target]Frozen{}, reported: map[string]map[string]time.Time{}}
}

func (m *memState) Reported(_ context.Context, kind string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for k, v := range m.reported[kind] {
		out[k] = v
	}
	return out, nil
}

func (m *memState) MarkReported(_ context.Context, kind, account string, at time.Time) error {
	if m.reported[kind] == nil {
		m.reported[kind] = map[string]time.Time{}
	}
	m.reported[kind][account] = at
	return nil
}

func (m *memState) ClearReported(_ context.Context, kind, account string) error {
	delete(m.reported[kind], account)
	return nil
}

func (m *memState) Load(context.Context) (*State, error) {
	st := &State{}
	if m.raw != nil {
		if err := json.Unmarshal(m.raw, st); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func (m *memState) Save(_ context.Context, s *State) error {
	b, err := json.Marshal(s)
	m.raw = b
	return err
}

func (m *memState) MarkFrozen(_ context.Context, f Frozen) error {
	m.frozen[f.Target] = f
	return nil
}

func (m *memState) ClearFrozen(_ context.Context, t config.Target) error {
	delete(m.frozen, t)
	return nil
}

func (m *memState) ListFrozen(context.Context) ([]Frozen, error) {
	var out []Frozen
	for _, f := range m.frozen {
		out = append(out, f)
	}
	return out, nil
}

// fakeBudgets holds every budget in the account, owned or not.
type fakeBudgets struct {
	all      map[string]BudgetStatus
	puts     int
	deletes  []string
	releases []string
	freezes  []string
	// org, when set, is where Freeze, Release, and AWS's reset attach and
	// detach policies.
	org         *fakeOrg
	resetting   map[string]int // reads left before a reset reads as done
	resetReads  int            // status reads a reset takes
	statusReads int
	// actionCap, when set, is how many budgets can have an action, like
	// AWS's per-account limit: a new action past it is refused, and the
	// budget is written without one (add-enforcing-scope-limit).
	actionCap int
}

func newFakeBudgets() *fakeBudgets {
	return &fakeBudgets{all: map[string]BudgetStatus{}, resetting: map[string]int{}, resetReads: 1}
}

func (f *fakeBudgets) ListOwned(context.Context) ([]BudgetStatus, error) {
	var out []BudgetStatus
	for name, b := range f.all {
		if strings.HasPrefix(name, BudgetPrefix) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeBudgets) Put(_ context.Context, b BudgetSpec) error {
	f.puts++
	cur := f.all[b.Name]
	if b.Enforce && !cur.Spec.Enforce && f.actionCap > 0 && f.actions() >= f.actionCap {
		cur.Spec = b
		cur.Spec.Enforce, cur.Spec.PolicyID = false, ""
		f.all[b.Name] = cur
		return fmt.Errorf("create budget action: %w: ServiceQuotaExceededException: limit reached", ErrBudgetActionLimit)
	}
	if cur.ActionExecuted {
		// AWS resets an executed action when its budget is updated: the
		// policy is detached at once, and the action reads as resetting for a
		// few status reads before standby (fix-action-reset-unfreeze).
		cur.ActionExecuted = false
		if f.org != nil {
			f.org.detach(cur.Spec.PolicyID, b.Target)
		}
		f.resetting[b.Name] = f.resetReads
	}
	cur.Spec = b
	f.all[b.Name] = cur
	return nil
}

// actions counts budgets with an action.
func (f *fakeBudgets) actions() int {
	n := 0
	for _, b := range f.all {
		if b.Spec.Enforce {
			n++
		}
	}
	return n
}

func (f *fakeBudgets) ActionStatus(_ context.Context, name string) (string, error) {
	f.statusReads++
	b, ok := f.all[name]
	if !ok || !b.Spec.Enforce {
		return "", nil
	}
	if n, ok := f.resetting[name]; ok {
		if n > 0 {
			f.resetting[name] = n - 1
			return "RESET_IN_PROGRESS", nil
		}
		delete(f.resetting, name)
	}
	if b.ActionExecuted {
		return "EXECUTION_SUCCESS", nil
	}
	return "STANDBY", nil
}

// fire is the Budgets action executing: the policy is attached and the
// action reads as executed.
func (f *fakeBudgets) fire(name string) {
	b := f.all[name]
	b.ActionExecuted = true
	f.all[name] = b
	if f.org != nil {
		f.org.attach(b.Spec.PolicyID, b.Spec.Target)
	}
}

func (f *fakeBudgets) Delete(_ context.Context, name string) error {
	if !strings.HasPrefix(name, BudgetPrefix) {
		panic("guard tried to delete a budget it doesn't own: " + name)
	}
	f.deletes = append(f.deletes, name)
	delete(f.all, name)
	return nil
}

func (f *fakeBudgets) Release(_ context.Context, b BudgetSpec) (string, error) {
	f.releases = append(f.releases, b.Name)
	if f.org != nil {
		if !f.org.isAttached(b.PolicyID, b.Target) {
			return "already_detached", nil
		}
		f.org.detach(b.PolicyID, b.Target)
	}
	cur := f.all[b.Name]
	cur.ActionExecuted = false
	f.all[b.Name] = cur
	return "reverse_budget_action", nil
}

func (f *fakeBudgets) Freeze(_ context.Context, b BudgetSpec) error {
	f.freezes = append(f.freezes, b.Name)
	if f.org != nil {
		f.org.attach(b.PolicyID, b.Target)
	}
	return nil
}

// fakeSpend is Cost Explorer: cost per account, summed over a scope's
// accounts. It ignores the period, as tests set the period's figure.
type fakeSpend struct {
	byAccount map[string]float64
	err       error
	calls     int
}

func (f *fakeSpend) Actual(_ context.Context, accounts []string, _, _ time.Time) (float64, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	var total float64
	for _, a := range accounts {
		total += f.byAccount[a]
	}
	return total, nil
}

func (f *fakeBudgets) setActual(name string, usd float64) {
	b := f.all[name]
	b.ActualUSD = usd
	f.all[name] = b
}

type fakeOrg struct {
	ous      map[string][]string
	attached map[string][]config.Target
	accounts []Account
}

func (o *fakeOrg) attach(policy string, t config.Target) {
	if !o.isAttached(policy, t) {
		o.attached[policy] = append(o.attached[policy], t)
	}
}

func (o *fakeOrg) detach(policy string, t config.Target) {
	var keep []config.Target
	for _, x := range o.attached[policy] {
		if x != t {
			keep = append(keep, x)
		}
	}
	o.attached[policy] = keep
}

func (o *fakeOrg) isAttached(policy string, t config.Target) bool {
	for _, x := range o.attached[policy] {
		if x == t {
			return true
		}
	}
	return false
}

func (o *fakeOrg) ListAccounts(context.Context) ([]Account, error) {
	return append([]Account(nil), o.accounts...), nil
}

func (o *fakeOrg) ParentOf(_ context.Context, account string) (string, error) {
	for ou, members := range o.ous {
		for _, m := range members {
			if m == account {
				return ou, nil
			}
		}
	}
	return "root", nil
}

func (o *fakeOrg) AccountsInOU(_ context.Context, id string) ([]string, error) {
	return append([]string(nil), o.ous[id]...), nil
}

func (o *fakeOrg) PolicyTargets(_ context.Context, p string) ([]config.Target, error) {
	return o.attached[p], nil
}

// fakeSubs is the alert topic's email subscriptions. A subscribe for an
// address that already has a subscription returns it, as SNS does.
type fakeSubs struct {
	byAddr       map[string]Subscription
	subscribes   []string
	unsubscribes []string
	nextARN      int
}

func newFakeSubs() *fakeSubs { return &fakeSubs{byAddr: map[string]Subscription{}} }

func (f *fakeSubs) List(context.Context) ([]Subscription, error) {
	var out []Subscription
	for _, s := range f.byAddr {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSubs) Subscribe(_ context.Context, addr string) error {
	f.subscribes = append(f.subscribes, addr)
	if _, ok := f.byAddr[addr]; !ok {
		f.byAddr[addr] = Subscription{Address: addr, Pending: true}
	}
	return nil
}

func (f *fakeSubs) Unsubscribe(_ context.Context, arn string) error {
	for addr, s := range f.byAddr {
		if s.ARN == arn && !s.Pending {
			f.unsubscribes = append(f.unsubscribes, addr)
			delete(f.byAddr, addr)
			return nil
		}
	}
	panic("unsubscribe of an unknown or pending subscription: " + arn)
}

// confirm is the recipient clicking the link.
func (f *fakeSubs) confirm(addr string) {
	f.nextARN++
	f.byAddr[addr] = Subscription{Address: addr, ARN: fmt.Sprintf("arn:aws:sns:us-east-1:%s:alerts:%d", payer, f.nextARN)}
}

// subscribed adds a confirmed subscription the guard didn't make.
func (f *fakeSubs) subscribed(addr string) { f.confirm(addr) }

type harness struct {
	t       *testing.T
	g       *Guard
	jennah  *jennahtest.Server
	client  *jennah.Client
	events  *events
	alerts  *alerts
	state   *memState
	budgets *fakeBudgets
	org     *fakeOrg
	subs    *fakeSubs
	spend   *fakeSpend
	now     time.Time
	nextVer int64
}

func newHarness(t *testing.T) *harness {
	srv, c := jennahtest.Start(t)
	h := &harness{
		t: t, jennah: srv, client: c,
		events: &events{}, alerts: &alerts{t: t}, state: newMemState(), budgets: newFakeBudgets(), subs: newFakeSubs(),
		spend: &fakeSpend{byAccount: map[string]float64{}},
		org:   &fakeOrg{ous: map[string][]string{}, attached: map[string][]config.Target{}},
		now:   time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC),
	}
	h.g = h.newGuard()
	return h
}

func (h *harness) newGuard() *Guard {
	return &Guard{
		S: Settings{
			PayerAccountID:     payer,
			MSPID:              msp,
			GuardVersion:       "v0.0.0-test",
			PolicyA:            "p-styleA",
			PolicyB:            "p-styleB",
			LooseningApprovers: []string{"approver@example.com"},
			LooseningRequired:  1,
			LooseningWindow:    2 * time.Hour,
			LooseningDelay:     30 * time.Minute,
			AlertEmail:         "stack@example.com",
		},
		Config: h.client, Inbox: h.client, Events: h.events, Alerts: h.alerts, State: h.state,
		Budgets: h.budgets, Org: h.org, Subs: h.subs, Spend: h.spend,
		Now: func() time.Time { return h.now },
	}
}

func (h *harness) publish(doc string) int64 {
	h.t.Helper()
	h.nextVer++
	err := h.client.InsertVersion(context.Background(), schema.ConfigDatasetID(msp, payer), jennah.Version{
		Number: h.nextVer, Document: json.RawMessage(doc), PublishedAt: h.now,
	}, "")
	if err != nil {
		h.t.Fatal(err)
	}
	return h.nextVer
}

func (h *harness) pull() {
	h.t.Helper()
	if err := h.g.Pull(context.Background()); err != nil {
		h.t.Fatalf("pull: %v", err)
	}
}

func (h *harness) st() *State {
	st, _ := h.state.Load(context.Background())
	return st
}

func (h *harness) ceiling(target string) float64 {
	b, ok := h.budgets.all[target]
	if !ok {
		h.t.Fatalf("no budget %s", target)
	}
	return b.Spec.LimitUSD
}
