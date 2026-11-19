package activity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Object is one S3 object in a listing.
type Object struct {
	Key          string
	LastModified time.Time
}

// Bucket is the part of S3 the reader uses, on the trail's bucket.
type Bucket interface {
	// List returns keys under prefix that sort after startAfter, in order. With
	// limit above 0 it may stop paging once it holds at least limit keys.
	List(ctx context.Context, prefix, startAfter string, limit int) ([]Object, error)
	// Dirs returns the common prefixes one level below prefix.
	Dirs(ctx context.Context, prefix string) ([]string, error)
	Get(ctx context.Context, key string) ([]byte, error)
}

// Timings of the reader (design D1).
const (
	// Rediscover is how often every account and region prefix is listed;
	// between rediscoveries only active prefixes are polled.
	Rediscover = 10 * time.Minute
	// ActiveFor is how long a prefix stays polled every minute after its last
	// file.
	ActiveFor = 24 * time.Hour
	// Lookback re-lists this much before a prefix's newest file, because
	// CloudTrail files can land slightly out of name order.
	Lookback = 15 * time.Minute
)

// Reader reads new organization trail files, each once.
type Reader struct {
	Bucket Bucket
	Store  Store
	// Base is the trail's key prefix down to the organization:
	// "[TrailPrefix/]AWSLogs/<org id>/".
	Base     string
	lastFile time.Time
	bad      []string
	pending  *readerState
}

// Bad lists the files the last ReadNew skipped because they couldn't be
// parsed. Each is skipped once, not retried.
func (r *Reader) Bad() []string { return r.bad }

// prefixState is a region prefix's reading position.
type prefixState struct {
	Newest  time.Time       `json:"newest"`  // newest file seen
	Seen    map[string]bool `json:"seen"`    // keys read within the look-back window
	LastHit time.Time       `json:"lasthit"` // when a file last arrived
}

type readerState struct {
	Discovered time.Time               `json:"discovered"`
	Prefixes   map[string]*prefixState `json:"prefixes"` // "<account>/CloudTrail/<region>/"
	// Cursor is the last prefix the previous run read a file from; the next
	// run's order starts after it (fair-trail-reading D2).
	Cursor string `json:"cursor,omitempty"`
}

// LastFile is the time the newest file read in the last ReadNew landed.
func (r *Reader) LastFile() time.Time { return r.lastFile }

// ReadNew returns the records in trail files that landed since the last
// committed position (see Commit).
func (r *Reader) ReadNew(ctx context.Context, now time.Time) ([]Record, error) {
	var st readerState
	if _, err := r.Store.GetMeta(ctx, "reader", &st); err != nil {
		return nil, err
	}
	if st.Prefixes == nil {
		st.Prefixes = map[string]*prefixState{}
	}
	var errs []error
	discover := now.Sub(st.Discovered) >= Rediscover
	if discover {
		if err := r.discover(ctx, &st, now); err != nil {
			errs = append(errs, err)
		} else {
			st.Discovered = now
		}
	}
	r.lastFile, r.bad = time.Time{}, nil
	names := make([]string, 0, len(st.Prefixes))
	for p := range st.Prefixes {
		names = append(names, p)
	}
	sort.Strings(names)

	// List every prefix due this run, then share the run's files among the
	// prefixes that have new ones (fair-trail-reading D1).
	queues := map[string][]Object{}
	var due []string
	for _, p := range names {
		ps := st.Prefixes[p]
		if !discover && now.Sub(ps.LastHit) > ActiveFor {
			continue // idle; polled again at the next rediscovery
		}
		due = append(due, p)
		objs, err := r.unread(ctx, p, ps)
		if err != nil {
			errs = append(errs, err)
		}
		if len(objs) > 0 {
			queues[p] = objs
		}
	}
	order := rotate(names, st.Cursor, queues)
	var out []Record
	budget := MaxFilesPerTick
	take := func(p string) {
		o := queues[p][0]
		queues[p] = queues[p][1:]
		recs, read, err := r.readFile(ctx, st.Prefixes[p], o, now)
		if err != nil {
			errs = append(errs, err)
		}
		if read {
			budget--
			st.Cursor = p
		}
		out = append(out, recs...)
	}
	if len(order) > 0 {
		share := max(1, budget/len(order))
		for _, p := range order {
			for n := 0; n < share && budget > 0 && len(queues[p]) > 0; n++ {
				take(p)
			}
		}
		// The rest, one file at a time round-robin, until the budget or the
		// files run out.
		for more := true; more && budget > 0; {
			more = false
			for _, p := range order {
				if budget > 0 && len(queues[p]) > 0 {
					take(p)
					more = true
				}
			}
		}
	}
	for _, p := range due {
		forget(st.Prefixes[p])
	}
	r.pending = &st
	return out, errors.Join(errs...)
}

// Commit saves the position reached by the last ReadNew. The engine calls it
// only once the records' usage is stored, so a failed write leaves the files
// to be read again on the next tick instead of losing their calls.
func (r *Reader) Commit(ctx context.Context) error {
	if r.pending == nil {
		return nil
	}
	if err := r.Store.PutMeta(ctx, "reader", r.pending); err != nil {
		return err
	}
	r.pending = nil
	return nil
}

func (r *Reader) discover(ctx context.Context, st *readerState, now time.Time) error {
	accts, err := r.Bucket.Dirs(ctx, r.Base)
	if err != nil {
		return fmt.Errorf("list trail accounts: %w", err)
	}
	for _, a := range accts {
		regions, err := r.Bucket.Dirs(ctx, a+"CloudTrail/")
		if err != nil {
			return fmt.Errorf("list trail regions of %s: %w", a, err)
		}
		for _, reg := range regions {
			p := strings.TrimPrefix(reg, r.Base)
			if _, ok := st.Prefixes[p]; !ok {
				// A new prefix starts at the beginning of today, so a
				// region's first calls are read, not skipped.
				st.Prefixes[p] = &prefixState{Newest: now.Truncate(24 * time.Hour), Seen: map[string]bool{}, LastHit: now}
			}
		}
	}
	return nil
}

// fileTime parses the time in a CloudTrail file name:
// <account>_CloudTrail_<region>_<YYYYMMDDTHHMMZ>_<unique>.json.gz
func fileTime(key string) (time.Time, bool) {
	name := key[strings.LastIndex(key, "/")+1:]
	parts := strings.Split(name, "_")
	if len(parts) < 5 {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102T1504Z", parts[3])
	return t, err == nil
}

// startKey is a key just before the files named at t under a region prefix
// "<base><account>/CloudTrail/<region>/".
func startKey(full, p string, t time.Time) string {
	parts := strings.Split(strings.TrimSuffix(p, "/"), "/")
	if len(parts) < 3 {
		return full + t.Format("2006/01/02/")
	}
	acct, region := parts[0], parts[2]
	return full + t.Format("2006/01/02/") + acct + "_CloudTrail_" + region + "_" + t.Format("20060102T1504Z")
}

// MaxFilesPerTick bounds one tick's reading so a first run on a busy
// organization stays inside the function's time limit; the rest is read on
// the next ticks, and counted at its event time.
const MaxFilesPerTick = 400

// unread lists a prefix's files not yet read, oldest first. The listing
// stops near what one run could read: the run's files plus the keys already
// seen in the look-back window (fair-trail-reading D3).
func (r *Reader) unread(ctx context.Context, p string, ps *prefixState) ([]Object, error) {
	full := r.Base + p
	from := ps.Newest.Add(-Lookback)
	if ps.Seen == nil {
		ps.Seen = map[string]bool{}
	}
	objs, err := r.Bucket.List(ctx, full, startKey(full, p, from), MaxFilesPerTick+len(ps.Seen))
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", p, err)
	}
	var out []Object
	for _, o := range objs {
		ft, ok := fileTime(o.Key)
		if !ok || ps.Seen[o.Key] || ft.Before(from) {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

// readFile reads one file of a prefix. read is false when the file couldn't
// be fetched, so it's retried on a later run and doesn't count against the
// run's files.
func (r *Reader) readFile(ctx context.Context, ps *prefixState, o Object, now time.Time) ([]Record, bool, error) {
	body, err := r.Bucket.Get(ctx, o.Key)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", o.Key, err)
	}
	ps.Seen[o.Key] = true
	recs, err := ParseFile(body)
	if err != nil {
		r.bad = append(r.bad, o.Key)
		return nil, true, nil // a bad file is skipped and alerted, not retried forever
	}
	if ft, _ := fileTime(o.Key); ft.After(ps.Newest) {
		ps.Newest = ft
	}
	ps.LastHit = now
	if o.LastModified.After(r.lastFile) {
		r.lastFile = o.LastModified
	}
	return recs, true, nil
}

// rotate orders the prefixes with unread files starting after cursor,
// wrapping around, so no prefix is always read last (fair-trail-reading D2).
// names is sorted.
func rotate(names []string, cursor string, queues map[string][]Object) []string {
	start := sort.SearchStrings(names, cursor)
	if start < len(names) && names[start] == cursor {
		start++
	}
	var out []string
	for i := range names {
		p := names[(start+i)%len(names)]
		if len(queues[p]) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// forget drops seen keys older than the look-back window.
func forget(ps *prefixState) {
	for k := range ps.Seen {
		if ft, ok := fileTime(k); !ok || ft.Before(ps.Newest.Add(-2*Lookback)) {
			delete(ps.Seen, k)
		}
	}
}
