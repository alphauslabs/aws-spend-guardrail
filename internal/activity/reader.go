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
	// List returns keys under prefix that sort after startAfter, in order.
	List(ctx context.Context, prefix, startAfter string) ([]Object, error)
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
}

// LastFile is the time the newest file read in the last ReadNew landed.
func (r *Reader) LastFile() time.Time { return r.lastFile }

// ReadNew returns the records in trail files that landed since the last call.
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
	var out []Record
	r.lastFile, r.bad = time.Time{}, nil
	budget := MaxFilesPerTick
	names := make([]string, 0, len(st.Prefixes))
	for p := range st.Prefixes {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		ps := st.Prefixes[p]
		if !discover && now.Sub(ps.LastHit) > ActiveFor {
			continue // idle; polled again at the next rediscovery
		}
		recs, err := r.readPrefix(ctx, p, ps, now, &budget)
		out = append(out, recs...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	if err := r.Store.PutMeta(ctx, "reader", st); err != nil {
		errs = append(errs, err)
	}
	return out, errors.Join(errs...)
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

func (r *Reader) readPrefix(ctx context.Context, p string, ps *prefixState, now time.Time, budget *int) ([]Record, error) {
	full := r.Base + p
	from := ps.Newest.Add(-Lookback)
	objs, err := r.Bucket.List(ctx, full, startKey(full, p, from))
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", p, err)
	}
	if ps.Seen == nil {
		ps.Seen = map[string]bool{}
	}
	var out []Record
	var errs []error
	for _, o := range objs {
		if *budget <= 0 {
			break
		}
		ft, ok := fileTime(o.Key)
		if !ok || ps.Seen[o.Key] || ft.Before(from) {
			continue
		}
		body, err := r.Bucket.Get(ctx, o.Key)
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", o.Key, err))
			continue // retried next tick
		}
		ps.Seen[o.Key] = true
		*budget--
		recs, err := ParseFile(body)
		if err != nil {
			r.bad = append(r.bad, o.Key)
			continue // a bad file is skipped and alerted, not retried forever
		}
		out = append(out, recs...)
		if ft.After(ps.Newest) {
			ps.Newest = ft
		}
		ps.LastHit = now
		if o.LastModified.After(r.lastFile) {
			r.lastFile = o.LastModified
		}
	}
	// Forget keys older than the look-back window.
	for k := range ps.Seen {
		if ft, ok := fileTime(k); !ok || ft.Before(ps.Newest.Add(-2*Lookback)) {
			delete(ps.Seen, k)
		}
	}
	return out, errors.Join(errs...)
}
