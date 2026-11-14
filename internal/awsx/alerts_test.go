package awsx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type fakeSNSPublish struct{ subject, msg string }

func (f *fakeSNSPublish) Publish(_ context.Context, in *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	f.subject, f.msg = aws.ToString(in.Subject), aws.ToString(in.Message)
	return &sns.PublishOutput{}, nil
}

func namesOf(m map[string]string) func(context.Context) map[string]string {
	return func(context.Context) map[string]string { return m }
}

func TestAlertsNameAccounts(t *testing.T) {
	f := &fakeSNSPublish{}
	a := &Alerts{API: f, Prefix: "[339712710400]", Names: namesOf(map[string]string{"730335604511": "e2enormal", "339712710400": "payer"})}
	if err := a.Alert(context.Background(), "guardrail: 730335604511 cut off", "Account 730335604511 is cut off. Cutoff id ai:730335604511:20261114T1457."); err != nil {
		t.Fatal(err)
	}
	if f.subject != "[339712710400] guardrail: 730335604511 (e2enormal) cut off" {
		t.Errorf("subject %q", f.subject)
	}
	if f.msg != "Account 730335604511 (e2enormal) is cut off. Cutoff id ai:730335604511:20261114T1457." {
		t.Errorf("body %q", f.msg)
	}
}

func TestAlertsLongNameKeepsSubjectBare(t *testing.T) {
	f := &fakeSNSPublish{}
	long := strings.Repeat("n", 50)
	a := &Alerts{API: f, Prefix: "[339712710400]", Names: namesOf(map[string]string{"730335604511": long})}
	subject := "guardrail: 730335604511 would be cut off (dry-run), hourly cap"
	if err := a.Alert(context.Background(), subject, "Account 730335604511 would be cut off."); err != nil {
		t.Fatal(err)
	}
	if f.subject != "[339712710400] "+subject {
		t.Errorf("a subject that names would push past 100 characters must stay bare: %q", f.subject)
	}
	if f.msg != "Account 730335604511 ("+long+") would be cut off." {
		t.Errorf("the body must still be named: %q", f.msg)
	}
}

func TestAlertsWithoutNames(t *testing.T) {
	for _, names := range []func(context.Context) map[string]string{nil, namesOf(nil)} {
		f := &fakeSNSPublish{}
		a := &Alerts{API: f, Names: names}
		if err := a.Alert(context.Background(), "guardrail: 730335604511 cut off", "Account 730335604511 is cut off."); err != nil {
			t.Fatal(err)
		}
		if f.subject != "guardrail: 730335604511 cut off" || f.msg != "Account 730335604511 is cut off." {
			t.Errorf("got %q / %q", f.subject, f.msg)
		}
	}
}

func TestAccountNamesCacheAndRefresh(t *testing.T) {
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	calls := 0
	var fail bool
	name := "e2enormal"
	n := &AccountNames{
		Now: func() time.Time { return now },
		List: func(ctx context.Context) ([]guard.Account, error) {
			calls++
			if _, ok := ctx.Deadline(); !ok {
				t.Error("a listing must have a time limit")
			}
			if fail {
				return nil, errors.New("AccessDenied")
			}
			return []guard.Account{{ID: "730335604511", Name: name}, {ID: "111122223333"}}, nil
		},
	}
	ctx := context.Background()

	// A failed first listing gives no names, and the next call tries again.
	fail = true
	if got := n.Names(ctx); got != nil {
		t.Fatalf("no listing yet must give nil: %v", got)
	}
	fail = false
	if got := n.Names(ctx); got["730335604511"] != "e2enormal" || len(got) != 1 || calls != 2 {
		t.Fatalf("first listing: %v, %d calls", got, calls)
	}
	// Within the hour, the cache answers.
	now = now.Add(59 * time.Minute)
	n.Names(ctx)
	if calls != 2 {
		t.Fatalf("listed again within the hour: %d calls", calls)
	}
	// After an hour it lists again and picks up a rename.
	name = "renamed"
	now = now.Add(2 * time.Minute)
	if got := n.Names(ctx); got["730335604511"] != "renamed" || calls != 3 {
		t.Fatalf("refresh: %v, %d calls", got, calls)
	}
	// A failed refresh keeps the last good list.
	fail = true
	now = now.Add(2 * time.Hour)
	if got := n.Names(ctx); got["730335604511"] != "renamed" {
		t.Fatalf("a failed refresh must keep the last list: %v", got)
	}
}
