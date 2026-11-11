package awsx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

type fakeCost struct {
	in    []*costexplorer.GetCostAndUsageInput
	pages []*costexplorer.GetCostAndUsageOutput
	err   error
}

func (f *fakeCost) GetCostAndUsage(_ context.Context, in *costexplorer.GetCostAndUsageInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	c := *in
	f.in = append(f.in, &c)
	if f.err != nil {
		return nil, f.err
	}
	p := f.pages[0]
	f.pages = f.pages[1:]
	return p, nil
}

func result(amount string) cetypes.ResultByTime {
	return cetypes.ResultByTime{Total: map[string]cetypes.MetricValue{"UnblendedCost": {Amount: aws.String(amount), Unit: aws.String("USD")}}}
}

func TestActualSumsThePeriodForTheAccounts(t *testing.T) {
	f := &fakeCost{pages: []*costexplorer.GetCostAndUsageOutput{
		{ResultsByTime: []cetypes.ResultByTime{result("3.20")}, NextPageToken: aws.String("p2")},
		{ResultsByTime: []cetypes.ResultByTime{result("2.54")}},
	}}
	c := &CostExplorer{API: f}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 11, 21, 15, 0, 0, time.UTC)
	got, err := c.Actual(context.Background(), []string{"111122223333", "444455556666"}, from, to)
	if err != nil || got < 5.739 || got > 5.741 {
		t.Fatalf("got %v %v", got, err)
	}
	in := f.in[0]
	if aws.ToString(in.TimePeriod.Start) != "2026-10-01" || aws.ToString(in.TimePeriod.End) != "2026-11-12" {
		t.Fatalf("period %s to %s", aws.ToString(in.TimePeriod.Start), aws.ToString(in.TimePeriod.End))
	}
	if in.Granularity != cetypes.GranularityMonthly || len(in.Metrics) != 1 || in.Metrics[0] != "UnblendedCost" {
		t.Fatalf("granularity %v metrics %v", in.Granularity, in.Metrics)
	}
	d := in.Filter.Dimensions
	if d.Key != cetypes.DimensionLinkedAccount || len(d.Values) != 2 {
		t.Fatalf("filter %+v", d)
	}
	if aws.ToString(f.in[1].NextPageToken) != "p2" {
		t.Fatal("second page not requested")
	}
}

func TestActualWithoutAccountsCallsNothing(t *testing.T) {
	f := &fakeCost{}
	got, err := (&CostExplorer{API: f}).Actual(context.Background(), nil, time.Now(), time.Now())
	if err != nil || got != 0 || len(f.in) != 0 {
		t.Fatalf("got %v %v, %d calls", got, err, len(f.in))
	}
}

func TestActualReportsErrors(t *testing.T) {
	f := &fakeCost{err: errors.New("throttled")}
	if _, err := (&CostExplorer{API: f}).Actual(context.Background(), []string{"111122223333"}, time.Now(), time.Now()); err == nil {
		t.Fatal("want an error")
	}
}
