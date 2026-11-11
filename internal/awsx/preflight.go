package awsx

import (
	"context"
	"fmt"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	otypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

// MaxSCPsPerTarget is the AWS Organizations limit on SCPs attached directly to
// one root, OU, or account. It is a hard limit; see "Number of policies that you
// can attach to an entity" in the AWS Organizations quotas documentation
// (https://docs.aws.amazon.com/organizations/latest/userguide/orgs_reference_limits.html).
const MaxSCPsPerTarget = 10

// PoliciesForTargetAPI lists the SCPs attached to a target.
type PoliciesForTargetAPI interface {
	ListPoliciesForTarget(context.Context, *organizations.ListPoliciesForTargetInput, ...func(*organizations.Options)) (*organizations.ListPoliciesForTargetOutput, error)
}

// SlotReport is one target's SCP count.
type SlotReport struct {
	Target   config.Target
	Attached int
	// Full means a freeze policy cannot be attached: the target already has
	// the maximum number of SCPs, none of them a guardrail freeze policy.
	Full bool
}

// CheckSCPSlots reports, for each scope, how many SCPs are attached and
// whether a freeze policy would still fit. A target that already carries one
// of the freeze policies is not reported as full.
func CheckSCPSlots(ctx context.Context, api PoliciesForTargetAPI, targets []config.Target, freezePolicyNames map[string]bool) ([]SlotReport, error) {
	var out []SlotReport
	for _, t := range targets {
		var n int
		hasFreeze := false
		var token *string
		for {
			p, err := api.ListPoliciesForTarget(ctx, &organizations.ListPoliciesForTargetInput{
				TargetId: aws.String(t.ID), Filter: otypes.PolicyTypeServiceControlPolicy, NextToken: token,
			})
			if err != nil {
				return nil, fmt.Errorf("list SCPs on %s: %w", t, err)
			}
			for _, pol := range p.Policies {
				n++
				if freezePolicyNames[aws.ToString(pol.Name)] {
					hasFreeze = true
				}
			}
			if p.NextToken == nil {
				break
			}
			token = p.NextToken
		}
		out = append(out, SlotReport{Target: t, Attached: n, Full: n >= MaxSCPsPerTarget && !hasFreeze})
	}
	return out, nil
}
