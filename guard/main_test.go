package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
)

func TestRoute(t *testing.T) {
	cases := map[string]string{
		`{"task":"pull"}`:  routePull,
		`{"task":"daily"}`: routeDaily,
		`{"task":"trail"}`: routeTrail,
		`{"RequestType":"Delete","ResponseURL":"https://example.com"}`:  routeTeardown,
		`{"Records":[{"eventSource":"aws:sqs","body":"{}"}]}`:           routeOutbox,
		`{"Records":[{"EventSource":"aws:sns","Sns":{"Message":"x"}}]}`: routeNotify,
		// An EventBridge event for an account created through CloudTrail.
		`{"version":"0","source":"aws.organizations","detail-type":"AWS API Call via CloudTrail",
		  "detail":{"eventSource":"organizations.amazonaws.com","eventName":"CreateAccount"}}`: routeCoverage,
	}
	for raw, want := range cases {
		got, err := route(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("%s: got %q %v, want %q", raw, got, err, want)
		}
	}
	if _, err := route(json.RawMessage(`{"hello":1}`)); err == nil {
		t.Error("an unknown event must be refused")
	}
}

func TestAINotStartedAlertReadsWell(t *testing.T) {
	if err := alerttext.Check(aiNotStarted(errors.New("find the region of trail bucket b: access denied"))); err != nil {
		t.Fatal(err)
	}
}
