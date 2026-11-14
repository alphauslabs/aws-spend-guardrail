package alerttext

import (
	"errors"
	"testing"
)

func TestNameAccounts(t *testing.T) {
	names := map[string]string{"730335604511": "e2enormal", "891376926337": "e2estandard03", "111122223333": "dev", "444455556666": ""}
	cases := []struct{ in, want string }{
		{"guardrail: 730335604511 cut off", "guardrail: 730335604511 (e2enormal) cut off"},
		{"Account 891376926337 is frozen.", "Account 891376926337 (e2estandard03) is frozen."},
		{"Accounts 730335604511 and 891376926337, then 730335604511.",
			"Accounts 730335604511 (e2enormal) and 891376926337 (e2estandard03), then 730335604511 (e2enormal)."},
		// Unknown ids, longer digit runs, and accounts with no name stay bare.
		{"Account 999988887777 is frozen.", "Account 999988887777 is frozen."},
		{"Event 17303356045111 at 1794538924.", "Event 17303356045111 at 1794538924."},
		{"Account 444455556666 is frozen.", "Account 444455556666 is frozen."},
		// Inside ARNs, cutoff ids, targets, and keys: left alone.
		{"Caller arn:aws:sts::730335604511:assumed-role/x by 730335604511.",
			"Caller arn:aws:sts::730335604511:assumed-role/x by 730335604511 (e2enormal)."},
		{"Cutoff id ai:730335604511:20261114T1457.", "Cutoff id ai:730335604511:20261114T1457."},
		{"Scope account:730335604511 and usage#ai#730335604511 and a/730335604511/b.",
			"Scope account:730335604511 and usage#ai#730335604511 and a/730335604511/b."},
		// Already named (the unprotected-account alert), and naming twice.
		{"- 111122223333 (dev, under root)", "- 111122223333 (dev, under root)"},
		{"Account 730335604511 (e2enormal) is cut off.", "Account 730335604511 (e2enormal) is cut off."},
	}
	for _, c := range cases {
		if got := NameAccounts(c.in, names); got != c.want {
			t.Errorf("NameAccounts(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestNameAccountsLeavesTheErrorLine(t *testing.T) {
	names := map[string]string{"730335604511": "e2enormal"}
	body := WithError("Account 730335604511 could not be cut off.", errors.New("account 730335604511: AccessDenied"))
	want := "Account 730335604511 (e2enormal) could not be cut off.\n\nError: account 730335604511: AccessDenied"
	if got := NameAccounts(body, names); got != want {
		t.Fatalf("got %q", got)
	}
	if err := Check(NameAccounts(body, names)); err != nil {
		t.Fatalf("a named alert must still read well: %v", err)
	}
	if got := NameAccounts("Error: 730335604511 denied", names); got != "Error: 730335604511 denied" {
		t.Fatalf("an error-only text changed: %q", got)
	}
	if got := NameAccounts("Account 730335604511.", nil); got != "Account 730335604511." {
		t.Fatalf("no names must change nothing: %q", got)
	}
}
