package prices

import (
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
)

func TestReadManual(t *testing.T) {
	got, err := ReadManual([]byte(`{"models": {"us.xai.grok-4.6": {"input_per_1k": 0.003, "output_per_1k": 0.015}, "zai.glm-5.3": {"input_per_1k": 0.001, "output_per_1k": 0.0032}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["xai.grok-4.6"] != (config.ModelPrice{InputPer1K: 0.003, OutputPer1K: 0.015}) {
		t.Fatalf("ids must be normalized: %+v", got)
	}
	for name, tc := range map[string]struct{ file, want string }{
		"unknown top field":   {`{"models": {"a.m": {"input_per_1k": 1, "output_per_1k": 1}}, "note": "x"}`, `unknown field "note"`},
		"unknown price field": {`{"models": {"a.m": {"input_per_1k": 1, "output_per_1k": 1, "per_image": 2}}}`, `unknown field "per_image"`},
		"zero price":          {`{"models": {"a.m": {"input_per_1k": 1, "output_per_1k": 0}}}`, "a.m: output_per_1k must be above zero"},
		"missing price":       {`{"models": {"a.m": {"output_per_1k": 1}}}`, "a.m: input_per_1k must be above zero"},
		"negative price":      {`{"models": {"a.m": {"input_per_1k": -1, "output_per_1k": 1}}}`, "a.m: input_per_1k must be above zero"},
		"same model twice":    {`{"models": {"us.a.m": {"input_per_1k": 1, "output_per_1k": 1}, "a.m": {"input_per_1k": 2, "output_per_1k": 2}}}`, "a.m and us.a.m are the same model"},
		"no models":           {`{"models": {}}`, "no models"},
		"not JSON":            {`models:`, "manual prices:"},
	} {
		if _, err := ReadManual([]byte(tc.file)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", name, err, tc.want)
		}
	}
}

func TestMergeManual(t *testing.T) {
	built := &Table{Basis: Basis, Models: map[string]config.ModelPrice{
		"amazon.nova-pro-v1:0": {InputPer1K: 0.0008, OutputPer1K: 0.0032},
		"amazon.nova-lite":     {InputPer1K: 0.00006, OutputPer1K: 0.00024},
	}}
	manual := map[string]ManualPrice{
		"xai.grok-4.6":         {InputPer1K: 0.003, OutputPer1K: 0.015, SetInVersion: 5},   // missed by the build
		"amazon.nova-pro-v1:0": {InputPer1K: 0.0005, OutputPer1K: 0.004, SetInVersion: 5},  // below on input, above on output
		"amazon.nova-lite":     {InputPer1K: 0.0001, OutputPer1K: 0.0003, SetInVersion: 4}, // above on both
	}
	got := MergeManual(built, manual)
	want := map[string]config.ModelPrice{
		"xai.grok-4.6":         {InputPer1K: 0.003, OutputPer1K: 0.015},
		"amazon.nova-pro-v1:0": {InputPer1K: 0.0008, OutputPer1K: 0.004},
		"amazon.nova-lite":     {InputPer1K: 0.0001, OutputPer1K: 0.0003},
	}
	for id, w := range want {
		if got.Models[id] != w {
			t.Errorf("%s: %+v, want %+v", id, got.Models[id], w)
		}
	}
	if len(got.Manual) != 3 || got.Manual["amazon.nova-pro-v1:0"].InputPer1K != 0.0005 || got.Manual["amazon.nova-lite"].SetInVersion != 4 {
		t.Errorf("manual must record the prices as given: %+v", got.Manual)
	}
	if !strings.HasSuffix(got.Basis, ManualNote) || built.Models["amazon.nova-pro-v1:0"].OutputPer1K != 0.0032 || built.Manual != nil {
		t.Errorf("basis %q, or the built table changed", got.Basis)
	}
	// Merging again, or with no manual prices, doesn't stack the note.
	if again := MergeManual(got, manual); strings.Count(again.Basis, ManualNote) != 1 {
		t.Errorf("basis %q", again.Basis)
	}
	if none := MergeManual(got, nil); none.Manual != nil || strings.Contains(none.Basis, ManualNote) {
		t.Errorf("no manual prices: %+v", none)
	}
}

func TestPriceOfReadsAsDecimals(t *testing.T) {
	if got := (PriceOf{In: 0.00001, Out: 0.00005}).String(); got != "in 0.00001 / out 0.00005 per 1k" {
		t.Errorf("PriceOf = %q", got)
	}
}
