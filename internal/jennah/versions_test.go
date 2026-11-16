package jennah_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

func TestPayersAndGuardVersions(t *testing.T) {
	ctx := context.Background()
	f, c := jennahtest.Start(t)
	for _, d := range []string{
		schema.InboxDatasetID("msp_a", "333333333333"), schema.InboxDatasetID("msp_a", "111111111111"),
		schema.ConfigDatasetID("msp_a", "111111111111"), schema.InboxDatasetID("msp_a", "222222222222"),
		schema.InboxDatasetID("msp_b", "444444444444"), schema.InboxDatasetID("msp_ab", "555555555555"),
		schema.PricesDatasetID, "guardrail.msp_a.not-a-payer.inbox",
	} {
		if _, err := f.CreateDataset(ctx, &datastorev1.CreateDatasetRequest{DatasetId: d}); err != nil {
			t.Fatal(err)
		}
	}
	payers, err := c.Payers(ctx, "msp_a")
	if err != nil || !reflect.DeepEqual(payers, []string{"111111111111", "222222222222", "333333333333"}) {
		t.Fatalf("payers %v %v (paged two at a time)", payers, err)
	}

	at := time.Date(2026, 11, 16, 0, 15, 0, 0, time.UTC)
	hb := func(payer, id, version, code string, when time.Time) {
		detail, _ := json.Marshal(map[string]any{"guard_version": version, "code_sha256": code})
		if err := c.CommitEvent(ctx, schema.InboxDatasetID("msp_a", payer), jennah.Event{ID: id, OccurredAt: when, Kind: schema.KindHeartbeat, GuardVersion: version, Detail: detail}); err != nil {
			t.Fatal(err)
		}
	}
	hb("111111111111", "heartbeat:old", "v0.1.0", "old=", at.Add(-24*time.Hour))
	hb("111111111111", "heartbeat:new", "v0.2.0", "new=", at)
	hb("222222222222", "heartbeat:1", "v0.1.0", "one=", at)
	got := c.GuardVersions(ctx, "msp_a", payers)
	if len(got) != 3 || got[0].Version != "v0.2.0" || got[0].CodeSHA256 != "new=" || !got[0].HeartbeatAt.Equal(at) ||
		got[1].Version != "v0.1.0" || got[2].Version != "" || !got[2].HeartbeatAt.IsZero() || got[2].Err != nil {
		t.Fatalf("versions %+v", got)
	}

	f.FailReads = true
	if got := c.GuardVersions(ctx, "msp_a", payers[:1]); got[0].Err == nil {
		t.Fatalf("a failed read must be reported: %+v", got)
	}
	if _, err := c.Payers(ctx, "msp_a"); err == nil {
		t.Fatal("a failed listing must be an error")
	}
}
