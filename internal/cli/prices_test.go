package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/prices"
)

func table(models map[string]config.ModelPrice) *prices.Table {
	return &prices.Table{Source: prices.Source, Basis: prices.Basis, Published: "2026-11-11T00:00:00Z", Models: models}
}

func TestPublishPricesShowsChangesAndHonoursDryRun(t *testing.T) {
	ctx := context.Background()
	c, f, out, status := newCLI(t)
	if err := c.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	t1 := table(map[string]config.ModelPrice{"a.m1": {InputPer1K: 1, OutputPer1K: 2}, "a.m2": {InputPer1K: 1, OutputPer1K: 1}})
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", t1, []prices.Unmatched{{ID: "x.new", Name: "New"}}, "first", false); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "enterprise ent-1") || !strings.Contains(got, "added    a.m1") || !strings.Contains(got, "published price version 1") {
		t.Fatalf("first publish:\n%s", got)
	}
	if !strings.Contains(status.String(), "no price found for x.new") {
		t.Fatalf("unmatched not reported: %s", status)
	}

	t2 := table(map[string]config.ModelPrice{"a.m1": {InputPer1K: 2, OutputPer1K: 2}, "a.m3": {InputPer1K: 1, OutputPer1K: 1}})
	commits := f.Commits
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", t2, nil, "", true); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"raised   a.m1", "added    a.m3", "removed  a.m2", "dry run: nothing published"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if f.Commits != commits {
		t.Fatal("a dry run wrote something")
	}
	out.Reset()
	if err := c.PublishPrices(ctx, "ent-1", t1, nil, "", false); err != nil || !strings.Contains(out.String(), "no changes") {
		t.Fatalf("same prices: %v\n%s", err, out)
	}
}
