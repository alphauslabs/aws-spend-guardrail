package jennah

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

// GuardSeen is what a payer org's newest heartbeat says about its guard
// (add-guard-upgrades D6).
type GuardSeen struct {
	Payer       string
	Version     string    // "" when there is no heartbeat
	CodeSHA256  string    // "" when there is no heartbeat
	HeartbeatAt time.Time // zero when there is no heartbeat
	Err         error     // the heartbeat couldn't be read
}

// Payers lists the payer orgs of an MSP that have an inbox dataset, sorted.
func (c *Client) Payers(ctx context.Context, msp string) ([]string, error) {
	re := regexp.MustCompile(`^guardrail\.` + regexp.QuoteMeta(msp) + `\.([0-9]{12})\.inbox$`)
	var out []string
	for d, err := range c.c.Datasets.All(ctx, &datastorev1.ListDatasetsRequest{}) {
		if err != nil {
			return nil, fmt.Errorf("list datasets: %w", err)
		}
		if m := re.FindStringSubmatch(d.GetDatasetId()); m != nil && schema.InboxDatasetID(msp, m[1]) == d.GetDatasetId() {
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out, nil
}

// GuardVersions reads each payer org's newest heartbeat, eight at a time, in
// the order given.
func (c *Client) GuardVersions(ctx context.Context, msp string, payers []string) []GuardSeen {
	out := make([]GuardSeen, len(payers))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, p := range payers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = c.guardSeen(ctx, msp, p)
		}()
	}
	wg.Wait()
	return out
}

func (c *Client) guardSeen(ctx context.Context, msp, payer string) GuardSeen {
	g := GuardSeen{Payer: payer}
	e, err := c.LatestEvent(ctx, schema.InboxDatasetID(msp, payer), schema.KindHeartbeat)
	if err != nil {
		g.Err = err
		return g
	}
	if e == nil {
		return g
	}
	var d struct {
		Version string `json:"guard_version"`
		Code    string `json:"code_sha256"`
	}
	_ = json.Unmarshal(e.Detail, &d)
	g.Version, g.CodeSHA256, g.HeartbeatAt = e.GuardVersion, d.Code, e.OccurredAt
	if g.Version == "" {
		g.Version = d.Version
	}
	return g
}
