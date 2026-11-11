package awsx

import (
	"context"
	"fmt"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	otypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

type fakePolicies map[string][]string

func (f fakePolicies) ListPoliciesForTarget(_ context.Context, in *organizations.ListPoliciesForTargetInput, _ ...func(*organizations.Options)) (*organizations.ListPoliciesForTargetOutput, error) {
	var ps []otypes.PolicySummary
	for _, n := range f[aws.ToString(in.TargetId)] {
		ps = append(ps, otypes.PolicySummary{Name: aws.String(n)})
	}
	return &organizations.ListPoliciesForTargetOutput{Policies: ps}, nil
}

func scps(n int, extra ...string) []string {
	out := []string{"FullAWSAccess"}
	for i := 1; len(out) < n-len(extra); i++ {
		out = append(out, fmt.Sprintf("p%d", i))
	}
	return append(out, extra...)
}

func TestSCPSlotLimitIsTen(t *testing.T) {
	const freeze = "guardrail-freeze-b-msp_a"
	api := fakePolicies{
		"111122223333":     scps(5),
		"444455556666":     scps(10),
		"ou-ab12-cd34ef56": scps(10, freeze),
	}
	targets := []config.Target{
		{Kind: "account", ID: "111122223333"},
		{Kind: "ou", ID: "444455556666"},
		{Kind: "ou", ID: "ou-ab12-cd34ef56"},
	}
	got, err := CheckSCPSlots(context.Background(), api, targets, map[string]bool{freeze: true})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Full || got[0].Attached != 5 {
		t.Fatalf("5 SCPs leave free slots: %+v", got[0])
	}
	if !got[1].Full || got[1].Attached != 10 {
		t.Fatalf("10 SCPs, none a freeze policy, is full: %+v", got[1])
	}
	if got[2].Full || got[2].Attached != 10 {
		t.Fatalf("a target already holding a freeze policy is not full: %+v", got[2])
	}
}
