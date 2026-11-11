// Package activitytest has in-memory fakes of the activity engine's
// dependencies, for tests in this repo.
package activitytest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
)

// Store is an in-memory activity.Store with the same counter semantics as the
// DynamoDB store: minute buckets for the hour, day and month counters.
type Store struct {
	mu      sync.Mutex
	minutes map[string]float64 // pack/account/YYYYMMDDHHMM
	days    map[string]float64 // pack/account/YYYYMMDD
	months  map[string]float64 // pack/account/YYYYMM
	hours   map[string]*activity.Summary
	cuts    map[string]activity.Cutoff
	meta    map[string][]byte
}

func NewStore() *Store {
	return &Store{minutes: map[string]float64{}, days: map[string]float64{}, months: map[string]float64{},
		hours: map[string]*activity.Summary{}, cuts: map[string]activity.Cutoff{}, meta: map[string][]byte{}}
}

func (s *Store) AddUsage(_ context.Context, pack string, u activity.Usage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := u.Time.UTC()
	s.minutes[pack+"/"+u.Account+"/"+t.Format("200601021504")] += u.USD
	s.days[pack+"/"+u.Account+"/"+t.Format("20060102")] += u.USD
	s.months[pack+"/"+u.Account+"/"+t.Format("200601")] += u.USD
	k := pack + "/" + t.Format("2006010215") + "/" + u.Account + "/" + u.Model
	h := s.hours[k]
	if h == nil {
		h = &activity.Summary{Account: u.Account, Model: u.Model}
		s.hours[k] = h
	}
	h.Calls += u.Calls
	h.In += u.In
	h.Out += u.Out
	h.USD += u.USD
	return nil
}

func (s *Store) Totals(_ context.Context, pack, account string, now time.Time) (activity.Totals, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t activity.Totals
	now = now.UTC()
	for m := now.Add(-59 * time.Minute).Truncate(time.Minute); !m.After(now); m = m.Add(time.Minute) {
		t.Hour += s.minutes[pack+"/"+account+"/"+m.Format("200601021504")]
	}
	t.Day = s.days[pack+"/"+account+"/"+now.Format("20060102")]
	t.Month = s.months[pack+"/"+account+"/"+now.Format("200601")]
	return t, nil
}

func (s *Store) HourSummaries(_ context.Context, pack string, hour time.Time) ([]activity.Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []activity.Summary
	p := pack + "/" + hour.UTC().Format("2006010215") + "/"
	for k, v := range s.hours {
		if strings.HasPrefix(k, p) {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account+out[i].Model < out[j].Account+out[j].Model })
	return out, nil
}

func (s *Store) Cutoffs(_ context.Context, pack string) ([]activity.Cutoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []activity.Cutoff
	for _, c := range s.cuts {
		if c.Pack == pack {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out, nil
}

func (s *Store) PutCutoff(_ context.Context, c activity.Cutoff) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cuts[c.Pack+"/"+c.Account] = c
	return nil
}

func (s *Store) DeleteCutoff(_ context.Context, pack, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cuts, pack+"/"+account)
	return nil
}

func (s *Store) GetMeta(_ context.Context, name string, v any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.meta[name]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, v)
}

func (s *Store) PutMeta(_ context.Context, name string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(v)
	s.meta[name] = b
	return err
}

// Bucket is an in-memory trail bucket.
type Bucket struct {
	mu      sync.Mutex
	Objects map[string]activity.Object
	Bodies  map[string][]byte
	Gets    int
	// Deny makes every read fail with an access error.
	Deny bool
}

func NewBucket() *Bucket {
	return &Bucket{Objects: map[string]activity.Object{}, Bodies: map[string][]byte{}}
}

// Put stores a file at key, landed at t.
func (b *Bucket) Put(key string, body []byte, t time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Objects[key] = activity.Object{Key: key, LastModified: t}
	b.Bodies[key] = body
}

func (b *Bucket) List(_ context.Context, prefix, after string) ([]activity.Object, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Deny {
		return nil, errors.New("AccessDenied: fake")
	}
	var out []activity.Object
	for k, o := range b.Objects {
		if strings.HasPrefix(k, prefix) && k > after {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (b *Bucket) Dirs(_ context.Context, prefix string) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Deny {
		return nil, errors.New("AccessDenied: fake")
	}
	seen := map[string]bool{}
	for k := range b.Objects {
		if rest, ok := strings.CutPrefix(k, prefix); ok {
			if i := strings.Index(rest, "/"); i > 0 {
				seen[prefix+rest[:i+1]] = true
			}
		}
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func (b *Bucket) Get(_ context.Context, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Deny {
		return nil, errors.New("AccessDenied: fake")
	}
	b.Gets++
	body, ok := b.Bodies[key]
	if !ok {
		return nil, fmt.Errorf("no such key %s", key)
	}
	return body, nil
}

// Policies records attach and detach calls.
type Policies struct {
	mu       sync.Mutex
	Attached map[string]bool // policy/account
	Calls    []string
}

func NewPolicies() *Policies { return &Policies{Attached: map[string]bool{}} }

func (p *Policies) Attach(_ context.Context, policy, account string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Attached[policy+"/"+account] = true
	p.Calls = append(p.Calls, "attach "+account)
	return nil
}

func (p *Policies) Detach(_ context.Context, policy, account string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.Attached, policy+"/"+account)
	p.Calls = append(p.Calls, "detach "+account)
	return nil
}

// TrailKey is a trail file key under base for an account and region, named at t.
func TrailKey(base, account, region string, t time.Time, unique string) string {
	return fmt.Sprintf("%s%s/CloudTrail/%s/%s%s_CloudTrail_%s_%s_%s.json.gz", base, account, region, t.Format("2006/01/02/"), account, region, t.Format("20060102T1504Z"), unique)
}

// TrailFile gzips CloudTrail records into a file body.
func TrailFile(records ...map[string]any) []byte {
	raw, _ := json.Marshal(map[string]any{"Records": records})
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return buf.Bytes()
}

// BedrockCall is a CloudTrail record of a non-streaming Bedrock call.
func BedrockCall(account, region, model, caller string, t time.Time, in, out int64, bearer bool) map[string]any {
	add := map[string]any{"inferenceRegion": region, "inputTokens": in, "outputTokens": out}
	if bearer {
		add["callWithBearerToken"] = true
	}
	return map[string]any{
		"eventTime": t.UTC().Format(time.RFC3339), "eventSource": "bedrock.amazonaws.com", "eventName": "Converse",
		"eventType": "AwsApiCall", "awsRegion": region, "recipientAccountId": account,
		"userIdentity":        map[string]any{"type": "AssumedRole", "arn": caller},
		"requestParameters":   map[string]any{"modelId": model},
		"additionalEventData": add,
	}
}
