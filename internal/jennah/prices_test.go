package jennah_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

func pricesClient(t *testing.T) (*jennahtest.Server, *jennah.Client) {
	t.Helper()
	srv, c := jennahtest.Start(t)
	ctx := context.Background()
	if _, err := srv.CreateDataset(ctx, &datastorev1.CreateDatasetRequest{DatasetId: schema.PricesDatasetID}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DeclareTables(ctx, &datastorev1.DeclareTablesRequest{DatasetId: schema.PricesDatasetID, Tables: schema.PricesTables()}); err != nil {
		t.Fatal(err)
	}
	return srv, c
}

func chunk(pack, region, doc string) jennah.PriceChunk {
	return jennah.PriceChunk{Pack: pack, Region: region, Document: json.RawMessage(doc), Models: 1}
}

func TestPublishAndReadPrices(t *testing.T) {
	ctx := context.Background()
	srv, c := pricesClient(t)
	if v, err := c.LatestPrices(ctx); err != nil || v != nil {
		t.Fatalf("empty: %v %v", v, err)
	}
	ai := `{"models":{"m1":{"input_per_1k":1,"output_per_1k":2}}}`
	n, err := c.PublishPrices(ctx, []jennah.PriceChunk{chunk("ai", "us-east-1", ai), chunk("ec2", "eu-west-1", `{"m":1}`)}, "first", time.Now())
	if err != nil || n != 1 {
		t.Fatalf("publish: %d %v", n, err)
	}
	v, err := c.LatestPrices(ctx)
	if err != nil || v.Number != 1 || v.Note != "first" || len(v.Chunks) != 2 || v.Layout != schema.PricesLayoutVersion {
		t.Fatalf("manifest: %+v %v", v, err)
	}
	ref, ok := v.Chunk("ai", "us-east-1")
	doc, err := c.PriceChunkDoc(ctx, 1, "ai", "us-east-1")
	if !ok || err != nil || string(doc) != ai || jennah.HashChunk(doc) != ref.SHA256 {
		t.Fatalf("chunk: %s %v (hash ok: %v)", doc, err, jennah.HashChunk(doc) == ref.SHA256)
	}
	if doc, _ := c.PriceChunkDoc(ctx, 1, "ai", "eu-west-1"); doc != nil {
		t.Fatal("a missing chunk must read as nil")
	}

	commits := srv.Commits
	if n, err := c.PublishPrices(ctx, []jennah.PriceChunk{chunk("ec2", "eu-west-1", `{"m":1}`), chunk("ai", "us-east-1", ai)}, "again", time.Now()); err != nil || n != 0 || srv.Commits != commits {
		t.Fatalf("same content must write nothing: %d %v, %d commits", n, err, srv.Commits-commits)
	}
	if n, err := c.PublishPrices(ctx, []jennah.PriceChunk{chunk("ai", "us-east-1", `{"models":{}}`)}, "", time.Now()); err != nil || n != 2 {
		t.Fatalf("second version: %d %v", n, err)
	}
	if doc, _ := c.PriceChunkDoc(ctx, 1, "ai", "us-east-1"); string(doc) != ai {
		t.Fatal("version 1 changed")
	}
}

func TestOversizedChunkIsRefused(t *testing.T) {
	ctx := context.Background()
	srv, c := pricesClient(t)
	big := `{"x":"` + strings.Repeat("a", jennah.MaxPriceChunkBytes) + `"}`
	_, err := c.PublishPrices(ctx, []jennah.PriceChunk{chunk("ec2", "us-east-1", big)}, "", time.Now())
	if !errors.Is(err, jennah.ErrChunkTooLarge) || !strings.Contains(err.Error(), "ec2/us-east-1") || srv.Commits != 0 {
		t.Fatalf("err %v, commits %d", err, srv.Commits)
	}
}

func TestManyChunksSplitAcrossCommitsManifestLast(t *testing.T) {
	ctx := context.Background()
	srv, c := pricesClient(t)
	var chunks []jennah.PriceChunk
	body := `{"x":"` + strings.Repeat("a", 900<<10) + `"}`
	for _, r := range []string{"r1", "r2", "r3", "r4", "r5"} {
		chunks = append(chunks, chunk("ec2", r, body))
	}
	if _, err := c.PublishPrices(ctx, chunks, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if srv.Commits < 3 {
		t.Fatalf("4.5 MB of chunks must be split under the request limit: %d commits", srv.Commits)
	}
	if len(srv.Rows(schema.PricesDatasetID, schema.PriceChunksTable)) != 5 || len(srv.Rows(schema.PricesDatasetID, schema.PriceVersionsTable)) != 1 {
		t.Fatal("rows")
	}
}
