package main

import (
	"strings"
	"testing"
)

func TestUnpinned(t *testing.T) {
	wf := `jobs:
  a:
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-go@v7
      - name: x
        uses: "some/action@main"
      - uses: ./local/action
      - uses: sigstore/cosign-installer@756fe2d284c7292c4e9b62c8a28f1e5629c59532
`
	got := Unpinned(strings.NewReader(wf))
	if len(got) != 2 || got[0].Uses != "actions/setup-go@v7" || got[0].Line != 5 || got[1].Uses != "some/action@main" {
		t.Fatalf("got %+v", got)
	}
}
