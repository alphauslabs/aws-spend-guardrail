package guard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

func (h *harness) publishNote(doc, note string) int64 {
	h.t.Helper()
	h.nextVer++
	err := h.client.InsertVersion(context.Background(), schema.ConfigDatasetID(msp, payer), jennah.Version{
		Number: h.nextVer, Document: json.RawMessage(doc), PublishedAt: h.now, Note: note,
	}, "")
	if err != nil {
		h.t.Fatal(err)
	}
	return h.nextVer
}

// raised publishes version 1 as the baseline, then a loosening version 2 with
// the given note, and returns that version's approval.
func raised(t *testing.T, note string) (*harness, string, string) {
	h := newHarness(t)
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publishNote(doc("50000", "80000"), note)
	h.pull()
	a := h.jennah.Approval("guardrail:123456789012:msp_a:v2")
	if a == nil {
		t.Fatal("no approval raised")
	}
	return h, a.GetSummary(), a.GetPayload().GetFields()["reason"].GetStringValue()
}

func TestLooseningApprovalShowsTheReason(t *testing.T) {
	const note = "Month-end batch, on trend for 6 months; forecast reaches the ceiling tomorrow"
	h, summary, payload := raised(t, note)
	if !strings.Contains(summary, "Reason given:\n"+note) {
		t.Fatalf("summary lacks the reason:\n%s", summary)
	}
	if payload != note {
		t.Fatalf("payload reason = %q", payload)
	}
	var detail map[string]any
	for _, e := range h.events.list {
		if e.Kind == schema.KindLooseningRaised {
			_ = json.Unmarshal(e.Detail, &detail)
		}
	}
	if detail["reason"] != note {
		t.Fatalf("loosening_raised event reason = %v", detail["reason"])
	}
	if h.st().Pending.Note != note {
		t.Fatalf("pending note = %q", h.st().Pending.Note)
	}
}

func TestLooseningApprovalWithoutAReason(t *testing.T) {
	_, summary, payload := raised(t, "  ")
	if !strings.Contains(summary, "Reason given:\nNo reason was given.") || payload != "" {
		t.Fatalf("summary:\n%s\npayload %q", summary, payload)
	}
}

func TestLongReasonIsShortened(t *testing.T) {
	_, summary, _ := raised(t, strings.Repeat("x", 10000))
	if !strings.Contains(summary, "[shortened; the full note is on config version 2]") {
		t.Fatalf("no shortening line:\n%.200s", summary)
	}
	if n := utf8.RuneCountInString(summary); n > 16000 || strings.Count(summary, "x") != MaxReasonLen {
		t.Fatalf("summary is %d characters with %d of the note", n, strings.Count(summary, "x"))
	}
}
