package awsx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/oam"
)

// CloudWatchAPI is the part of CloudWatch the mantle reader uses.
type CloudWatchAPI interface {
	GetMetricData(context.Context, *cloudwatch.GetMetricDataInput, ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error)
}

// mantleSearch is the cross-account search of design D2 of add-mantle-usage:
// every linked account's mantle inferences and tokens per model and minute,
// labeled with the account, the model, and the metric.
const (
	mantleSearch = `SEARCH('{AWS/BedrockMantle,Model} MetricName=("Inferences" OR "TotalInputTokens" OR "TotalOutputTokens")', 'Sum', 60)`
	mantleLabel  = `${PROP('AccountId')}|${PROP('Dim.Model')}|${PROP('MetricName')}`
)

// MantleMetrics reads one region's mantle usage from the payer account, which
// is the monitoring account of a CloudWatch cross-account observability sink.
type MantleMetrics struct {
	API CloudWatchAPI
	// Sinks, when set, is checked before each read: without a sink the search
	// returns no series instead of failing, which would look like no use
	// (add-mantle-usage D5, found in the live check).
	Sinks OAMAPI
}

// ErrNoSink is returned when the region has no observability sink.
var ErrNoSink = errors.New("no CloudWatch observability sink in the payer account in this region, so member accounts share no mantle usage")

// Usage returns each account, model, and minute's inferences and tokens in
// [from, to).
func (m *MantleMetrics) Usage(ctx context.Context, from, to time.Time) ([]activity.MetricPoint, error) {
	if m.Sinks != nil {
		sinks, err := m.Sinks.ListSinks(ctx, &oam.ListSinksInput{})
		if err != nil {
			return nil, fmt.Errorf("list observability sinks: %w", err)
		}
		if len(sinks.Items) == 0 {
			return nil, ErrNoSink
		}
	}
	in := &cloudwatch.GetMetricDataInput{
		StartTime: aws.Time(from), EndTime: aws.Time(to), ScanBy: cwtypes.ScanByTimestampAscending,
		MetricDataQueries: []cwtypes.MetricDataQuery{{Id: aws.String("mantle"), Expression: aws.String(mantleSearch), Label: aws.String(mantleLabel), Period: aws.Int32(60)}},
	}
	points := map[string]*activity.MetricPoint{}
	var order []string
	for {
		out, err := m.API.GetMetricData(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("read mantle metrics: %w", err)
		}
		for _, msg := range out.Messages {
			if code := aws.ToString(msg.Code); code != "" && !strings.EqualFold(code, "PartialData") {
				return nil, fmt.Errorf("read mantle metrics: %s: %s", code, aws.ToString(msg.Value))
			}
		}
		for _, r := range out.MetricDataResults {
			if r.StatusCode == cwtypes.StatusCodeForbidden || r.StatusCode == cwtypes.StatusCodeInternalError {
				return nil, fmt.Errorf("read mantle metrics: %s", r.StatusCode)
			}
			parts := strings.Split(aws.ToString(r.Label), "|")
			if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
				continue
			}
			for i, ts := range r.Timestamps {
				if i >= len(r.Values) {
					break
				}
				minute := ts.UTC().Truncate(time.Minute)
				k := minute.Format(time.RFC3339) + "|" + parts[0] + "|" + parts[1]
				p := points[k]
				if p == nil {
					p = &activity.MetricPoint{Account: parts[0], Model: parts[1], Minute: minute}
					points[k] = p
					order = append(order, k)
				}
				v := int64(r.Values[i] + 0.5)
				switch parts[2] {
				case "Inferences":
					p.Calls = v
				case "TotalInputTokens":
					p.In = v
				case "TotalOutputTokens":
					p.Out = v
				}
			}
		}
		if out.NextToken == nil {
			break
		}
		in.NextToken = out.NextToken
	}
	res := make([]activity.MetricPoint, 0, len(order))
	for _, k := range order {
		res = append(res, *points[k])
	}
	return res, nil
}

// OAMAPI is the part of CloudWatch Observability Access Manager the guard uses.
type OAMAPI interface {
	ListSinks(context.Context, *oam.ListSinksInput, ...func(*oam.Options)) (*oam.ListSinksOutput, error)
	ListAttachedLinks(context.Context, *oam.ListAttachedLinksInput, ...func(*oam.Options)) (*oam.ListAttachedLinksOutput, error)
}

// MantleLinks reports the account's sink in one region and the accounts
// linked to it (add-mantle-usage D6). sink is "" when there is none.
func MantleLinks(ctx context.Context, api OAMAPI) (sink string, linked map[string]bool, err error) {
	sinks, err := api.ListSinks(ctx, &oam.ListSinksInput{})
	if err != nil {
		return "", nil, fmt.Errorf("list observability sinks: %w", err)
	}
	if len(sinks.Items) == 0 {
		return "", nil, nil
	}
	sink = aws.ToString(sinks.Items[0].Arn)
	linked = map[string]bool{}
	in := &oam.ListAttachedLinksInput{SinkIdentifier: aws.String(sink)}
	for {
		out, err := api.ListAttachedLinks(ctx, in)
		if err != nil {
			return sink, nil, fmt.Errorf("list links to %s: %w", sink, err)
		}
		for _, l := range out.Items {
			// arn:aws:oam:<region>:<account>:link/<id>
			if parts := strings.Split(aws.ToString(l.LinkArn), ":"); len(parts) > 4 && parts[4] != "" {
				linked[parts[4]] = true
			}
		}
		if out.NextToken == nil {
			break
		}
		in.NextToken = out.NextToken
	}
	return sink, linked, nil
}
