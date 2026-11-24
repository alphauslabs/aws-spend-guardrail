package activity_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity/activitytest"
)

// dataEventsFile is testdata/bedrock-data-events.json gzipped as a trail file.
// It holds records from a trail logging Bedrock model data events, captured
// in the test organization on 2026-11-17 and scrubbed: the management and
// data copies of one Converse call, a management-only Converse, and a
// data-only InvokeModelWithBidirectionalStream.
func dataEventsFile(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/bedrock-data-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestParseFileKeepsEventCategory(t *testing.T) {
	recs, err := activity.ParseFile(dataEventsFile(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, cat string }{
		{"Converse", activity.CategoryManagement},
		{"Converse", activity.CategoryData},
		{"Converse", activity.CategoryManagement},
		{"InvokeModelWithBidirectionalStream", activity.CategoryData},
	}
	if len(recs) != len(want) {
		t.Fatalf("got %d records, want %d", len(recs), len(want))
	}
	for i, w := range want {
		if recs[i].EventName != w.name || recs[i].EventCategory != w.cat {
			t.Errorf("record %d: %s %s, want %s %s", i, recs[i].EventName, recs[i].EventCategory, w.name, w.cat)
		}
	}
	if recs[0].InputTokens != 3 || recs[1].InputTokens != 3 || recs[0].OutputTokens != 20 {
		t.Errorf("both copies carry the call's tokens: %+v %+v", recs[0], recs[1])
	}
}

func TestRecordWithoutCategoryIsManagement(t *testing.T) {
	recs, err := activity.ParseFile(activitytest.TrailFile(activitytest.BedrockCall(acct1, "us-east-1", "m", "arn:aws:sts::"+acct1+":assumed-role/app/x", time.Now(), 1, 1, false)))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].EventCategory != activity.CategoryManagement {
		t.Fatalf("records %+v", recs)
	}
}

// tierEventsFile is testdata/bedrock-tier-events.json gzipped as a trail
// file: records of Priority calls through each runtime API, a standard and a
// streamed Chat Completions call, captured in the test organization on
// 2026-11-24 and scrubbed (add-tier-pricing task 1).
func tierEventsFile(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/bedrock-tier-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestParseFileReadsServiceTier(t *testing.T) {
	recs, err := activity.ParseFile(tierEventsFile(t))
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		name, typ, tier string
		in, out         int64
		stream, parent  bool
	}
	wants := []want{
		{"Converse", "AwsApiCall", "priority", 69, 5, false, false},
		{"InvokeModelWithResponseStream", "AwsApiCall", "priority", 0, 0, false, false},
		{"InvokeModel", "AwsApiCall", "priority", 69, 5, false, false},
		{"InvokeModelWithResponseStream", "AwsServiceEvent", "priority", 69, 5, false, true},
		{"ConverseStream", "AwsApiCall", "priority", 0, 0, false, false},
		{"ConverseStream", "AwsServiceEvent", "", 69, 5, false, true},
		{"ChatCompletions", "AwsApiCall", "", 69, 5, false, false},
		{"ChatCompletions", "AwsApiCall", "priority", 69, 5, false, false},
		{"ChatCompletions", "AwsApiCall", "priority", 0, 0, true, false},
		{"InvokeModelWithResponseStream", "AwsServiceEvent", "priority", 0, 0, true, true},
	}
	if len(recs) != len(wants) {
		t.Fatalf("%d records", len(recs))
	}
	for i, w := range wants {
		r := recs[i]
		if r.EventName != w.name || r.EventType != w.typ || r.ServiceTier != w.tier || r.InputTokens != w.in || r.OutputTokens != w.out || r.Stream != w.stream || (r.ParentRequestID != "") != w.parent {
			t.Errorf("record %d: %s %s tier %q tokens %d/%d stream %v parent %q, want %+v", i, r.EventName, r.EventType, r.ServiceTier, r.InputTokens, r.OutputTokens, r.Stream, r.ParentRequestID, w)
		}
	}
	// The ConverseStream completion names its call record's request id.
	if recs[5].ParentRequestID != recs[4].RequestID || recs[4].RequestID == "" {
		t.Errorf("ConverseStream parent %q, call %q", recs[5].ParentRequestID, recs[4].RequestID)
	}
}
