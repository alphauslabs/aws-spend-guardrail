package guard

import (
	"testing"

	approvalv1 "github.com/alphauslabs/jennah-sdk-go/jennah/approval/v1"
)

// loosen publishes a version that tightens X and loosens Y, and pulls it.
func loosen(h *harness, approvers []string, required int) {
	h.g.S.LooseningApprovers = approvers
	h.g.S.LooseningRequired = required
	h.publish(doc("50000", "50000"))
	h.pull()
	h.publish(doc("40000", "60000"))
	h.pull()
}

func TestApprovalQuorumFollowsTheStack(t *testing.T) {
	for _, tc := range []struct {
		name      string
		approvers []string
		required  int
		quorum    approvalv1.Quorum
		count     int32
	}{
		{"one of three", []string{"a@example.com", "b@example.com", "c@example.com"}, 1, approvalv1.Quorum_QUORUM_ANY_ONE, 1},
		{"two of two", []string{"a@example.com", "b@example.com"}, 2, approvalv1.Quorum_QUORUM_ALL, 2},
		{"blank and repeated approvers are not counted", []string{" a@example.com", "", "A@example.com", "b@example.com "}, 2, approvalv1.Quorum_QUORUM_ALL, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			loosen(h, tc.approvers, tc.required)
			a := h.jennah.Approval("guardrail:123456789012:msp_a:v2")
			if a == nil {
				t.Fatalf("no approval raised; alerts: %v", h.alerts.list)
			}
			if a.GetQuorum() != tc.quorum || a.GetRequiredApprovals() != tc.count {
				t.Fatalf("got %v with %d required", a.GetQuorum(), a.GetRequiredApprovals())
			}
		})
	}
}

func TestUnsupportedApprovalCountRaisesNothing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		approvers []string
		required  int
		says      string
	}{
		{"two of three", []string{"a@example.com", "b@example.com", "c@example.com"}, 2, "Jennah needs 1 (any one approver) or 3 (all of them)"},
		{"more than there are", []string{"a@example.com"}, 2, "Jennah needs 1."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			loosen(h, tc.approvers, tc.required)
			if h.jennah.CreatedApprovals != 0 {
				t.Fatalf("raised %d approvals", h.jennah.CreatedApprovals)
			}
			if !h.alerts.has(tc.says) {
				t.Fatalf("alerts don't say %q: %v", tc.says, h.alerts.list)
			}
			if h.ceiling(bX) != 40000 {
				t.Fatalf("the tightening should still apply, X is %v", h.ceiling(bX))
			}
			if h.ceiling(bY) != 50000 {
				t.Fatalf("the loosening must not apply, Y is %v", h.ceiling(bY))
			}
		})
	}
}
