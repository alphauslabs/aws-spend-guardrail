package awsx

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
)

// BlockPolicyAPI is the part of Organizations the AI block policy uses.
type BlockPolicyAPI interface {
	DescribePolicy(context.Context, *organizations.DescribePolicyInput, ...func(*organizations.Options)) (*organizations.DescribePolicyOutput, error)
	UpdatePolicy(context.Context, *organizations.UpdatePolicyInput, ...func(*organizations.Options)) (*organizations.UpdatePolicyOutput, error)
	ListRoots(context.Context, *organizations.ListRootsInput, ...func(*organizations.Options)) (*organizations.ListRootsOutput, error)
	ListTargetsForPolicy(context.Context, *organizations.ListTargetsForPolicyInput, ...func(*organizations.Options)) (*organizations.ListTargetsForPolicyOutput, error)
	AttachPolicy(context.Context, *organizations.AttachPolicyInput, ...func(*organizations.Options)) (*organizations.AttachPolicyOutput, error)
	DetachPolicy(context.Context, *organizations.DetachPolicyInput, ...func(*organizations.Options)) (*organizations.DetachPolicyOutput, error)
}

// OrgBlockPolicy is the stack's AI block SCP (add-ai-blocked D3).
type OrgBlockPolicy struct {
	API      BlockPolicyAPI
	PolicyID string
	root     string
}

// Content returns the policy's content.
func (p *OrgBlockPolicy) Content(ctx context.Context) (string, error) {
	out, err := p.API.DescribePolicy(ctx, &organizations.DescribePolicyInput{PolicyId: aws.String(p.PolicyID)})
	if err != nil {
		return "", err
	}
	if out.Policy == nil {
		return "", fmt.Errorf("policy %s not found", p.PolicyID)
	}
	return aws.ToString(out.Policy.Content), nil
}

// Update replaces the policy's content.
func (p *OrgBlockPolicy) Update(ctx context.Context, content string) error {
	_, err := p.API.UpdatePolicy(ctx, &organizations.UpdatePolicyInput{PolicyId: aws.String(p.PolicyID), Content: aws.String(content)})
	return err
}

func (p *OrgBlockPolicy) rootID(ctx context.Context) (string, error) {
	if p.root != "" {
		return p.root, nil
	}
	out, err := p.API.ListRoots(ctx, &organizations.ListRootsInput{})
	if err != nil {
		return "", fmt.Errorf("find the organization root: %w", err)
	}
	if len(out.Roots) == 0 {
		return "", fmt.Errorf("the organization has no root")
	}
	p.root = aws.ToString(out.Roots[0].Id)
	return p.root, nil
}

// AttachedToRoot reports whether the policy is attached to the root.
func (p *OrgBlockPolicy) AttachedToRoot(ctx context.Context) (bool, error) {
	root, err := p.rootID(ctx)
	if err != nil {
		return false, err
	}
	in := &organizations.ListTargetsForPolicyInput{PolicyId: aws.String(p.PolicyID)}
	for {
		out, err := p.API.ListTargetsForPolicy(ctx, in)
		if err != nil {
			return false, err
		}
		for _, t := range out.Targets {
			if aws.ToString(t.TargetId) == root {
				return true, nil
			}
		}
		if out.NextToken == nil {
			return false, nil
		}
		in.NextToken = out.NextToken
	}
}

// AttachToRoot attaches the policy to the root; already attached is fine.
func (p *OrgBlockPolicy) AttachToRoot(ctx context.Context) error {
	root, err := p.rootID(ctx)
	if err != nil {
		return err
	}
	_, err = p.API.AttachPolicy(ctx, &organizations.AttachPolicyInput{PolicyId: aws.String(p.PolicyID), TargetId: aws.String(root)})
	if err != nil && strings.Contains(err.Error(), "DuplicatePolicyAttachment") {
		return nil
	}
	return err
}

// DetachFromRoot detaches the policy from the root; not attached is fine.
func (p *OrgBlockPolicy) DetachFromRoot(ctx context.Context) error {
	root, err := p.rootID(ctx)
	if err != nil {
		return err
	}
	_, err = p.API.DetachPolicy(ctx, &organizations.DetachPolicyInput{PolicyId: aws.String(p.PolicyID), TargetId: aws.String(root)})
	if err != nil && strings.Contains(err.Error(), "PolicyNotAttached") {
		return nil
	}
	return err
}
