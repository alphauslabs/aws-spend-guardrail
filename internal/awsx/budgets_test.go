package awsx

import (
	"context"
	"errors"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	btypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
)

type fakeAPI struct {
	budgets  map[string]btypes.Budget
	tags     map[string]bool
	actions  map[string]btypes.Action
	notifs   map[string][]btypes.Notification
	executed []string
	failExec bool
	updates  int
	// actionUpdates counts UpdateBudgetAction calls.
	actionUpdates int
	// notifWrites counts notification creates and deletes. AWS resets a
	// budget's calculated spend when its notifications change.
	notifWrites int
}

// stored mimics what Budgets returns for a notification: a PERCENTAGE
// threshold type, being the default, is left out.
func stored(n btypes.Notification) btypes.Notification {
	if n.ThresholdType == btypes.ThresholdTypePercentage {
		n.ThresholdType = ""
	}
	return n
}

// storedThreshold returns an action threshold the way the guard must accept
// it back. Live Budgets returns the PERCENTAGE type (seen 2026-11-11), but the
// fake leaves it out, as Budgets does for notifications, so the guard is
// tested against the less forgiving form.
func storedThreshold(t *btypes.ActionThreshold) *btypes.ActionThreshold {
	if t == nil {
		return nil
	}
	c := *t
	if c.ActionThresholdType == btypes.ThresholdTypePercentage {
		c.ActionThresholdType = ""
	}
	return &c
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{budgets: map[string]btypes.Budget{}, tags: map[string]bool{}, actions: map[string]btypes.Action{}, notifs: map[string][]btypes.Notification{}}
}

func notFound() error { return &btypes.NotFoundException{Message: aws.String("nf")} }

func (f *fakeAPI) DescribeBudgets(context.Context, *budgets.DescribeBudgetsInput, ...func(*budgets.Options)) (*budgets.DescribeBudgetsOutput, error) {
	out := &budgets.DescribeBudgetsOutput{}
	for _, b := range f.budgets {
		out.Budgets = append(out.Budgets, b)
	}
	return out, nil
}
func (f *fakeAPI) DescribeBudget(_ context.Context, in *budgets.DescribeBudgetInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error) {
	b, ok := f.budgets[aws.ToString(in.BudgetName)]
	if !ok {
		return nil, notFound()
	}
	return &budgets.DescribeBudgetOutput{Budget: &b}, nil
}
func (f *fakeAPI) CreateBudget(_ context.Context, in *budgets.CreateBudgetInput, _ ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error) {
	n := aws.ToString(in.Budget.BudgetName)
	f.budgets[n] = *in.Budget
	for _, t := range in.ResourceTags {
		if aws.ToString(t.Key) == OwnedTagKey {
			f.tags[n] = true
		}
	}
	for _, ns := range in.NotificationsWithSubscribers {
		f.notifs[n] = append(f.notifs[n], stored(*ns.Notification))
	}
	return &budgets.CreateBudgetOutput{}, nil
}
func (f *fakeAPI) UpdateBudget(_ context.Context, in *budgets.UpdateBudgetInput, _ ...func(*budgets.Options)) (*budgets.UpdateBudgetOutput, error) {
	n := aws.ToString(in.NewBudget.BudgetName)
	if _, ok := f.budgets[n]; !ok {
		return nil, notFound()
	}
	f.budgets[n] = *in.NewBudget
	f.updates++
	return &budgets.UpdateBudgetOutput{}, nil
}
func (f *fakeAPI) DeleteBudget(_ context.Context, in *budgets.DeleteBudgetInput, _ ...func(*budgets.Options)) (*budgets.DeleteBudgetOutput, error) {
	delete(f.budgets, aws.ToString(in.BudgetName))
	return &budgets.DeleteBudgetOutput{}, nil
}
func (f *fakeAPI) ListTagsForResource(_ context.Context, in *budgets.ListTagsForResourceInput, _ ...func(*budgets.Options)) (*budgets.ListTagsForResourceOutput, error) {
	out := &budgets.ListTagsForResourceOutput{}
	name := aws.ToString(in.ResourceARN)
	name = name[len("arn:aws:budgets::123456789012:budget/"):]
	if f.tags[name] {
		out.ResourceTags = []btypes.ResourceTag{{Key: aws.String(OwnedTagKey), Value: aws.String("true")}}
	}
	return out, nil
}
func (f *fakeAPI) DescribeNotificationsForBudget(_ context.Context, in *budgets.DescribeNotificationsForBudgetInput, _ ...func(*budgets.Options)) (*budgets.DescribeNotificationsForBudgetOutput, error) {
	return &budgets.DescribeNotificationsForBudgetOutput{Notifications: f.notifs[aws.ToString(in.BudgetName)]}, nil
}
func (f *fakeAPI) CreateNotification(_ context.Context, in *budgets.CreateNotificationInput, _ ...func(*budgets.Options)) (*budgets.CreateNotificationOutput, error) {
	n := aws.ToString(in.BudgetName)
	f.notifs[n] = append(f.notifs[n], stored(*in.Notification))
	f.notifWrites++
	return &budgets.CreateNotificationOutput{}, nil
}
func (f *fakeAPI) DeleteNotification(_ context.Context, in *budgets.DeleteNotificationInput, _ ...func(*budgets.Options)) (*budgets.DeleteNotificationOutput, error) {
	f.notifs[aws.ToString(in.BudgetName)] = nil
	f.notifWrites++
	return &budgets.DeleteNotificationOutput{}, nil
}
func (f *fakeAPI) DescribeBudgetActionsForBudget(_ context.Context, in *budgets.DescribeBudgetActionsForBudgetInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetActionsForBudgetOutput, error) {
	a, ok := f.actions[aws.ToString(in.BudgetName)]
	if !ok {
		return &budgets.DescribeBudgetActionsForBudgetOutput{}, nil
	}
	return &budgets.DescribeBudgetActionsForBudgetOutput{Actions: []btypes.Action{a}}, nil
}
func (f *fakeAPI) CreateBudgetAction(_ context.Context, in *budgets.CreateBudgetActionInput, _ ...func(*budgets.Options)) (*budgets.CreateBudgetActionOutput, error) {
	n := aws.ToString(in.BudgetName)
	f.actions[n] = btypes.Action{ActionId: aws.String("act-" + n), ActionType: in.ActionType, NotificationType: in.NotificationType,
		ActionThreshold: storedThreshold(in.ActionThreshold), Definition: in.Definition, ExecutionRoleArn: in.ExecutionRoleArn,
		ApprovalModel: in.ApprovalModel, Subscribers: in.Subscribers, Status: btypes.ActionStatusStandby}
	return &budgets.CreateBudgetActionOutput{}, nil
}
func (f *fakeAPI) UpdateBudgetAction(_ context.Context, in *budgets.UpdateBudgetActionInput, _ ...func(*budgets.Options)) (*budgets.UpdateBudgetActionOutput, error) {
	n := aws.ToString(in.BudgetName)
	a := f.actions[n]
	f.actionUpdates++
	a.NotificationType, a.ActionThreshold, a.Definition, a.ApprovalModel = in.NotificationType, storedThreshold(in.ActionThreshold), in.Definition, in.ApprovalModel
	a.ExecutionRoleArn, a.Subscribers = in.ExecutionRoleArn, in.Subscribers
	f.actions[n] = a
	return &budgets.UpdateBudgetActionOutput{}, nil
}
func (f *fakeAPI) DeleteBudgetAction(_ context.Context, in *budgets.DeleteBudgetActionInput, _ ...func(*budgets.Options)) (*budgets.DeleteBudgetActionOutput, error) {
	delete(f.actions, aws.ToString(in.BudgetName))
	return &budgets.DeleteBudgetActionOutput{}, nil
}
func (f *fakeAPI) ExecuteBudgetAction(_ context.Context, in *budgets.ExecuteBudgetActionInput, _ ...func(*budgets.Options)) (*budgets.ExecuteBudgetActionOutput, error) {
	if f.failExec {
		return nil, errors.New("cannot reverse")
	}
	f.executed = append(f.executed, string(in.ExecutionType))
	return &budgets.ExecuteBudgetActionOutput{}, nil
}

type fakeDetach struct{ detached []string }

func (d *fakeDetach) DetachPolicy(_ context.Context, in *organizations.DetachPolicyInput, _ ...func(*organizations.Options)) (*organizations.DetachPolicyOutput, error) {
	d.detached = append(d.detached, aws.ToString(in.PolicyId)+"@"+aws.ToString(in.TargetId))
	return &organizations.DetachPolicyOutput{}, nil
}

func spec(enforce bool) guard.BudgetSpec {
	return guard.BudgetSpec{
		Name:         "guardrail-account-111122223333",
		Target:       config.Target{Kind: "account", ID: "111122223333"},
		Accounts:     []string{"111122223333"},
		LimitUSD:     50000,
		Period:       config.PeriodMonthly,
		ThresholdPct: 90,
		PolicyID:     "p-styleB",
		Enforce:      enforce,
	}
}

func newBudgets(api *fakeAPI, d *fakeDetach) *Budgets {
	return &Budgets{API: api, Org: d, AccountID: "123456789012", ExecutionRoleARN: "arn:role", NotifyTopicARN: "arn:topic"}
}

func dryRunSpec() guard.BudgetSpec {
	s := spec(false)
	s.PolicyID = "" // the guard gives dry-run scopes no policy
	return s
}

func TestDryRunBudgetHasNoAction(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	b := newBudgets(api, &fakeDetach{})
	if err := b.Put(ctx, dryRunSpec()); err != nil {
		t.Fatal(err)
	}
	if len(api.actions) != 0 {
		t.Fatalf("a dry-run budget must have no action: %+v", api.actions)
	}
	if !api.tags["guardrail-account-111122223333"] || len(api.notifs["guardrail-account-111122223333"]) != 1 {
		t.Fatal("dry-run budget should be tagged and have its notification")
	}
	got, err := b.ListOwned(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v %v", got, err)
	}
	s, want := got[0].Spec, dryRunSpec()
	if s.Name != want.Name || s.Target != want.Target || s.LimitUSD != want.LimitUSD || s.ThresholdPct != 90 ||
		s.PolicyID != "" || s.Enforce || s.Period != config.PeriodMonthly {
		t.Fatalf("round trip of a dry-run budget: %+v", s)
	}
}

func TestSwitchingModeAddsAndRemovesTheAction(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	b := newBudgets(api, &fakeDetach{})
	_ = b.Put(ctx, dryRunSpec())

	if err := b.Put(ctx, spec(true)); err != nil {
		t.Fatal(err)
	}
	a, ok := api.actions["guardrail-account-111122223333"]
	if !ok || a.ApprovalModel != btypes.ApprovalModelAuto || a.ActionType != btypes.ActionTypeScp ||
		aws.ToString(a.Definition.ScpActionDefinition.PolicyId) != "p-styleB" {
		t.Fatalf("switching to enforce should add an automatic SCP action: %+v", a)
	}
	got, _ := b.ListOwned(ctx)
	if s := got[0].Spec; !s.Enforce || s.PolicyID != "p-styleB" || s.ThresholdPct != 90 {
		t.Fatalf("round trip of an enforcing budget: %+v", s)
	}

	if err := b.Put(ctx, dryRunSpec()); err != nil {
		t.Fatal(err)
	}
	if len(api.actions) != 0 {
		t.Fatal("switching back to dry-run should delete the action")
	}
	if _, ok := api.budgets["guardrail-account-111122223333"]; !ok || len(api.notifs["guardrail-account-111122223333"]) != 1 {
		t.Fatal("the budget and its notification must stay")
	}
}

func TestListOwnedSkipsUntaggedAndUnprefixed(t *testing.T) {
	api := newFakeAPI()
	api.budgets["team-budget"] = btypes.Budget{BudgetName: aws.String("team-budget")}
	api.budgets["guardrail-account-999"] = btypes.Budget{BudgetName: aws.String("guardrail-account-999")} // no tag
	got, err := newBudgets(api, &fakeDetach{}).ListOwned(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestThresholdChangeReplacesNotification(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	b := newBudgets(api, &fakeDetach{})
	_ = b.Put(ctx, spec(false))
	s := spec(false)
	s.ThresholdPct = 80
	_ = b.Put(ctx, s)
	ns := api.notifs[s.Name]
	if len(ns) != 1 || ns[0].Threshold != 80 {
		t.Fatalf("notifications %+v", ns)
	}
}

// Budgets leaves a PERCENTAGE threshold type out of what it returns. An
// unchanged budget must still be left alone on every run: rewriting its
// notification resets the budget's calculated spend, so it would never reach
// its threshold. Seen live on 2026-11-10, when every 5-minute run recreated
// every notification and actual spend stayed at 0.
func TestUnchangedBudgetWritesNothing(t *testing.T) {
	ctx := context.Background()
	for _, s := range []guard.BudgetSpec{dryRunSpec(), spec(true)} {
		api := newFakeAPI()
		b := newBudgets(api, &fakeDetach{})
		if err := b.Put(ctx, s); err != nil {
			t.Fatal(err)
		}
		api.notifWrites, api.updates = 0, 0
		for i := 0; i < 3; i++ {
			if err := b.Put(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
		if api.notifWrites != 0 || api.updates != 0 || api.actionUpdates != 0 {
			t.Fatalf("enforce=%v: unchanged budget rewrote %d notification(s), %d budget(s), and %d action(s)", s.Enforce, api.notifWrites, api.updates, api.actionUpdates)
		}
		got, err := b.ListOwned(ctx)
		if err != nil || len(got) != 1 || got[0].Spec.ThresholdPct != 90 {
			t.Fatalf("enforce=%v: threshold not read back from the stored notification: %+v %v", s.Enforce, got, err)
		}
	}
}

func TestReleaseReversesExecutedActionElseDetaches(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	d := &fakeDetach{}
	b := newBudgets(api, d)
	_ = b.Put(ctx, spec(true))
	a := api.actions["guardrail-account-111122223333"]
	a.Status = btypes.ActionStatusExecutionSuccess
	api.actions["guardrail-account-111122223333"] = a

	how, err := b.Release(ctx, spec(true))
	if err != nil || how != "reverse_budget_action" || len(api.executed) != 1 {
		t.Fatalf("how %s err %v", how, err)
	}
	api.failExec = true
	how, err = b.Release(ctx, spec(true))
	if err != nil || how != "detach_policy" || d.detached[0] != "p-styleB@111122223333" {
		t.Fatalf("fallback: %s %v %v", how, err, d.detached)
	}
}

func TestDeleteRemovesActionThenBudget(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	b := newBudgets(api, &fakeDetach{})
	_ = b.Put(ctx, spec(true))
	if err := b.Delete(ctx, "guardrail-account-111122223333"); err != nil {
		t.Fatal(err)
	}
	if len(api.actions) != 0 || len(api.budgets) != 0 {
		t.Fatal("not deleted")
	}
}

func TestBudgetNameFromNotification(t *testing.T) {
	text := "AWS Budget Notification November 10, 2026\nAWS Account 123456789012\n\nDear AWS Customer,\n\nBudget Name: guardrail-account-111122223333\nBudget Type: Cost\n"
	if n, err := BudgetNameFromNotification(text); err != nil || n != "guardrail-account-111122223333" {
		t.Fatalf("text: %q %v", n, err)
	}
	if n, err := BudgetNameFromNotification(`{"budgetName":"guardrail-ou-ou-ab12-cd34ef56"}`); err != nil || n != "guardrail-ou-ou-ab12-cd34ef56" {
		t.Fatalf("json: %q %v", n, err)
	}
	if _, err := BudgetNameFromNotification("hello"); err == nil {
		t.Fatal("want error")
	}
}

func TestUnchangedBudgetIsNotUpdated(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	b := newBudgets(api, &fakeDetach{})
	_ = b.Put(ctx, dryRunSpec())
	_ = b.Put(ctx, dryRunSpec())
	_ = b.Put(ctx, spec(true)) // only the action changes
	if api.updates != 0 {
		t.Fatalf("UpdateBudget called %d times for an unchanged budget", api.updates)
	}
	s := spec(true)
	s.LimitUSD = 60000
	_ = b.Put(ctx, s)
	if api.updates != 1 {
		t.Fatalf("a new limit should update the budget once, got %d", api.updates)
	}
}

// Each field the guard sets on an action, when changed, updates the action
// once; a frozen action with unchanged config is left alone.
func TestActionUpdatedOnlyWhenChanged(t *testing.T) {
	ctx := context.Background()
	changes := map[string]func(*Budgets, *guard.BudgetSpec, *fakeAPI){
		"threshold": func(_ *Budgets, s *guard.BudgetSpec, _ *fakeAPI) { s.ThresholdPct = 80 },
		"policy":    func(_ *Budgets, s *guard.BudgetSpec, _ *fakeAPI) { s.PolicyID = "p-styleA" },
		"role":      func(b *Budgets, _ *guard.BudgetSpec, _ *fakeAPI) { b.ExecutionRoleARN = "arn:role2" },
		"subscriber changed outside": func(_ *Budgets, _ *guard.BudgetSpec, api *fakeAPI) {
			a := api.actions[spec(true).Name]
			a.Subscribers = append(a.Subscribers, btypes.Subscriber{SubscriptionType: btypes.SubscriptionTypeEmail, Address: aws.String("x@example.com")})
			api.actions[spec(true).Name] = a
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI()
			b := newBudgets(api, &fakeDetach{})
			s := spec(true)
			if err := b.Put(ctx, s); err != nil {
				t.Fatal(err)
			}
			change(b, &s, api)
			for i := 0; i < 3; i++ {
				if err := b.Put(ctx, s); err != nil {
					t.Fatal(err)
				}
			}
			if api.actionUpdates != 1 {
				t.Fatalf("%d action updates, want 1", api.actionUpdates)
			}
			if a := api.actions[s.Name]; len(a.Subscribers) != 1 || a.ActionThreshold.ActionThresholdValue != float64(s.ThresholdPct) {
				t.Fatalf("action not put back: %+v", a)
			}
		})
	}
	t.Run("frozen", func(t *testing.T) {
		api := newFakeAPI()
		b := newBudgets(api, &fakeDetach{})
		s := spec(true)
		_ = b.Put(ctx, s)
		a := api.actions[s.Name]
		a.Status = btypes.ActionStatusExecutionSuccess
		api.actions[s.Name] = a
		_ = b.Put(ctx, s)
		if api.actionUpdates != 0 || api.actions[s.Name].Status != btypes.ActionStatusExecutionSuccess {
			t.Fatalf("a frozen action with unchanged config must be left alone: %d updates", api.actionUpdates)
		}
	})
}
