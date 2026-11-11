package guard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

type fakeKey struct {
	info jennah.KeyInfo
	err  error
}

func (f *fakeKey) CallingKey(context.Context) (jennah.KeyInfo, error) { return f.info, f.err }

func withKeys(t *testing.T) (*harness, *fakeKey, *fakeKey) {
	h := newHarness(t)
	reader := &fakeKey{info: jennah.KeyInfo{Known: true, ID: "k-r1", Label: "guardrail-msp_a-123456789012-reader", ExpiresAt: h.now.Add(365 * 24 * time.Hour)}}
	writer := &fakeKey{info: jennah.KeyInfo{Known: true, ID: "k-w1", Label: "guardrail-msp_a-123456789012-writer"}} // never expires
	h.g.Keys = []GuardKey{{Name: "reader", Key: reader}, {Name: "writer", Key: writer}}
	return h, reader, writer
}

func (h *harness) daily() {
	h.t.Helper()
	if err := h.g.Daily(context.Background()); err != nil {
		h.t.Fatalf("daily: %v", err)
	}
}

func (h *harness) expiryAlerts() int {
	n := 0
	for _, a := range h.alerts.list {
		if strings.Contains(a, "key expires") {
			n++
		}
	}
	return n
}

func (h *harness) heartbeatKeys() []map[string]any {
	h.t.Helper()
	var last jennah.Event
	for _, e := range h.events.list {
		if e.Kind == schema.KindHeartbeat {
			last = e
		}
	}
	var d struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(last.Detail, &d); err != nil {
		h.t.Fatal(err)
	}
	return d.Keys
}

func TestHeartbeatRecordsKeys(t *testing.T) {
	h, reader, _ := withKeys(t)
	h.daily()
	keys := h.heartbeatKeys()
	if len(keys) != 2 || keys[0]["name"] != "reader" || keys[0]["expires_at"] != reader.info.ExpiresAt.Format(time.RFC3339) ||
		keys[1]["name"] != "writer" || keys[1]["expires_at"] != "never" || keys[1]["label"] != "guardrail-msp_a-123456789012-writer" {
		t.Fatalf("heartbeat keys: %v", keys)
	}
	if h.expiryAlerts() != 0 {
		t.Fatalf("no warning a year out: %v", h.alerts.list)
	}
}

func TestThirtyDayWarningIsSentOnce(t *testing.T) {
	h, reader, _ := withKeys(t)
	reader.info.ExpiresAt = h.now.Add(29 * 24 * time.Hour)
	h.daily()
	if h.expiryAlerts() != 1 || !h.alerts.has("guardrail: reader key expires in 29 day(s)") || !h.alerts.has("Rotate a key before it expires") {
		t.Fatalf("alerts: %v", h.alerts.list)
	}
	h.now = h.now.Add(24 * time.Hour)
	h.daily()
	if h.expiryAlerts() != 1 {
		t.Fatalf("the next day sends nothing new: %v", h.alerts.list)
	}
}

func TestMissedThresholdsSendOneAlert(t *testing.T) {
	h, reader, _ := withKeys(t)
	reader.info.ExpiresAt = h.now.Add(6 * 24 * time.Hour)
	h.daily()
	if h.expiryAlerts() != 1 || h.st().KeyWarnings["reader"].WarnedDays != 7 {
		t.Fatalf("want one 7-day alert, got %v, state %+v", h.alerts.list, h.st().KeyWarnings)
	}
	h.now = h.now.Add(4 * 24 * time.Hour) // 2 days left: the 3-day threshold
	h.daily()
	if h.expiryAlerts() != 2 || h.st().KeyWarnings["reader"].WarnedDays != 3 {
		t.Fatalf("want the 3-day alert, got %v", h.alerts.list)
	}
}

func TestRotatedKeyStartsOver(t *testing.T) {
	h, reader, _ := withKeys(t)
	reader.info.ExpiresAt = h.now.Add(13 * 24 * time.Hour)
	h.daily()
	reader.info = jennah.KeyInfo{Known: true, ID: "k-r2", Label: reader.info.Label, ExpiresAt: h.now.Add(365 * 24 * time.Hour)}
	h.now = h.now.Add(24 * time.Hour)
	h.daily()
	if h.expiryAlerts() != 1 {
		t.Fatalf("the rotated key must not warn: %v", h.alerts.list)
	}
	if w := h.st().KeyWarnings["reader"]; w.KeyID != "k-r2" || w.WarnedDays != 0 {
		t.Fatalf("warning state not reset: %+v", w)
	}
	h.now = reader.info.ExpiresAt.Add(-29 * 24 * time.Hour)
	h.daily()
	if h.expiryAlerts() != 2 {
		t.Fatalf("the new key warns 30 days before its own expiry: %v", h.alerts.list)
	}
}

func TestUnknownAndNeverDontWarn(t *testing.T) {
	h, reader, writer := withKeys(t)
	reader.info, reader.err = jennah.KeyInfo{}, errors.New("unavailable")
	writer.info = jennah.KeyInfo{} // Jennah answered without the key
	h.daily()
	keys := h.heartbeatKeys()
	if keys[0]["expires_at"] != "unknown" || keys[1]["expires_at"] != "unknown" || h.expiryAlerts() != 0 {
		t.Fatalf("keys %v alerts %v", keys, h.alerts.list)
	}
	if h.events.count(schema.KindHeartbeat) != 1 {
		t.Fatal("a failed key check must not stop the daily run")
	}
}
