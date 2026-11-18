package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

type fakeBlocks struct {
	content  string
	attached bool
	updates  int
	failAll  error
}

func (f *fakeBlocks) Content(context.Context) (string, error) { return f.content, f.failAll }
func (f *fakeBlocks) Update(_ context.Context, c string) error {
	if f.failAll != nil {
		return f.failAll
	}
	f.content = c
	f.updates++
	return nil
}
func (f *fakeBlocks) AttachedToRoot(context.Context) (bool, error) { return f.attached, f.failAll }
func (f *fakeBlocks) AttachToRoot(context.Context) error {
	if f.failAll != nil {
		return f.failAll
	}
	f.attached = true
	return nil
}
func (f *fakeBlocks) DetachFromRoot(context.Context) error {
	if f.failAll != nil {
		return f.failAll
	}
	f.attached = false
	return nil
}

// policyOf parses the policy's statements.
type testStatement struct {
	Effect    string
	Action    any
	Resource  any
	Condition map[string]any
}

func policyOf(t *testing.T, s string) []testStatement {
	t.Helper()
	var p struct{ Statement []testStatement }
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatal(err)
	}
	return p.Statement
}

// Statements per set of kinds and resource group, every deny limited to its
// accounts, and the placeholder denying nothing (add-ai-blocked D3).
func TestBlockContent(t *testing.T) {
	if c := blockContent(nil); c != BlockPlaceholder {
		t.Fatalf("nothing blocked: %s", c)
	}
	for _, st := range policyOf(t, BlockPlaceholder) {
		if st.Effect != "Allow" {
			t.Fatalf("the placeholder must deny nothing: %+v", st)
		}
	}
	c := blockContent(map[string][]string{
		"111122223333": {"agents", "async_invoke"},
		"444455556666": {"agents", "async_invoke"},
		"777788889999": {"bidirectional_streams", "knowledge_bases"},
	})
	stmts := policyOf(t, c)
	if len(stmts) != 4 {
		t.Fatalf("want 4 statements (2 sets, 2 resource groups each), got %d: %s", len(stmts), c)
	}
	for _, st := range stmts {
		accts, _ := st.Condition["StringEquals"].(map[string]any)["aws:PrincipalAccount"].([]any)
		if st.Effect != "Deny" || len(accts) == 0 {
			t.Fatalf("every deny must name its accounts: %+v", st)
		}
	}
	if !strings.Contains(c, `"arn:aws:bedrock:*:*:async-invoke/*"`) || !strings.Contains(c, "foundation-model/*sonic*") || !strings.Contains(c, "bedrock:InvokeInlineAgent") || !strings.Contains(c, "bedrock:RetrieveAndGenerate") {
		t.Fatalf("content %s", c)
	}
	// About 300 accounts fit; many more don't.
	many := map[string][]string{}
	for i := 0; i < 400; i++ {
		many[strings.Repeat("1", 9)+string(rune('0'+i/100))+string(rune('0'+i/10%10))+string(rune('0'+i%10))] = []string{"agents"}
	}
	if len(blockContent(many)) <= MaxSCPChars {
		t.Fatal("400 accounts should not fit in one policy")
	}
}

func blockedHarness(t *testing.T) (*harness, *fakeBlocks) {
	h := coverageHarness(t)
	withFast(t, h)
	b := &fakeBlocks{content: BlockPlaceholder}
	h.g.Blocks = b
	h.org.ous = map[string][]string{"ou-ab12-cd34ef56": {"444455556666", "555566667777"}}
	return h, b
}

const blockedConfig = `{"payer_account_id":"` + payer + `","scopes":[{"account_id":"111122223333","ceiling_usd":100,"mode":"enforce"},{"account_id":"444455556666","ceiling_usd":100,"mode":"enforce"}],"ai":{"defaults":{"mode":"enforce","blocked":["agents"]},"scopes":[`

// The policy follows the config: written and attached when something is
// blocked, an account scope beating its OU, a dry-run scope blocking nothing,
// and detached when nothing is (add-ai-blocked D5).
func TestBlockedReconcile(t *testing.T) {
	h, b := blockedHarness(t)
	h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56","blocked":["agents","async_invoke"]},{"account_id":"555566667777","blocked":[]},{"account_id":"111122223333","mode":"dry-run"}]}}`)
	h.pull()
	if !b.attached || b.updates != 1 {
		t.Fatalf("attached %v, %d updates", b.attached, b.updates)
	}
	c := b.content
	if !strings.Contains(c, "444455556666") || strings.Contains(c, "555566667777") || strings.Contains(c, "111122223333") {
		t.Fatalf("OU member blocked, account override allowed, dry-run blocking nothing: %s", c)
	}
	if n := len(eventsOf(h, schema.KindAIBlocked)); n != 1 {
		t.Fatalf("ai_blocked events: %d", n)
	}
	h.pull()
	if b.updates != 1 || len(eventsOf(h, schema.KindAIBlocked)) != 1 {
		t.Fatalf("an unchanged config must write nothing: %d updates", b.updates)
	}
	// Allowing everything is a loosening: still blocked while it waits, then
	// detached once approved and past the delay.
	v := h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56","blocked":[]}],"defaults":{"mode":"enforce","blocked":[]}}}`)
	h.pull()
	if !b.attached {
		t.Fatal("an allow waiting for approval must keep the block")
	}
	approveAndWait(h, fmt.Sprint(v), 10*time.Minute)
	h.now = h.now.Add(time.Hour)
	h.pull()
	if b.attached || b.content != BlockPlaceholder {
		t.Fatalf("nothing blocked: attached %v, %s", b.attached, b.content)
	}
}

// A failure alerts once, retries, and alerts when it works again.
func TestBlockedFailureAlertsOnce(t *testing.T) {
	h, b := blockedHarness(t)
	b.failAll = errors.New("AccessDeniedException: not authorized")
	h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56"}]}}`)
	h.pull()
	h.pull()
	if n := countAlerts(h, "AI block policy not updated"); n != 1 {
		t.Fatalf("failure alerts: %d", n)
	}
	b.failAll = nil
	h.pull()
	if !b.attached || countAlerts(h, "AI block policy updated again") != 1 {
		t.Fatalf("recovery: attached %v, alerts %v", b.attached, h.alerts.list)
	}
}

// Too large keeps the last policy and alerts once.
func TestBlockedTooLarge(t *testing.T) {
	h, b := blockedHarness(t)
	var members []string
	for i := 0; i < 400; i++ {
		members = append(members, "9"+strings.Repeat("0", 8)+string(rune('0'+i/100))+string(rune('0'+i/10%10))+string(rune('0'+i%10)))
	}
	h.org.ous["ou-ab12-cd34ef56"] = members
	h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56"}]}}`)
	h.pull()
	h.pull()
	if b.updates != 0 || b.attached || countAlerts(h, "AI block policy too large") != 1 {
		t.Fatalf("updates %d, attached %v, alerts %v", b.updates, b.attached, h.alerts.list)
	}
}

// The daily check restores a policy changed by hand, alerting once.
func TestBlockedHandEditRestored(t *testing.T) {
	h, b := blockedHarness(t)
	h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56"}]}}`)
	h.pull()
	want := b.content
	b.content, b.attached = BlockPlaceholder, false
	daily(h)
	if b.content != want || !b.attached || countAlerts(h, "changed outside the guard") != 1 {
		t.Fatalf("restored %v, attached %v, alerts %v", b.content == want, b.attached, h.alerts.list)
	}
	daily(h)
	if countAlerts(h, "changed outside the guard") != 1 {
		t.Fatal("an intact policy must not alert")
	}
}

// Deleting the stack detaches the block policy first, since AWS won't delete
// an attached policy.
func TestTeardownDetachesBlockPolicy(t *testing.T) {
	h, b := blockedHarness(t)
	h.publish(blockedConfig + `{"ou_id":"ou-ab12-cd34ef56"}]}}`)
	h.pull()
	if !b.attached {
		t.Fatal("not attached")
	}
	if err := h.g.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.attached {
		t.Fatal("teardown must detach the block policy")
	}
}

var _ = config.BlockAgents
