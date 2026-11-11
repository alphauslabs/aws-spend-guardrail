package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
	"google.golang.org/protobuf/proto"
)

func TestSetupAppliesRetentionToAnOldInbox(t *testing.T) {
	ctx := context.Background()
	c, f, out, _ := newCLI(t)

	// An inbox set up before retention existed.
	inbox := schema.InboxDatasetID(c.MSP, c.Payer)
	if _, _, err := c.J.SDK().Datasets.Create(ctx, &datastorev1.CreateDatasetRequest{DatasetId: inbox}); err != nil {
		t.Fatal(err)
	}
	old := proto.Clone(schema.InboxTables()[0]).(*datastorev1.TableDeclaration)
	old.Retention = nil
	if _, err := c.J.SDK().Dataset(inbox).Schema.Declare(ctx, &datastorev1.DeclareTablesRequest{Tables: []*datastorev1.TableDeclaration{old}}); err != nil {
		t.Fatal(err)
	}

	if err := c.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		inbox + ": events: rows whose occurred_at is older than 400 days will be removed",
		"events: retention none -> occurred_at, 400 days; rows older than 400 days are being removed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	sch, err := c.J.SDK().Dataset(inbox).Schema.Get(ctx)
	if err != nil || sch.GetTables()[0].GetRetention().GetDays() != 400 {
		t.Fatalf("retention not in force: %v %v", sch, err)
	}

	declares := f.Declares
	out.Reset()
	if err := c.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if f.Declares != declares || !strings.Contains(out.String(), "nothing changed") || strings.Contains(out.String(), "removed") {
		t.Fatalf("second run changed something:\n%s", out)
	}
}

func TestSetupCreatesANewInboxWithRetention(t *testing.T) {
	ctx := context.Background()
	c, _, out, _ := newCLI(t)
	if err := c.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "will be removed") {
		t.Fatalf("a new inbox has nothing to remove, so no warning:\n%s", out)
	}
	sch, err := c.J.SDK().Dataset(schema.InboxDatasetID(c.MSP, c.Payer)).Schema.Get(ctx)
	if err != nil || sch.GetTables()[0].GetRetention().GetDays() != 400 {
		t.Fatalf("new inbox retention: %v %v", sch, err)
	}
	cfg, err := c.J.SDK().Dataset(schema.ConfigDatasetID(c.MSP, c.Payer)).Schema.Get(ctx)
	if err != nil || cfg.GetTables()[0].GetRetention() != nil {
		t.Fatalf("config must have no retention: %v %v", cfg, err)
	}
}
