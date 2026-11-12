package alerttext

import (
	"errors"
	"testing"
)

func TestCheck(t *testing.T) {
	good := []string{
		"Account 111122223333 is frozen. New spend is blocked.",
		WithError("The guard can't read its config from Jennah.", errors.New("rpc error: code = Unavailable")),
		"These active accounts have no budget:\n- 111122223333 (dev, under root)\n- 444455556666 (prod, under ou-x)",
		"Config version 2 is invalid and was not applied. Fix these problems:\n- scopes: required",
	}
	for _, b := range good {
		if err := Check(b); err != nil {
			t.Errorf("%q: %v", b, err)
		}
	}
	bad := []string{
		"",
		"account:111122223333 is frozen.",
		"cannot read config.",
		"The freeze was lifted",
		"Error: boom",
		"The guard failed.\nError: boom\nMore text.",
	}
	for _, b := range bad {
		if err := Check(b); err == nil {
			t.Errorf("%q passed", b)
		}
	}
}

func TestWithError(t *testing.T) {
	if got := WithError("Done.", nil); got != "Done." {
		t.Fatalf("got %q", got)
	}
	if got := WithError("Failed.", errors.New("boom")); got != "Failed.\n\nError: boom" {
		t.Fatalf("got %q", got)
	}
}
