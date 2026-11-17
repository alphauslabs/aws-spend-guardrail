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
