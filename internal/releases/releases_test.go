package releases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatest(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent")
		}
		_, _ = w.Write([]byte(`{"tag_name": "v0.2.0", "name": "v0.2.0"}`))
	}))
	defer ok.Close()
	if v, err := Latest(context.Background(), ok.Client(), ok.URL); err != nil || v != "v0.2.0" {
		t.Fatalf("latest %q %v", v, err)
	}
	for name, h := range map[string]http.HandlerFunc{
		"rate limited": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"no tag":       func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"tag_name": "nightly"}`)) },
		"not JSON":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) },
	} {
		s := httptest.NewServer(h)
		if _, err := Latest(context.Background(), s.Client(), s.URL); err == nil || !strings.Contains(err.Error(), "latest release") {
			t.Errorf("%s: %v", name, err)
		}
		s.Close()
	}
}

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		n    int
		ok   bool
	}{
		{"v0.1.0", "v0.2.0", -1, true},
		{"v0.10.0", "v0.9.3", 1, true},
		{"v1.2.3", "v1.2.3", 0, true},
		{"dev", "v0.2.0", 0, false},
		{"9c6aa3c", "v0.2.0", 0, false},
		{"v0.2.0", "", 0, false},
		{"v0.2", "v0.2.0", 0, false},
	} {
		if n, ok := Compare(tc.a, tc.b); n != tc.n || ok != tc.ok {
			t.Errorf("Compare(%q, %q) = %d %v", tc.a, tc.b, n, ok)
		}
	}
}
