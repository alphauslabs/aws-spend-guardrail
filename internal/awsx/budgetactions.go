package awsx

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
)

// BudgetActionsAPI lists every budget action in an account. Preflight uses it
// with the operator's credentials; the guard doesn't need it.
type BudgetActionsAPI interface {
	DescribeBudgetActionsForAccount(context.Context, *budgets.DescribeBudgetActionsForAccountInput, ...func(*budgets.Options)) (*budgets.DescribeBudgetActionsForAccountOutput, error)
}

// BudgetActionsOutside counts the budget actions in an account on budgets whose
// name doesn't start with prefix. AWS allows 100 budget actions per account, and
// these take room the guard's enforcing scopes can't use. Actions on the
// guard's own budgets aren't counted: they belong to the config's enforcing
// scopes, or the guard removes them.
func BudgetActionsOutside(ctx context.Context, api BudgetActionsAPI, accountID, prefix string) (int, error) {
	n := 0
	var token *string
	for {
		out, err := api.DescribeBudgetActionsForAccount(ctx, &budgets.DescribeBudgetActionsForAccountInput{
			AccountId: aws.String(accountID), NextToken: token,
		})
		if err != nil {
			return 0, fmt.Errorf("list budget actions: %w", err)
		}
		for _, a := range out.Actions {
			if !strings.HasPrefix(aws.ToString(a.BudgetName), prefix) {
				n++
			}
		}
		if aws.ToString(out.NextToken) == "" {
			return n, nil
		}
		token = out.NextToken
	}
}
