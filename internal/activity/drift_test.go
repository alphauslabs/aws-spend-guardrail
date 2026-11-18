package activity_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
)

func countPrefix(list []string, prefix string) int {
	n := 0
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

// A cutoff detached by hand is counted as lifted, and the account is watched
// again (fix-cutoff-drift D1, D2).
func TestDetachedCutoffIsLifted(t *testing.T) {
	w := newWorld(t)
	w.caps[acct1] = activity.Caps{Hourly: 5, Enforce: true}
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 6000)
	w.run()
	if !w.policies.Attached["p-ai/"+acct1] {
		t.Fatal("not cut off")
	}
	// An administrator detaches it by hand.
	delete(w.policies.Attached, "p-ai/"+acct1)
	w.now = w.now.Add(6 * time.Minute)
	w.run()
	if cuts, _ := w.store.Cutoffs(context.Background(), "ai"); len(cuts) != 0 {
		t.Fatalf("the detached cutoff must be forgotten: %+v", cuts)
	}
	if w.count("cutoff_lifted ") != 1 || w.count("drift ") != 1 || countPrefix(w.alerts, "guardrail: "+acct1+" cutoff detached by hand") != 1 {
		t.Fatalf("events %v alerts %v", w.events, w.alerts)
	}
	if strings.Contains(strings.Join(w.policies.Calls, ","), "detach") {
		t.Fatalf("the guard must not detach again: %v", w.policies.Calls)
	}
	// A new breach cuts it off again with a new id.
	w.call(acct1, "us-east-1", "m", w.now, 1000)
	w.now = w.now.Add(time.Minute)
	w.run()
	if !w.policies.Attached["p-ai/"+acct1] || w.count("cutoff ") != 2 {
		t.Fatalf("a new breach must cut off again: %v", w.events)
	}
}

// A cutoff just attached isn't taken as detached, as AWS's listing can lag.
func TestJustAttachedCutoffIsTrusted(t *testing.T) {
	w := newWorld(t)
	w.caps[acct1] = activity.Caps{Hourly: 5, Enforce: true}
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 6000)
	w.run()
	delete(w.policies.Attached, "p-ai/"+acct1) // not listed yet
	w.now = w.now.Add(time.Minute)
	w.run()
	if cuts, _ := w.store.Cutoffs(context.Background(), "ai"); len(cuts) != 1 {
		t.Fatalf("a cutoff under %v old must be kept: %+v", activity.DetachedGrace, cuts)
	}
}

// A failed read forgets nothing; with no cutoffs held, nothing is read.
func TestDetachedCheckReadsOnlyWhenNeeded(t *testing.T) {
	w := newWorld(t)
	w.run()
	if w.policies.Lists != 0 {
		t.Fatalf("no cutoffs held, but read %d times", w.policies.Lists)
	}
	w.caps[acct1] = activity.Caps{Hourly: 5, Enforce: true}
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 6000)
	w.run()
	w.policies.ListErr = errors.New("ThrottlingException")
	delete(w.policies.Attached, "p-ai/"+acct1)
	w.now = w.now.Add(10 * time.Minute)
	w.run()
	if cuts, _ := w.store.Cutoffs(context.Background(), "ai"); len(cuts) != 1 || w.count("cutoff_lifted ") != 0 {
		t.Fatalf("a failed read must forget nothing: %+v %v", cuts, w.events)
	}
}
