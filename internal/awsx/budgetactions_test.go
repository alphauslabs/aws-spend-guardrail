package awsx

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	btypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
)

type fakeActionPages struct{ pages [][]string }

func (f *fakeActionPages) DescribeBudgetActionsForAccount(_ context.Context, in *budgets.DescribeBudgetActionsForAccountInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetActionsForAccountOutput, error) {
	i := 0
	if in.NextToken != nil {
		i = 1
	}
	out := &budgets.DescribeBudgetActionsForAccountOutput{}
	for _, name := range f.pages[i] {
		out.Actions = append(out.Actions, btypes.Action{BudgetName: aws.String(name)})
	}
	if i+1 < len(f.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func TestBudgetActionsOutsideCountsOtherBudgetsAcrossPages(t *testing.T) {
	api := &fakeActionPages{pages: [][]string{
		{"guardrail-account-111122223333", "team-budget", "guardrail-ou-ou-ab12-cdef3456"},
		{"finance-cap", "guardrail-account-444455556666", "sandbox"},
	}}
	n, err := BudgetActionsOutside(context.Background(), api, "123456789012", "guardrail-")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("outside = %d, want 3 (team-budget, finance-cap, sandbox)", n)
	}
}
