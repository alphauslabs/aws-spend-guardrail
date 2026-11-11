package jennah_test

import (
	"context"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	authv1 "github.com/alphauslabs/jennah-sdk-go/jennah/auth/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCallingKey(t *testing.T) {
	ctx := context.Background()
	srv, c := jennahtest.Start(t)

	exp := time.Date(2027, 11, 11, 0, 0, 0, 0, time.UTC)
	srv.CallingKey = &authv1.ApiKey{KeyId: "k-1", Label: "guardrail-msp_a-123456789012-reader", ExpiresAt: timestamppb.New(exp)}
	k, err := c.CallingKey(ctx)
	if err != nil || !k.Known || k.ID != "k-1" || k.Label != "guardrail-msp_a-123456789012-reader" || !k.ExpiresAt.Equal(exp) {
		t.Fatalf("a key with an expiry: %+v %v", k, err)
	}

	srv.CallingKey = &authv1.ApiKey{KeyId: "k-2"}
	if k, err := c.CallingKey(ctx); err != nil || !k.Known || !k.ExpiresAt.IsZero() {
		t.Fatalf("a key that never expires: %+v %v", k, err)
	}

	srv.CallingKey = nil
	if k, err := c.CallingKey(ctx); err != nil || k.Known {
		t.Fatalf("no key in the response must be unknown: %+v %v", k, err)
	}

	srv.FailReads = true
	if _, err := c.CallingKey(ctx); err == nil {
		t.Fatal("a failed call must return an error")
	}
}
