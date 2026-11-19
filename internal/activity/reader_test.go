package activity_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity/activitytest"
)

// readerWorld drives a Reader directly, one run at a time
// (fair-trail-reading).
type readerWorld struct {
	t      *testing.T
	now    time.Time
	bucket *activitytest.Bucket
	store  *activitytest.Store
	r      *activity.Reader
}

func newReaderWorld(t *testing.T) *readerWorld {
	w := &readerWorld{t: t, now: time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC), bucket: activitytest.NewBucket(), store: activitytest.NewStore()}
	w.restart()
	return w
}

func (w *readerWorld) restart() {
	w.r = &activity.Reader{Bucket: w.bucket, Store: w.store, Base: base}
}

// put writes n files for an account in us-east-1, called a minute apart from
// from, landing now.
func (w *readerWorld) put(account string, from time.Time, n int) {
	for i := 0; i < n; i++ {
		at := from.Add(time.Duration(i) * time.Minute)
		k := activitytest.TrailKey(base, account, "us-east-1", at, fmt.Sprintf("u%d", i))
		w.bucket.Put(k, activitytest.TrailFile(activitytest.BedrockCall(account, "us-east-1", "m", "arn:aws:sts::"+account+":assumed-role/app/x", at, 1, 1, false)), w.now)
	}
}

// read runs ReadNew, commits unless told not to, and returns the records read
// per account, in order.
func (w *readerWorld) read(commit bool) (map[string]int, []string) {
	w.t.Helper()
	recs, err := w.r.ReadNew(context.Background(), w.now)
	if err != nil {
		w.t.Fatalf("read: %v", err)
	}
	if commit {
		if err := w.r.Commit(context.Background()); err != nil {
			w.t.Fatalf("commit: %v", err)
		}
	}
	per := map[string]int{}
	var seq []string
	for _, r := range recs {
		per[r.Account]++
		seq = append(seq, r.Account+"@"+r.EventTime.Format(time.RFC3339))
	}
	if len(recs) > activity.MaxFilesPerTick {
		w.t.Fatalf("read %d files in one run", len(recs))
	}
	return per, seq
}

func TestBacklogDoesNotDelayAnotherAccount(t *testing.T) {
	w := newReaderWorld(t)
	day := w.now.Truncate(24 * time.Hour)
	w.put(acct1, day, 2000) // sorts first
	w.put(acct2, w.now.Add(-time.Minute), 1)
	per, _ := w.read(true)
	if per[acct2] != 1 || per[acct1] != activity.MaxFilesPerTick-1 {
		t.Fatalf("first run: %v", per)
	}
	for i := 0; i < 3; i++ {
		w.now = w.now.Add(time.Minute)
		w.put(acct2, w.now.Add(-30*time.Second), 1)
		per, _ = w.read(true)
		if per[acct2] != 1 {
			t.Fatalf("run %d: the active account's new file waited behind the backlog: %v", i+2, per)
		}
	}
}

func TestIdleAccountBurstIsReadAtRediscovery(t *testing.T) {
	w := newReaderWorld(t)
	w.put(acct2, w.now.Add(-time.Minute), 1)
	w.read(true)
	// A day later acct2 is idle, and acct1 has a large backlog.
	w.now = w.now.Add(25 * time.Hour)
	w.put(acct1, w.now.Truncate(24*time.Hour), 2000)
	w.read(true) // a rediscovery run: finds acct1, polls idle acct2, which has nothing new
	w.now = w.now.Add(time.Minute)
	w.put(acct2, w.now.Add(-30*time.Second), 1)
	per, _ := w.read(true) // not a rediscovery: idle acct2 isn't polled
	if per[acct2] != 0 {
		t.Fatalf("an idle prefix was polled between rediscoveries: %v", per)
	}
	w.now = w.now.Add(activity.Rediscover)
	per, _ = w.read(true)
	if per[acct2] != 1 {
		t.Fatalf("the idle account's burst wasn't read at the rediscovery: %v", per)
	}
	w.now = w.now.Add(time.Minute)
	w.put(acct2, w.now.Add(-30*time.Second), 1)
	per, _ = w.read(true)
	if per[acct2] != 1 {
		t.Fatalf("the account isn't read every minute after its burst: %v", per)
	}
}

func TestManyBacklogsShareEachRun(t *testing.T) {
	w := newReaderWorld(t)
	day := w.now.Truncate(24 * time.Hour)
	var accts []string
	for i := 0; i < 40; i++ {
		a := fmt.Sprintf("1000000000%02d", i)
		accts = append(accts, a)
		w.put(a, day, 100)
	}
	seen := map[string]bool{}
	for run := 0; run < 10; run++ {
		per, seq := w.read(true)
		for _, a := range accts {
			if per[a] != 10 {
				t.Fatalf("run %d: %s read %d files, want 10", run+1, a, per[a])
			}
		}
		for _, k := range seq {
			if seen[k] {
				t.Fatalf("run %d read %s again", run+1, k)
			}
			seen[k] = true
		}
		w.now = w.now.Add(time.Minute)
	}
	if len(seen) != 4000 || w.bucket.Gets != 4000 {
		t.Fatalf("read %d files with %d gets, want 4000", len(seen), w.bucket.Gets)
	}
	if per, _ := w.read(true); len(per) != 0 {
		t.Fatalf("files left after 10 runs: %v", per)
	}
}

func TestMorePrefixesThanTheRunReadsRotate(t *testing.T) {
	w := newReaderWorld(t)
	var accts []string
	for i := 0; i < 450; i++ {
		a := fmt.Sprintf("%012d", 200000000000+i)
		accts = append(accts, a)
		w.put(a, w.now.Add(-10*time.Minute), 2)
	}
	reached := map[string]bool{}
	per, seq := w.read(true)
	for a := range per {
		reached[a] = true
	}
	if len(per) != activity.MaxFilesPerTick || seq[0][:12] != accts[0] {
		t.Fatalf("first run reached %d prefixes, starting at %s", len(per), seq[0])
	}
	w.now = w.now.Add(time.Minute)
	per, seq = w.read(true)
	for a := range per {
		reached[a] = true
	}
	if seq[0][:12] != accts[activity.MaxFilesPerTick] {
		t.Fatalf("the second run didn't start after the last prefix read: %s", seq[0])
	}
	if len(reached) != len(accts) {
		t.Fatalf("%d of %d prefixes reached within 2 runs", len(reached), len(accts))
	}
}

func TestUncommittedRunRepeatsInTheSameOrder(t *testing.T) {
	w := newReaderWorld(t)
	day := w.now.Truncate(24 * time.Hour)
	for i := 0; i < 450; i++ {
		w.put(fmt.Sprintf("%012d", 300000000000+i), day, 2)
	}
	w.read(true)
	w.now = w.now.Add(time.Minute)
	_, first := w.read(false) // the usage write failed: nothing committed
	w.restart()
	_, again := w.read(true)
	if fmt.Sprint(first) != fmt.Sprint(again) {
		t.Fatal("a run after a failed commit read different files or in a different order")
	}
}

func TestStateWithoutCursorContinues(t *testing.T) {
	w := newReaderWorld(t)
	day := w.now.Truncate(24 * time.Hour)
	w.put(acct1, day, 300)
	w.put(acct2, day, 300)
	w.read(true)
	// State as the previous release wrote it: no cursor.
	var st map[string]any
	if _, err := w.store.GetMeta(context.Background(), "reader", &st); err != nil {
		t.Fatal(err)
	}
	if _, ok := st["cursor"]; !ok {
		t.Fatal("the reader didn't save its cursor")
	}
	delete(st, "cursor")
	if err := w.store.PutMeta(context.Background(), "reader", st); err != nil {
		t.Fatal(err)
	}
	w.restart()
	total := 0
	for i := 0; i < 3; i++ {
		w.now = w.now.Add(time.Minute)
		per, _ := w.read(true)
		total += per[acct1] + per[acct2]
	}
	if total != 600-activity.MaxFilesPerTick || w.bucket.Gets != 600 {
		t.Fatalf("read %d more files with %d gets in all, want %d and 600", total, w.bucket.Gets, 600-activity.MaxFilesPerTick)
	}
}

func TestBacklogListingIsBounded(t *testing.T) {
	w := newReaderWorld(t)
	w.put(acct1, w.now.Truncate(24*time.Hour), 2000)
	w.read(true)
	if w.bucket.Listed > activity.MaxFilesPerTick+50 {
		t.Fatalf("listed %d keys for a run that reads %d", w.bucket.Listed, activity.MaxFilesPerTick)
	}
}
