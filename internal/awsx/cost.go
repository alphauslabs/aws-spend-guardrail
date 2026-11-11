package awsx

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

// CostAPI is the part of the Cost Explorer client the guard uses. Each call is
// billed (0.01 USD at the time of writing), so callers keep them few.
type CostAPI interface {
	GetCostAndUsage(context.Context, *costexplorer.GetCostAndUsageInput, ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

// CostExplorer implements guard.SpendSource. AWS Budgets zeroes a budget's
// calculated spend whenever the budget is created or updated, until AWS
// recalculates; Cost Explorer keeps reporting it, so the guard reads spend
// here when the budget's figure can't be trusted (fix-budget-spend-reset).
type CostExplorer struct {
	API CostAPI
}

const ceDate = "2006-01-02"

// Actual returns the unblended cost of the given linked accounts from the
// start of from's day to the end of to's day. No record type is filtered out,
// which matches a budget's default cost types (credits and refunds included).
func (c *CostExplorer) Actual(ctx context.Context, accounts []string, from, to time.Time) (float64, error) {
	if len(accounts) == 0 {
		return 0, nil
	}
	in := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(from.UTC().Format(ceDate)),
			End:   aws.String(to.UTC().AddDate(0, 0, 1).Format(ceDate)), // End is exclusive
		},
		Granularity: cetypes.GranularityMonthly,
		Metrics:     []string{"UnblendedCost"},
		Filter: &cetypes.Expression{Dimensions: &cetypes.DimensionValues{
			Key:    cetypes.DimensionLinkedAccount,
			Values: accounts,
		}},
	}
	var total float64
	for {
		out, err := c.API.GetCostAndUsage(ctx, in)
		if err != nil {
			return 0, fmt.Errorf("cost explorer: %w", err)
		}
		for _, r := range out.ResultsByTime {
			m, ok := r.Total["UnblendedCost"]
			if !ok || m.Amount == nil {
				continue
			}
			v, err := strconv.ParseFloat(aws.ToString(m.Amount), 64)
			if err != nil {
				return 0, fmt.Errorf("cost explorer amount %q: %w", aws.ToString(m.Amount), err)
			}
			total += v
		}
		if out.NextPageToken == nil {
			return total, nil
		}
		in.NextPageToken = out.NextPageToken
	}
}

var _ guard.SpendSource = (*CostExplorer)(nil)
