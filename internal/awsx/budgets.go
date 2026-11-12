// Package awsx implements the guard's AWS-facing interfaces: Budgets,
// Organizations, the DynamoDB state store and the SNS alert topic.
//
// Assumptions about AWS behavior here are listed in task 1.1 of
// add-budget-freeze-guard and must be confirmed in a test organization.
package awsx

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	btypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	otypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

// OwnedTagKey marks a budget as created by the guard. Ownership needs both the
// name prefix and this tag.
const OwnedTagKey = "guardrail:owned"

// BudgetsAPI is the part of the Budgets client the guard uses.
type BudgetsAPI interface {
	DescribeBudgets(context.Context, *budgets.DescribeBudgetsInput, ...func(*budgets.Options)) (*budgets.DescribeBudgetsOutput, error)
	DescribeBudget(context.Context, *budgets.DescribeBudgetInput, ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error)
	CreateBudget(context.Context, *budgets.CreateBudgetInput, ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error)
	UpdateBudget(context.Context, *budgets.UpdateBudgetInput, ...func(*budgets.Options)) (*budgets.UpdateBudgetOutput, error)
	DeleteBudget(context.Context, *budgets.DeleteBudgetInput, ...func(*budgets.Options)) (*budgets.DeleteBudgetOutput, error)
	ListTagsForResource(context.Context, *budgets.ListTagsForResourceInput, ...func(*budgets.Options)) (*budgets.ListTagsForResourceOutput, error)
	DescribeNotificationsForBudget(context.Context, *budgets.DescribeNotificationsForBudgetInput, ...func(*budgets.Options)) (*budgets.DescribeNotificationsForBudgetOutput, error)
	CreateNotification(context.Context, *budgets.CreateNotificationInput, ...func(*budgets.Options)) (*budgets.CreateNotificationOutput, error)
	DeleteNotification(context.Context, *budgets.DeleteNotificationInput, ...func(*budgets.Options)) (*budgets.DeleteNotificationOutput, error)
	DescribeBudgetActionsForBudget(context.Context, *budgets.DescribeBudgetActionsForBudgetInput, ...func(*budgets.Options)) (*budgets.DescribeBudgetActionsForBudgetOutput, error)
	CreateBudgetAction(context.Context, *budgets.CreateBudgetActionInput, ...func(*budgets.Options)) (*budgets.CreateBudgetActionOutput, error)
	UpdateBudgetAction(context.Context, *budgets.UpdateBudgetActionInput, ...func(*budgets.Options)) (*budgets.UpdateBudgetActionOutput, error)
	DeleteBudgetAction(context.Context, *budgets.DeleteBudgetActionInput, ...func(*budgets.Options)) (*budgets.DeleteBudgetActionOutput, error)
	ExecuteBudgetAction(context.Context, *budgets.ExecuteBudgetActionInput, ...func(*budgets.Options)) (*budgets.ExecuteBudgetActionOutput, error)
}

// PolicyAPI is the Organizations calls the guard makes on freeze policies:
// detaching one when reversing a Budgets action is not possible, and
// attaching one when the Budgets action can't fire yet.
type PolicyAPI interface {
	AttachPolicy(context.Context, *organizations.AttachPolicyInput, ...func(*organizations.Options)) (*organizations.AttachPolicyOutput, error)
	DetachPolicy(context.Context, *organizations.DetachPolicyInput, ...func(*organizations.Options)) (*organizations.DetachPolicyOutput, error)
}

// Budgets implements guard.Budgets.
type Budgets struct {
	API              BudgetsAPI
	Org              PolicyAPI
	AccountID        string // the payer account
	ExecutionRoleARN string // role Budgets assumes to attach the policy
	NotifyTopicARN   string // budget notifications go here, then to the guard
}

var periods = map[string]btypes.TimeUnit{
	config.PeriodMonthly:   btypes.TimeUnitMonthly,
	config.PeriodQuarterly: btypes.TimeUnitQuarterly,
	config.PeriodAnnually:  btypes.TimeUnitAnnually,
}

func periodOf(tu btypes.TimeUnit) string {
	for p, u := range periods {
		if u == tu {
			return p
		}
	}
	return string(tu)
}

func usd(v float64) *btypes.Spend {
	return &btypes.Spend{Amount: aws.String(strconv.FormatFloat(v, 'f', 2, 64)), Unit: aws.String("USD")}
}

func (b *Budgets) budgetARN(name string) string {
	return fmt.Sprintf("arn:aws:budgets::%s:budget/%s", b.AccountID, name)
}

func (b *Budgets) budget(s guard.BudgetSpec) *btypes.Budget {
	return &btypes.Budget{
		BudgetName:  aws.String(s.Name),
		BudgetType:  btypes.BudgetTypeCost,
		TimeUnit:    periods[s.Period],
		BudgetLimit: usd(s.LimitUSD),
		FilterExpression: &btypes.Expression{Dimensions: &btypes.ExpressionDimensionValues{
			Key:    btypes.DimensionLinkedAccount,
			Values: s.Accounts,
		}},
	}
}

func (b *Budgets) notification(s guard.BudgetSpec) *btypes.Notification {
	return &btypes.Notification{
		NotificationType:   btypes.NotificationTypeActual,
		ComparisonOperator: btypes.ComparisonOperatorGreaterThan,
		Threshold:          float64(s.ThresholdPct),
		ThresholdType:      btypes.ThresholdTypePercentage,
	}
}

func (b *Budgets) subscribers() []btypes.Subscriber {
	return []btypes.Subscriber{{SubscriptionType: btypes.SubscriptionTypeSns, Address: aws.String(b.NotifyTopicARN)}}
}

func approvalModel(enforce bool) btypes.ApprovalModel {
	if enforce {
		return btypes.ApprovalModelAuto
	}
	return btypes.ApprovalModelManual
}

// ListOwned returns guard-owned budgets: name prefix and owned tag.
func (b *Budgets) ListOwned(ctx context.Context) ([]guard.BudgetStatus, error) {
	var out []guard.BudgetStatus
	var token *string
	for {
		page, err := b.API.DescribeBudgets(ctx, &budgets.DescribeBudgetsInput{AccountId: aws.String(b.AccountID), NextToken: token})
		if err != nil {
			return nil, err
		}
		for _, bd := range page.Budgets {
			name := aws.ToString(bd.BudgetName)
			if !strings.HasPrefix(name, guard.BudgetPrefix) {
				continue
			}
			owned, err := b.isOwned(ctx, name)
			if err != nil {
				return nil, err
			}
			if !owned {
				continue
			}
			st, err := b.status(ctx, bd)
			if err != nil {
				return nil, err
			}
			out = append(out, st)
		}
		if page.NextToken == nil || aws.ToString(page.NextToken) == "" {
			return out, nil
		}
		token = page.NextToken
	}
}

func (b *Budgets) isOwned(ctx context.Context, name string) (bool, error) {
	tags, err := b.API.ListTagsForResource(ctx, &budgets.ListTagsForResourceInput{ResourceARN: aws.String(b.budgetARN(name))})
	if err != nil {
		return false, err
	}
	for _, t := range tags.ResourceTags {
		if aws.ToString(t.Key) == OwnedTagKey && aws.ToString(t.Value) == "true" {
			return true, nil
		}
	}
	return false, nil
}

func targetFromName(name string) config.Target {
	rest := strings.TrimPrefix(name, guard.BudgetPrefix)
	kind, id, _ := strings.Cut(rest, "-")
	return config.Target{Kind: kind, ID: id}
}

func (b *Budgets) status(ctx context.Context, bd btypes.Budget) (guard.BudgetStatus, error) {
	name := aws.ToString(bd.BudgetName)
	spec := guard.BudgetSpec{
		Name:   name,
		Target: targetFromName(name),
		Period: periodOf(bd.TimeUnit),
	}
	if bd.BudgetLimit != nil {
		spec.LimitUSD, _ = strconv.ParseFloat(aws.ToString(bd.BudgetLimit.Amount), 64)
	}
	if fe := bd.FilterExpression; fe != nil && fe.Dimensions != nil && fe.Dimensions.Key == btypes.DimensionLinkedAccount {
		spec.Accounts = append([]string(nil), fe.Dimensions.Values...)
		sort.Strings(spec.Accounts)
	}
	executed := false
	if act, err := b.action(ctx, name); err != nil {
		return guard.BudgetStatus{}, err
	} else if act != nil {
		if act.ActionThreshold != nil {
			spec.ThresholdPct = int(act.ActionThreshold.ActionThresholdValue)
		}
		if act.Definition != nil && act.Definition.ScpActionDefinition != nil {
			spec.PolicyID = aws.ToString(act.Definition.ScpActionDefinition.PolicyId)
		}
		spec.Enforce = act.ApprovalModel == btypes.ApprovalModelAuto
		executed = act.Status == btypes.ActionStatusExecutionSuccess
	} else {
		// A dry-run budget has no action; its threshold is the notification's.
		pct, err := b.notificationThreshold(ctx, name)
		if err != nil {
			return guard.BudgetStatus{}, err
		}
		spec.ThresholdPct = pct
	}
	st := guard.BudgetStatus{Spec: spec, ActionExecuted: executed}
	if cs := bd.CalculatedSpend; cs != nil && cs.ActualSpend != nil {
		st.ActualUSD, _ = strconv.ParseFloat(aws.ToString(cs.ActualSpend.Amount), 64)
	}
	if bd.TimePeriod != nil && bd.TimePeriod.Start != nil {
		st.PeriodStart = *bd.TimePeriod.Start
	}
	return st, nil
}

// sameBudget compares the fields the guard sets: limit, period and accounts.
func sameBudget(have btypes.Budget, want *btypes.Budget) bool {
	hl, _ := strconv.ParseFloat(aws.ToString(spendAmount(have.BudgetLimit)), 64)
	wl, _ := strconv.ParseFloat(aws.ToString(spendAmount(want.BudgetLimit)), 64)
	if hl != wl || have.TimeUnit != want.TimeUnit {
		return false
	}
	return strings.Join(linkedAccounts(have.FilterExpression), ",") == strings.Join(linkedAccounts(want.FilterExpression), ",")
}

func spendAmount(s *btypes.Spend) *string {
	if s == nil {
		return nil
	}
	return s.Amount
}

func linkedAccounts(e *btypes.Expression) []string {
	if e == nil || e.Dimensions == nil || e.Dimensions.Key != btypes.DimensionLinkedAccount {
		return nil
	}
	out := append([]string(nil), e.Dimensions.Values...)
	sort.Strings(out)
	return out
}

// isPercentage reports whether a stored notification's threshold is a
// percentage. Budgets leaves the type out when it is PERCENTAGE, its default,
// so an empty type counts as one. Without this every run would replace the
// notification, which resets the budget's calculated spend.
func isPercentage(t btypes.ThresholdType) bool {
	return t == btypes.ThresholdTypePercentage || t == ""
}

func (b *Budgets) notificationThreshold(ctx context.Context, name string) (int, error) {
	out, err := b.API.DescribeNotificationsForBudget(ctx, &budgets.DescribeNotificationsForBudgetInput{
		AccountId: aws.String(b.AccountID), BudgetName: aws.String(name),
	})
	var nf *btypes.NotFoundException
	if errors.As(err, &nf) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, n := range out.Notifications {
		if n.NotificationType == btypes.NotificationTypeActual && isPercentage(n.ThresholdType) {
			return int(n.Threshold), nil
		}
	}
	return 0, nil
}

func (b *Budgets) action(ctx context.Context, name string) (*btypes.Action, error) {
	out, err := b.API.DescribeBudgetActionsForBudget(ctx, &budgets.DescribeBudgetActionsForBudgetInput{
		AccountId: aws.String(b.AccountID), BudgetName: aws.String(name),
	})
	var nf *btypes.NotFoundException
	if errors.As(err, &nf) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for i := range out.Actions {
		if out.Actions[i].ActionType == btypes.ActionTypeScp {
			return &out.Actions[i], nil
		}
	}
	return nil, nil
}

// Put creates or updates the budget and its notification. An enforcing budget
// also gets its freeze action; a dry-run budget has none, so it is not billed
// as an action-enabled budget, and an action left from enforce is deleted.
func (b *Budgets) Put(ctx context.Context, s guard.BudgetSpec) error {
	existing, err := b.action(ctx, s.Name)
	if err != nil {
		return err
	}
	cur, err := b.API.DescribeBudget(ctx, &budgets.DescribeBudgetInput{AccountId: aws.String(b.AccountID), BudgetName: aws.String(s.Name)})
	var nf *btypes.NotFoundException
	switch {
	case errors.As(err, &nf):
		err = nil
		cur = nil
	case err != nil:
		return fmt.Errorf("describe budget: %w", err)
	}
	if cur != nil && cur.Budget != nil && sameBudget(*cur.Budget, b.budget(s)) {
		// Nothing about the budget itself changed: leave it alone, so Budgets
		// keeps its spend calculation and notification state untouched.
		if err := b.syncNotification(ctx, s); err != nil {
			return err
		}
	} else if cur == nil || cur.Budget == nil {
		_, err = b.API.CreateBudget(ctx, &budgets.CreateBudgetInput{
			AccountId: aws.String(b.AccountID),
			Budget:    b.budget(s),
			NotificationsWithSubscribers: []btypes.NotificationWithSubscribers{{
				Notification: b.notification(s), Subscribers: b.subscribers(),
			}},
			ResourceTags: []btypes.ResourceTag{{Key: aws.String(OwnedTagKey), Value: aws.String("true")}},
		})
		if err != nil {
			return fmt.Errorf("create budget: %w", err)
		}
	} else {
		if _, err := b.API.UpdateBudget(ctx, &budgets.UpdateBudgetInput{AccountId: aws.String(b.AccountID), NewBudget: b.budget(s)}); err != nil {
			return fmt.Errorf("update budget: %w", err)
		}
		if err := b.syncNotification(ctx, s); err != nil {
			return err
		}
	}

	if !s.Enforce {
		if existing == nil {
			return nil
		}
		if _, err := b.API.DeleteBudgetAction(ctx, &budgets.DeleteBudgetActionInput{
			AccountId: aws.String(b.AccountID), BudgetName: aws.String(s.Name), ActionId: existing.ActionId,
		}); err != nil {
			return fmt.Errorf("delete budget action of dry-run scope: %w", err)
		}
		return nil
	}

	threshold := &btypes.ActionThreshold{ActionThresholdValue: float64(s.ThresholdPct), ActionThresholdType: btypes.ThresholdTypePercentage}
	def := &btypes.Definition{ScpActionDefinition: &btypes.ScpActionDefinition{PolicyId: aws.String(s.PolicyID), TargetIds: []string{s.Target.ID}}}
	want := btypes.Action{
		NotificationType: btypes.NotificationTypeActual,
		ActionThreshold:  threshold,
		Definition:       def,
		ExecutionRoleArn: aws.String(b.ExecutionRoleARN),
		ApprovalModel:    approvalModel(s.Enforce),
		Subscribers:      b.subscribers(),
	}
	if existing != nil && sameAction(*existing, want) {
		// Leave an unchanged action alone, whatever its status: rewriting it
		// on every run might reset what Budgets has calculated or is doing.
		return nil
	}
	if existing == nil {
		_, err = b.API.CreateBudgetAction(ctx, &budgets.CreateBudgetActionInput{
			AccountId:        aws.String(b.AccountID),
			BudgetName:       aws.String(s.Name),
			NotificationType: btypes.NotificationTypeActual,
			ActionType:       btypes.ActionTypeScp,
			ActionThreshold:  threshold,
			Definition:       def,
			ExecutionRoleArn: aws.String(b.ExecutionRoleARN),
			ApprovalModel:    approvalModel(s.Enforce),
			Subscribers:      b.subscribers(),
		})
		if err != nil {
			if isActionLimit(err) {
				return fmt.Errorf("create budget action: %w: %w", guard.ErrBudgetActionLimit, err)
			}
			return fmt.Errorf("create budget action: %w", err)
		}
		return nil
	}
	_, err = b.API.UpdateBudgetAction(ctx, &budgets.UpdateBudgetActionInput{
		AccountId:        aws.String(b.AccountID),
		BudgetName:       aws.String(s.Name),
		ActionId:         existing.ActionId,
		NotificationType: btypes.NotificationTypeActual,
		ActionThreshold:  threshold,
		Definition:       def,
		ExecutionRoleArn: aws.String(b.ExecutionRoleARN),
		ApprovalModel:    approvalModel(s.Enforce),
		Subscribers:      b.subscribers(),
	})
	if err != nil {
		return fmt.Errorf("update budget action: %w", err)
	}
	return nil
}

// sameAction reports whether an existing action already has every field the
// guard sets. A missing threshold type counts as a percentage, as for
// notifications; target ids and subscribers are compared as sets.
func sameAction(cur, want btypes.Action) bool {
	if cur.NotificationType != want.NotificationType || cur.ApprovalModel != want.ApprovalModel ||
		aws.ToString(cur.ExecutionRoleArn) != aws.ToString(want.ExecutionRoleArn) {
		return false
	}
	if cur.ActionThreshold == nil || cur.ActionThreshold.ActionThresholdValue != want.ActionThreshold.ActionThresholdValue ||
		!isPercentage(cur.ActionThreshold.ActionThresholdType) {
		return false
	}
	if cur.Definition == nil || cur.Definition.ScpActionDefinition == nil {
		return false
	}
	c, w := cur.Definition.ScpActionDefinition, want.Definition.ScpActionDefinition
	if aws.ToString(c.PolicyId) != aws.ToString(w.PolicyId) || !sameSet(c.TargetIds, w.TargetIds) {
		return false
	}
	subs := func(list []btypes.Subscriber) []string {
		var out []string
		for _, s := range list {
			out = append(out, string(s.SubscriptionType)+" "+aws.ToString(s.Address))
		}
		return out
	}
	return sameSet(subs(cur.Subscribers), subs(want.Subscribers))
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		if seen[x] == 0 {
			return false
		}
		seen[x]--
	}
	return true
}

// syncNotification replaces the budget's notification when its threshold
// changed, so dry-run would_freeze events fire at the same threshold as the
// action.
func (b *Budgets) syncNotification(ctx context.Context, s guard.BudgetSpec) error {
	out, err := b.API.DescribeNotificationsForBudget(ctx, &budgets.DescribeNotificationsForBudgetInput{
		AccountId: aws.String(b.AccountID), BudgetName: aws.String(s.Name),
	})
	if err != nil {
		var nf *btypes.NotFoundException
		if !errors.As(err, &nf) {
			return fmt.Errorf("describe notifications: %w", err)
		}
	}
	want := b.notification(s)
	if out != nil {
		for _, n := range out.Notifications {
			if n.Threshold == want.Threshold && n.NotificationType == want.NotificationType && isPercentage(n.ThresholdType) {
				return nil
			}
			n := n
			if _, err := b.API.DeleteNotification(ctx, &budgets.DeleteNotificationInput{
				AccountId: aws.String(b.AccountID), BudgetName: aws.String(s.Name), Notification: &n,
			}); err != nil {
				return fmt.Errorf("delete notification: %w", err)
			}
		}
	}
	_, err = b.API.CreateNotification(ctx, &budgets.CreateNotificationInput{
		AccountId: aws.String(b.AccountID), BudgetName: aws.String(s.Name),
		Notification: want, Subscribers: b.subscribers(),
	})
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

// Delete removes the budget's action, then the budget.
func (b *Budgets) Delete(ctx context.Context, name string) error {
	if act, err := b.action(ctx, name); err != nil {
		return err
	} else if act != nil {
		if _, err := b.API.DeleteBudgetAction(ctx, &budgets.DeleteBudgetActionInput{
			AccountId: aws.String(b.AccountID), BudgetName: aws.String(name), ActionId: act.ActionId,
		}); err != nil {
			return fmt.Errorf("delete budget action: %w", err)
		}
	}
	_, err := b.API.DeleteBudget(ctx, &budgets.DeleteBudgetInput{AccountId: aws.String(b.AccountID), BudgetName: aws.String(name)})
	var nf *btypes.NotFoundException
	if err != nil && !errors.As(err, &nf) {
		return fmt.Errorf("delete budget: %w", err)
	}
	return nil
}

// Release lifts a freeze: reverse the Budgets action when it is in an
// executed state, otherwise detach the policy directly.
func (b *Budgets) Release(ctx context.Context, s guard.BudgetSpec) (string, error) {
	act, err := b.action(ctx, s.Name)
	if err != nil {
		return "", err
	}
	if act != nil && act.Status == btypes.ActionStatusExecutionSuccess {
		_, err := b.API.ExecuteBudgetAction(ctx, &budgets.ExecuteBudgetActionInput{
			AccountId: aws.String(b.AccountID), BudgetName: aws.String(s.Name),
			ActionId: act.ActionId, ExecutionType: btypes.ExecutionTypeReverseBudgetAction,
		})
		if err == nil {
			return "reverse_budget_action", nil
		}
	}
	policy := s.PolicyID
	if act != nil && act.Definition != nil && act.Definition.ScpActionDefinition != nil {
		policy = aws.ToString(act.Definition.ScpActionDefinition.PolicyId)
	}
	_, err = b.Org.DetachPolicy(ctx, &organizations.DetachPolicyInput{PolicyId: aws.String(policy), TargetId: aws.String(s.Target.ID)})
	var gone *otypes.PolicyNotAttachedException
	if errors.As(err, &gone) {
		// AWS reset the action when the budget was updated and detached the
		// policy already (fix-action-reset-unfreeze).
		return "already_detached", nil
	}
	if err != nil {
		return "", fmt.Errorf("detach %s from %s: %w", policy, s.Target.ID, err)
	}
	return "detach_policy", nil
}

// Freeze attaches the budget's freeze policy to its target. Budgets can't fire
// its action while it reports no spend, which it does for a while after the
// budget is created or updated, so the guard attaches the policy itself when
// Cost Explorer shows the scope over its threshold. A lift goes through
// Release, which detaches the policy when the action didn't attach it.
func (b *Budgets) Freeze(ctx context.Context, s guard.BudgetSpec) error {
	if !s.Enforce || s.PolicyID == "" {
		return fmt.Errorf("freeze %s: a dry-run scope is never frozen", s.Target)
	}
	_, err := b.Org.AttachPolicy(ctx, &organizations.AttachPolicyInput{PolicyId: aws.String(s.PolicyID), TargetId: aws.String(s.Target.ID)})
	var dup *otypes.DuplicatePolicyAttachmentException
	if err != nil && !errors.As(err, &dup) {
		return fmt.Errorf("attach %s to %s: %w", s.PolicyID, s.Target.ID, err)
	}
	return nil
}

// ActionStatus reads the budget's freeze action status, or "" when it has
// none.
func (b *Budgets) ActionStatus(ctx context.Context, name string) (string, error) {
	act, err := b.action(ctx, name)
	if err != nil || act == nil {
		return "", err
	}
	return string(act.Status), nil
}

var _ guard.Budgets = (*Budgets)(nil)

// isActionLimit reports whether AWS refused an action because the account is at
// its budget action limit. AWS doesn't document which of its two limit errors
// CreateBudgetAction returns for this, so both count.
func isActionLimit(err error) bool {
	var quota *btypes.ServiceQuotaExceededException
	var creation *btypes.CreationLimitExceededException
	return errors.As(err, &quota) || errors.As(err, &creation)
}
