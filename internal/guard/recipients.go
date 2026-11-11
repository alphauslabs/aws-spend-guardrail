package guard

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// Subscription is one email subscription on the local alert topic.
type Subscription struct {
	Address string
	ARN     string // empty while pending
	Pending bool   // awaiting the recipient's confirmation
}

// AlertSubscriptions manages email subscriptions on the stack's local alert
// topic, and no other topic.
type AlertSubscriptions interface {
	// List returns the topic's email subscriptions.
	List(ctx context.Context) ([]Subscription, error)
	// Subscribe asks SNS to subscribe an address, which sends it a
	// confirmation email.
	Subscribe(ctx context.Context, address string) error
	// Unsubscribe removes a confirmed subscription.
	Unsubscribe(ctx context.Context, arn string) error
}

// OwnSubscription is a subscription the guard asked for. Only these are ever
// unsubscribed, so the stack's subscription and any made by hand are safe.
type OwnSubscription struct {
	// AskedAt is when the guard last asked SNS to subscribe the address.
	AskedAt time.Time `json:"asked_at"`
}

// ReaskAfter is how long the guard waits before asking SNS again for an
// address whose confirmation is still pending. Each ask sends an email.
const ReaskAfter = 24 * time.Hour

// syncRecipients keeps the alert topic's email subscriptions in line with the
// recipients in force (design D3, D4). It only unsubscribes subscriptions it
// made, never the stack's AlertEmail, and it changes nothing when the
// recipients haven't changed.
func (g *Guard) syncRecipients(ctx context.Context, st *State) error {
	if g.Subs == nil {
		return nil
	}
	var want []string
	if st.Effective != nil {
		want = st.Effective.AlertEmails
	}
	if len(want) == 0 && len(st.OwnSubscriptions) == 0 {
		return nil // nothing to manage; skip the SNS call
	}
	subs, err := g.Subs.List(ctx)
	if err != nil {
		return fmt.Errorf("list alert subscriptions: %w", err)
	}
	have := map[string]Subscription{}
	for _, s := range subs {
		addr := strings.ToLower(s.Address)
		// A confirmed subscription wins over a pending one for the same
		// address.
		if cur, ok := have[addr]; ok && !cur.Pending {
			continue
		}
		s.Address = addr
		have[addr] = s
	}
	if st.OwnSubscriptions == nil {
		st.OwnSubscriptions = map[string]OwnSubscription{}
	}
	stack := strings.ToLower(g.S.AlertEmail)
	now := g.now()
	version := st.EffectiveVersion
	wanted := map[string]bool{}
	var errs []error

	for _, addr := range want {
		wanted[addr] = true
		if addr == stack {
			continue // the stack owns this one
		}
		own, mine := st.OwnSubscriptions[addr]
		cur, exists := have[addr]
		switch {
		case exists && !cur.Pending:
			continue // confirmed; whoever made it, there's nothing to do
		case exists && !mine:
			continue // someone else's pending subscription
		case mine && now.Sub(own.AskedAt) < ReaskAfter:
			continue // asked recently; don't send another email yet
		}
		if mine {
			// Asked a day or more ago and still not confirmed (pending, or
			// dropped by SNS for lack of confirmation). This is the daily
			// report.
			g.emit(ctx, schema.KindRecipientUnconfirmed, fmt.Sprintf("%s:%s:%d", schema.KindRecipientUnconfirmed, addr, now.Unix()), "", version,
				map[string]any{"address": addr, "asked_at": own.AskedAt.Format(time.RFC3339)})
		}
		if err := g.Subs.Subscribe(ctx, addr); err != nil {
			errs = append(errs, fmt.Errorf("subscribe %s: %w", addr, err))
			continue
		}
		own.AskedAt = now
		st.OwnSubscriptions[addr] = own
		g.emit(ctx, schema.KindRecipientSubscribed, fmt.Sprintf("%s:%s:%d", schema.KindRecipientSubscribed, addr, now.Unix()), "", version,
			map[string]any{"address": addr})
	}

	var mine []string
	for addr := range st.OwnSubscriptions {
		mine = append(mine, addr)
	}
	sort.Strings(mine)
	for _, addr := range mine {
		if wanted[addr] && addr != stack {
			continue
		}
		cur, exists := have[addr]
		switch {
		case addr == stack:
			// The stack's address is never unsubscribed; stop tracking it.
		case !exists:
			// Already gone (removed by hand, or never confirmed and dropped
			// by SNS).
		case cur.Pending:
			// Can't be removed while pending. Keep tracking it so that if the
			// old link is still confirmed, the next run removes it.
			continue
		default:
			if err := g.Subs.Unsubscribe(ctx, cur.ARN); err != nil {
				errs = append(errs, fmt.Errorf("unsubscribe %s: %w", addr, err))
				continue
			}
			g.emit(ctx, schema.KindRecipientRemoved, fmt.Sprintf("%s:%s:%d", schema.KindRecipientRemoved, addr, now.Unix()), "", version,
				map[string]any{"address": addr})
		}
		delete(st.OwnSubscriptions, addr)
	}
	return errors.Join(errs...)
}
