package awsx

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// One search over the day becomes tokens per account and model, across
// pages, skipping series with no tokens (add-tier-pricing D10).
func TestUltrafastTokens(t *testing.T) {
	day := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	f := &fakeCW{pages: []*cloudwatch.GetMetricDataOutput{
		{MetricDataResults: []cwtypes.MetricDataResult{
			series("111122223333|us.openai.gpt-6-astra|InputTokenCount", day, 1200),
			series("111122223333|us.openai.gpt-6-astra|OutputTokenCount", day, 40000),
		}, NextToken: aws.String("p2")},
		{MetricDataResults: []cwtypes.MetricDataResult{
			series("444455556666|openai.gpt-6.1-sol|OutputTokenCount", day, 0),
		}},
	}}
	got, err := Ultrafast(context.Background(), f, day)
	if err != nil || len(got) != 1 || got[0].Account != "111122223333" || got[0].Model != "us.openai.gpt-6-astra" || got[0].In != 1200 || got[0].Out != 40000 {
		t.Fatalf("%+v %v", got, err)
	}
	q := f.in.MetricDataQueries[0]
	if !strings.Contains(aws.ToString(q.Expression), `ResolvedServiceTier="ultrafast"`) || aws.ToInt32(q.Period) != 86400 || !f.in.StartTime.Equal(day) || !f.in.EndTime.Equal(day.Add(24*time.Hour)) {
		t.Fatalf("query %+v", f.in)
	}
	f = &fakeCW{pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{{Id: aws.String("ultrafast"), StatusCode: cwtypes.StatusCodeForbidden}}}}}
	if _, err := Ultrafast(context.Background(), f, day); err == nil {
		t.Fatal("a forbidden read must fail")
	}
}
