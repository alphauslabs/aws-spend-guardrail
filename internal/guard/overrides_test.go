package guard

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// newModel is a model only shared prices list, so the test sets its known
// price.
const newModel = "openai.gpt-test-new"

func aiPricesDoc(prices string) string {
	return `{"payer_account_id":"123456789012","scopes":[],"ai":{"prices":` + prices + `}}`
}

func lowAlerts(h *harness) int {
	n := 0
	for _, a := range h.alerts.list {
		if strings.HasPrefix(a, "guardrail: config price below the known price") {
			n++
		}
	}
	return n
}

// inForce publishes a config whose ai.prices entries are new (a loosening),
// approves it, and pulls past the delay.
func inForce(h *harness, doc string) {
	h.t.Helper()
	v := h.publish(doc)
	h.pull()
	approveAndWait(h, strconv.FormatInt(v, 10), time.Hour)
	h.pull()
	if h.st().AppliedVersion != v {
		h.t.Fatalf("version %d not in force: %+v", v, h.st())
	}
}

// nextDay moves a day on, pulls, and runs the daily check.
func nextDay(h *harness) {
	h.t.Helper()
	h.now = h.now.Add(24 * time.Hour)
	h.pull()
	h.daily()
}

func TestLowConfigPriceReportedOnce(t *testing.T) {
	h := newHarness(t)
	withFast(t, h)
	src := &fakePrices{}
	h.g.Prices = src
	inForce(h, aiPricesDoc(`{"`+newModel+`":{"input_per_1k":0.0022,"output_per_1k":0.011}}`))
	src.publish(t, 12, map[string]config.ModelPrice{newModel: {InputPer1K: 0.0022, OutputPer1K: 0.011}}, false)
	nextDay(h)
	if lowAlerts(h) != 0 {
		t.Fatalf("an entry equal to the known price (below only the floor) was reported: %v", h.alerts.list)
	}

	// AWS raises the input price; shared prices follow.
	src.publish(t, 13, map[string]config.ModelPrice{newModel: {InputPer1K: 0.0025, OutputPer1K: 0.011}}, false)
	nextDay(h)
	if lowAlerts(h) != 1 || h.events.count(schema.KindPriceOverrideLow) != 1 {
		t.Fatalf("alerts %v events %v", h.alerts.list, h.events.kinds())
	}
	if !h.alerts.has("prices "+newModel+" at 0.0022 USD input and 0.011 USD output per 1,000 tokens in ai.prices") || !h.alerts.has("for it: 0.0025 USD input") {
		t.Fatalf("alert lacks the prices: %v", h.alerts.list)
	}
	var d struct {
		Model string
		Entry struct {
			In float64 `json:"input_per_1k"`
		}
		Known struct {
			In float64 `json:"input_per_1k"`
		}
	}
	for _, e := range h.events.list {
		if e.Kind == schema.KindPriceOverrideLow {
			_ = json.Unmarshal(e.Detail, &d)
		}
	}
	if d.Model != newModel || d.Entry.In != 0.0022 || d.Known.In != 0.0025 {
		t.Fatalf("event detail %+v", d)
	}

	// The next day, nothing changed: nothing new.
	nextDay(h)
	if lowAlerts(h) != 1 {
		t.Fatalf("reported again with no change: %v", h.alerts.list)
	}

	// The known price rises again: reported with the new price.
	src.publish(t, 14, map[string]config.ModelPrice{newModel: {InputPer1K: 0.003, OutputPer1K: 0.011}}, false)
	nextDay(h)
	if lowAlerts(h) != 2 || !h.alerts.has("for it: 0.003 USD input") {
		t.Fatalf("a new known price wasn't reported: %v", h.alerts.list)
	}

	// The known price falls back to the entry: forgotten without an alert.
	src.publish(t, 15, map[string]config.ModelPrice{newModel: {InputPer1K: 0.0022, OutputPer1K: 0.011}}, false)
	nextDay(h)
	if lowAlerts(h) != 2 {
		t.Fatalf("a no-longer-low entry alerted: %v", h.alerts.list)
	}
	if r, _ := h.state.Reported(t.Context(), reportedPriceOverrideLow); len(r) != 0 {
		t.Fatalf("records not cleared: %v", r)
	}

	// Low again at the same prices as before: reported again, as a new event.
	src.publish(t, 16, map[string]config.ModelPrice{newModel: {InputPer1K: 0.003, OutputPer1K: 0.011}}, false)
	nextDay(h)
	if lowAlerts(h) != 3 || h.events.count(schema.KindPriceOverrideLow) != 3 {
		t.Fatalf("a return to low wasn't reported: alerts %v events %v", h.alerts.list, h.events.kinds())
	}

	// The org raises its entry, which applies at once: nothing more.
	v := h.publish(aiPricesDoc(`{"` + newModel + `":{"input_per_1k":0.003,"output_per_1k":0.011}}`))
	nextDay(h)
	if h.st().AppliedVersion != v {
		t.Fatalf("a raised price must apply at once: %+v", h.st())
	}
	if lowAlerts(h) != 3 {
		t.Fatalf("a raised entry alerted: %v", h.alerts.list)
	}
	if r, _ := h.state.Reported(t.Context(), reportedPriceOverrideLow); len(r) != 0 {
		t.Fatalf("records not cleared after the raise: %v", r)
	}
}

func TestConfigPriceNotReported(t *testing.T) {
	h := newHarness(t)
	withFast(t, h)
	src := &fakePrices{}
	h.g.Prices = src
	inForce(h, aiPricesDoc(`{"`+newModel+`":{"input_per_1k":0.0022,"output_per_1k":0.011},"xai.not-listed":{"input_per_1k":0.0001,"output_per_1k":0.0001}}`))
	// Below the known price only in eu-west-2.
	src.publishRegions(t, 12, map[string]map[string]config.ModelPrice{
		"us-east-1": {newModel: {InputPer1K: 0.0022, OutputPer1K: 0.011}},
		"eu-west-2": {newModel: {InputPer1K: 0.0024, OutputPer1K: 0.0121}},
	})
	nextDay(h)
	if lowAlerts(h) != 0 || h.events.count(schema.KindPriceOverrideLow) != 0 {
		t.Fatalf("reported: %v", h.alerts.list)
	}
}

func TestPriceCheckNeedsTheAIGuard(t *testing.T) {
	h := newHarness(t)
	inForce(h, aiPricesDoc(`{"anthropic.claude-opus-5-5":{"input_per_1k":0.000001,"output_per_1k":0.000001}}`))
	nextDay(h)
	if lowAlerts(h) != 0 {
		t.Fatalf("checked with the AI guard off: %v", h.alerts.list)
	}
}
