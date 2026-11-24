package awsx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// ultrafastSearch is every linked account's Bedrock runtime tokens served on
// the ultrafast tier, per model, over one period (add-tier-pricing D10).
const (
	ultrafastSearch = `SEARCH('{AWS/Bedrock,ModelId,ResolvedServiceTier,ServiceTier} ResolvedServiceTier="ultrafast" MetricName=("InputTokenCount" OR "OutputTokenCount")', 'Sum', 86400)`
	ultrafastLabel  = `${PROP('AccountId')}|${PROP('Dim.ModelId')}|${PROP('MetricName')}`
)

// Ultrafast returns the ultrafast tokens each linked account used per model
// in [from, from+24h), read through the payer account's observability sink
// with one search query (add-tier-pricing D10).
func Ultrafast(ctx context.Context, api CloudWatchAPI, from time.Time) ([]activity.UltrafastTokens, error) {
	in := &cloudwatch.GetMetricDataInput{
		StartTime: aws.Time(from), EndTime: aws.Time(from.Add(24 * time.Hour)),
		MetricDataQueries: []cwtypes.MetricDataQuery{{Id: aws.String("ultrafast"), Expression: aws.String(ultrafastSearch), Label: aws.String(ultrafastLabel), Period: aws.Int32(86400)}},
	}
	byKey := map[string]*activity.UltrafastTokens{}
	var order []string
	for {
		out, err := api.GetMetricData(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("read ultrafast metrics: %w", err)
		}
		for _, msg := range out.Messages {
			if code := aws.ToString(msg.Code); code != "" && !strings.EqualFold(code, "PartialData") {
				return nil, fmt.Errorf("read ultrafast metrics: %s: %s", code, aws.ToString(msg.Value))
			}
		}
		for _, r := range out.MetricDataResults {
			if r.StatusCode == cwtypes.StatusCodeForbidden || r.StatusCode == cwtypes.StatusCodeInternalError {
				return nil, fmt.Errorf("read ultrafast metrics: %s", r.StatusCode)
			}
			parts := strings.Split(aws.ToString(r.Label), "|")
			if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
				continue
			}
			var sum int64
			for _, v := range r.Values {
				sum += int64(v + 0.5)
			}
			k := parts[0] + "|" + parts[1]
			t := byKey[k]
			if t == nil {
				t = &activity.UltrafastTokens{Account: parts[0], Model: parts[1]}
				byKey[k] = t
				order = append(order, k)
			}
			switch parts[2] {
			case "InputTokenCount":
				t.In += sum
			case "OutputTokenCount":
				t.Out += sum
			}
		}
		if out.NextToken == nil {
			break
		}
		in.NextToken = out.NextToken
	}
	res := make([]activity.UltrafastTokens, 0, len(order))
	for _, k := range order {
		if t := byKey[k]; t.In > 0 || t.Out > 0 {
			res = append(res, *t)
		}
	}
	return res, nil
}
