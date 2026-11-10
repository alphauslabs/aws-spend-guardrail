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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	btypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
)

// OwnedTagKey marks a budget as created by the guard. Ownership needs both the
// name prefix and this tag.
const OwnedTagKey = "guardrail:owned"

// BudgetsAPI is the part of the Budgets client the guard uses.
type BudgetsAPI interface {
	DescribeBudgets(context.Context, *budgets.DescribeBudgetsInput, ...func(*budgets.Options)) (*budgets.DescribeBudgetsOutput, error)
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

// DetachAPI is the Organizations call used when reversing a Budgets action is
// not possible.
type DetachAPI interface {
	DetachPolicy(context.Context, *organizations.DetachPolicyInput, ...func(*organizations.Options)) (*organizations.DetachPolicyOutput, error)
}

// Budgets implements guard.Budgets.
type Budgets struct {
	API              BudgetsAPI
	Org              DetachAPI
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
	}
	st := guard.BudgetStatus{Spec: spec}
	if cs := bd.CalculatedSpend; cs != nil && cs.ActualSpend != nil {
		st.ActualUSD, _ = strconv.ParseFloat(aws.ToString(cs.ActualSpend.Amount), 64)
	}
	if bd.TimePeriod != nil && bd.TimePeriod.Start != nil {
		st.PeriodStart = *bd.TimePeriod.Start
	}
	return st, nil
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

// Put creates or updates the budget, its notification and its freeze action.
func (b *Budgets) Put(ctx context.Context, s guard.BudgetSpec) error {
	existing, err := b.action(ctx, s.Name)
	if err != nil {
		return err
	}
	_, err = b.API.UpdateBudget(ctx, &budgets.UpdateBudgetInput{AccountId: aws.String(b.AccountID), NewBudget: b.budget(s)})
	var nf *btypes.NotFoundException
	if errors.As(err, &nf) {
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
	} else if err != nil {
		return fmt.Errorf("update budget: %w", err)
	} else if err := b.syncNotification(ctx, s); err != nil {
		return err
	}

	threshold := &btypes.ActionThreshold{ActionThresholdValue: float64(s.ThresholdPct), ActionThresholdType: btypes.ThresholdTypePercentage}
	def := &btypes.Definition{ScpActionDefinition: &btypes.ScpActionDefinition{PolicyId: aws.String(s.PolicyID), TargetIds: []string{s.Target.ID}}}
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
			if n.Threshold == want.Threshold && n.NotificationType == want.NotificationType && n.ThresholdType == want.ThresholdType {
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
	if err != nil {
		return "", fmt.Errorf("detach %s from %s: %w", policy, s.Target.ID, err)
	}
	return "detach_policy", nil
}

var _ guard.Budgets = (*Budgets)(nil)
