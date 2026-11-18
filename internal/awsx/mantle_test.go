package awsx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/oam"
	oamtypes "github.com/aws/aws-sdk-go-v2/service/oam/types"
)

type fakeCW struct {
	pages []*cloudwatch.GetMetricDataOutput
	calls int
	in    *cloudwatch.GetMetricDataInput
}

func (f *fakeCW) GetMetricData(_ context.Context, in *cloudwatch.GetMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	f.in = in
	p := f.pages[f.calls]
	f.calls++
	return p, nil
}

func series(label string, at time.Time, v float64) cwtypes.MetricDataResult {
	return cwtypes.MetricDataResult{Id: aws.String("mantle"), Label: aws.String(label), StatusCode: cwtypes.StatusCodeComplete, Timestamps: []time.Time{at}, Values: []float64{v}}
}

// The labeled search, as observed on 2026-11-17, becomes points per account,
// model, and minute, across pages.
func TestMantleMetricsUsage(t *testing.T) {
	at := time.Date(2026, 11, 17, 18, 19, 0, 0, time.UTC)
	f := &fakeCW{pages: []*cloudwatch.GetMetricDataOutput{
		{MetricDataResults: []cwtypes.MetricDataResult{
			series("730335604511|openai.gpt-oss-20b|Inferences", at, 2),
			series("730335604511|openai.gpt-oss-20b|TotalInputTokens", at, 140),
		}, NextToken: aws.String("p2")},
		{MetricDataResults: []cwtypes.MetricDataResult{
			series("730335604511|openai.gpt-oss-20b|TotalOutputTokens", at, 117),
			series("|google.gemma-3-4b-it|TotalInputTokens", at, 12), // no account: skipped
		}},
	}}
	pts, err := (&MantleMetrics{API: f}).Usage(context.Background(), at.Add(-15*time.Minute), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || !strings.Contains(aws.ToString(f.in.MetricDataQueries[0].Expression), "{AWS/BedrockMantle,Model}") || aws.ToInt32(f.in.MetricDataQueries[0].Period) != 60 {
		t.Fatalf("calls %d, query %+v", f.calls, f.in.MetricDataQueries[0])
	}
	if len(pts) != 1 || pts[0].Account != "730335604511" || pts[0].Model != "openai.gpt-oss-20b" || pts[0].Calls != 2 || pts[0].In != 140 || pts[0].Out != 117 || !pts[0].Minute.Equal(at) {
		t.Fatalf("points %+v", pts)
	}
	// A forbidden result, as before a link takes effect, is an error.
	f = &fakeCW{pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{{Id: aws.String("mantle"), StatusCode: cwtypes.StatusCodeForbidden}}}}}
	if _, err := (&MantleMetrics{API: f}).Usage(context.Background(), at, at.Add(time.Minute)); err == nil {
		t.Fatal("a forbidden result must be an error")
	}
}

type fakeOAM struct {
	sinks []oamtypes.ListSinksItem
	links []oamtypes.ListAttachedLinksItem
}

func (f *fakeOAM) ListSinks(context.Context, *oam.ListSinksInput, ...func(*oam.Options)) (*oam.ListSinksOutput, error) {
	return &oam.ListSinksOutput{Items: f.sinks}, nil
}

func (f *fakeOAM) ListAttachedLinks(context.Context, *oam.ListAttachedLinksInput, ...func(*oam.Options)) (*oam.ListAttachedLinksOutput, error) {
	return &oam.ListAttachedLinksOutput{Items: f.links}, nil
}

// Without a sink the search returns nothing instead of failing, so the
// reader checks for one first (found in the live check of 2026-11-18).
func TestMantleMetricsNeedsASink(t *testing.T) {
	cw := &fakeCW{pages: []*cloudwatch.GetMetricDataOutput{{}}}
	at := time.Date(2026, 11, 18, 13, 0, 0, 0, time.UTC)
	if _, err := (&MantleMetrics{API: cw, Sinks: &fakeOAM{}}).Usage(context.Background(), at, at.Add(time.Minute)); !errors.Is(err, ErrNoSink) || cw.calls != 0 {
		t.Fatalf("no sink: %v, %d metric reads", err, cw.calls)
	}
	withSink := &fakeOAM{sinks: []oamtypes.ListSinksItem{{Arn: aws.String("arn:aws:oam:us-east-1:339712710400:sink/s")}}}
	if _, err := (&MantleMetrics{API: cw, Sinks: withSink}).Usage(context.Background(), at, at.Add(time.Minute)); err != nil || cw.calls != 1 {
		t.Fatalf("with a sink: %v, %d metric reads", err, cw.calls)
	}
}

func TestMantleLinks(t *testing.T) {
	sink, linked, err := MantleLinks(context.Background(), &fakeOAM{})
	if err != nil || sink != "" || linked != nil {
		t.Fatalf("no sink: %q %v %v", sink, linked, err)
	}
	f := &fakeOAM{
		sinks: []oamtypes.ListSinksItem{{Arn: aws.String("arn:aws:oam:us-east-1:339712710400:sink/dfe6")}},
		links: []oamtypes.ListAttachedLinksItem{{LinkArn: aws.String("arn:aws:oam:us-east-1:730335604511:link/f1a8")}},
	}
	sink, linked, err = MantleLinks(context.Background(), f)
	if err != nil || !strings.HasSuffix(sink, "sink/dfe6") || len(linked) != 1 || !linked["730335604511"] {
		t.Fatalf("%q %v %v", sink, linked, err)
	}
}
