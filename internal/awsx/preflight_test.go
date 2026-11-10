package awsx

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	otypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"

	"github.com/alphauslabs/aws-spend-guardrail/config"
)

type fakePolicies map[string][]string

func (f fakePolicies) ListPoliciesForTarget(_ context.Context, in *organizations.ListPoliciesForTargetInput, _ ...func(*organizations.Options)) (*organizations.ListPoliciesForTargetOutput, error) {
	var ps []otypes.PolicySummary
	for _, n := range f[aws.ToString(in.TargetId)] {
		ps = append(ps, otypes.PolicySummary{Name: aws.String(n)})
	}
	return &organizations.ListPoliciesForTargetOutput{Policies: ps}, nil
}

func TestTargetWithFiveSCPsIsFull(t *testing.T) {
	api := fakePolicies{
		"111122223333":     {"FullAWSAccess", "a", "b", "c", "d"},
		"444455556666":     {"FullAWSAccess"},
		"ou-ab12-cd34ef56": {"FullAWSAccess", "a", "b", "c", "guardrail-freeze-b-msp_a"},
	}
	targets := []config.Target{
		{Kind: "account", ID: "111122223333"},
		{Kind: "account", ID: "444455556666"},
		{Kind: "ou", ID: "ou-ab12-cd34ef56"},
	}
	got, err := CheckSCPSlots(context.Background(), api, targets, map[string]bool{"guardrail-freeze-b-msp_a": true})
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Full || got[0].Attached != 5 {
		t.Fatalf("5 SCPs should be full: %+v", got[0])
	}
	if got[1].Full {
		t.Fatal("1 SCP is not full")
	}
	if got[2].Full {
		t.Fatal("a target already holding a freeze policy is not full")
	}
}
