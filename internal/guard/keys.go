package guard

import (
	"context"
	"fmt"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
)

// KeyReader describes the Jennah API key one of the guard's clients uses.
type KeyReader interface {
	CallingKey(ctx context.Context) (jennah.KeyInfo, error)
}

// GuardKey is one of the guard's keys, by what it's for ("reader", "writer").
type GuardKey struct {
	Name string
	Key  KeyReader
}

// KeyWarning records the warnings sent for one key, so each threshold is
// alerted once. A different key id or expiry starts over.
type KeyWarning struct {
	KeyID     string    `json:"key_id"`
	ExpiresAt time.Time `json:"expires_at"`
	// WarnedDays is the smallest threshold already alerted; 0 means none.
	WarnedDays int `json:"warned_days,omitempty"`
}

// KeyWarnDays are the days before expiry at which a key is warned about.
var KeyWarnDays = []int{30, 14, 7, 3, 1}

// checkKeys reads each key's expiry, alerts when it crosses a threshold
// (design D2), and returns what the heartbeat records for each key. A failed
// read is recorded as unknown and warns nothing (design D3).
func (g *Guard) checkKeys(ctx context.Context, st *State) []map[string]any {
	if len(g.Keys) == 0 {
		return nil
	}
	if st.KeyWarnings == nil {
		st.KeyWarnings = map[string]KeyWarning{}
	}
	now := g.now()
	var out []map[string]any
	for _, k := range g.Keys {
		info, err := k.Key.CallingKey(ctx)
		rec := map[string]any{"name": k.Name}
		switch {
		case err != nil || !info.Known:
			rec["expires_at"] = "unknown"
			out = append(out, rec)
			continue
		case info.ExpiresAt.IsZero():
			rec["label"], rec["expires_at"] = info.Label, "never"
			delete(st.KeyWarnings, k.Name)
			out = append(out, rec)
			continue
		}
		rec["label"], rec["expires_at"] = info.Label, info.ExpiresAt.Format(time.RFC3339)
		out = append(out, rec)

		w := st.KeyWarnings[k.Name]
		if w.KeyID != info.ID || !w.ExpiresAt.Equal(info.ExpiresAt) {
			w = KeyWarning{KeyID: info.ID, ExpiresAt: info.ExpiresAt} // rotated, or first seen
		}
		left := info.ExpiresAt.Sub(now)
		threshold := 0
		for _, d := range KeyWarnDays {
			if left <= time.Duration(d)*24*time.Hour {
				threshold = d // smallest threshold reached so far
			}
		}
		if threshold > 0 && (w.WarnedDays == 0 || threshold < w.WarnedDays) {
			subject, body := keyExpiryAlert(k.Name, info, left)
			g.alert(ctx, subject, body)
			w.WarnedDays = threshold
		}
		st.KeyWarnings[k.Name] = w
	}
	return out
}

// keyExpiryAlert is the local alert for a key nearing expiry (design D4).
func keyExpiryAlert(name string, info jennah.KeyInfo, left time.Duration) (subject, body string) {
	when := "in less than a day"
	if days := int(left / (24 * time.Hour)); days >= 1 {
		when = fmt.Sprintf("in %d day(s)", days)
	}
	if left <= 0 {
		when = "now"
	}
	label := info.Label
	if label == "" {
		label = info.ID
	}
	subject = fmt.Sprintf("guardrail: %s key expires %s", name, when)
	body = fmt.Sprintf("The guard's Jennah %s key %q expires %s, at %s. When it expires, the guard can't read config or record events: freezes already in force keep working, but nothing new is applied or recorded. Mint a new key, store it in the same secret, and revoke the old one: see \"Rotate a key before it expires\" in docs/operating.md.",
		name, label, when, info.ExpiresAt.Format(time.RFC3339))
	return subject, body
}
