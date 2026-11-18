package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// ai.*.blocked (add-ai-blocked D1): a scope's list replaces the defaults',
// [] blocks nothing, and only enforce scopes block.
func TestBlockedResolution(t *testing.T) {
	r := aiDoc(t, `{"defaults":{"mode":"enforce","blocked":["async_invoke","agents"]},"scopes":[
		{"account_id":"111122223333"},
		{"account_id":"444455556666","blocked":[]},
		{"account_id":"777788889999","blocked":["knowledge_bases"]},
		{"ou_id":"ou-ab12-cd34ef56","mode":"dry-run"}]}`).AI
	get := func(id string) ResolvedAIScope {
		t.Helper()
		kind := "account"
		if strings.HasPrefix(id, "ou-") {
			kind = "ou"
		}
		s, ok := r.Scope(Target{Kind: kind, ID: id})
		if !ok {
			t.Fatalf("no scope %s", id)
		}
		return s
	}
	if s := get("111122223333"); strings.Join(s.Blocked, ",") != "agents,async_invoke" || !s.Blocks(BlockAgents) || s.Blocks(BlockKnowledgeBases) {
		t.Errorf("inherited: %+v", s)
	}
	if s := get("444455556666"); s.Blocked != nil || s.Blocks(BlockAgents) {
		t.Errorf("[] blocks nothing: %+v", s)
	}
	if s := get("777788889999"); strings.Join(s.Blocked, ",") != "knowledge_bases" {
		t.Errorf("replaced: %+v", s)
	}
	if s := get("ou-ab12-cd34ef56"); len(s.Blocked) != 2 || s.Blocks(BlockAgents) {
		t.Errorf("a dry-run scope blocks nothing: %+v", s)
	}
	// Unset everywhere: nothing blocked.
	if s, _ := aiDoc(t, `{"scopes":[{"account_id":"111122223333","mode":"enforce"}]}`).AI.Scope(Target{Kind: "account", ID: "111122223333"}); s.Blocked != nil {
		t.Errorf("unset: %+v", s)
	}
}

func TestBlockedValidation(t *testing.T) {
	base := `{"payer_account_id":"123456789012","scopes":[],"ai":`
	for _, c := range []struct{ ai, field, msg string }{
		{`{"scopes":[{"account_id":"111122223333","blocked":["rerank"]}]}`, "ai.scopes[0].blocked[0]", "must be one of agents, knowledge_bases, bidirectional_streams, async_invoke"},
		{`{"defaults":{"blocked":["agents","agents"]}}`, "ai.defaults.blocked[1]", "listed twice"},
		{`{"defaults":{"blocked":"agents"}}`, "ai.defaults.blocked", "must be an array of strings"},
	} {
		if probs := problems(t, base+c.ai+`}`, Options{}); !hasProblem(probs, c.field, c.msg) {
			t.Errorf("%s: want %q at %q, got %v", c.ai, c.msg, c.field, probs)
		}
	}
}

// Blocking is a tightening; allowing is a loosening that waits, with a
// sentence saying the use isn't counted (add-ai-blocked D6).
func TestBlockedClassification(t *testing.T) {
	const acct = `{"account_id":"730335604511","mode":"enforce"`
	from := aiDoc(t, `{"scopes":[`+acct+`,"blocked":["agents"]}]}`)
	add := aiDoc(t, `{"scopes":[`+acct+`,"blocked":["agents","async_invoke"]}]}`)
	ch := only1(t, Diff(from, add))
	if ch.Field != FieldAIBlocked || ch.Direction != Tightening || ch.To != BlockAsyncInvoke || ch.Sentence() != "Block Bedrock async invokes on account 730335604511." {
		t.Fatalf("add: %+v %q", ch, ch.Sentence())
	}
	allow := aiDoc(t, `{"scopes":[`+acct+`,"blocked":[]}]}`)
	ch = only1(t, Diff(from, allow))
	if ch.Direction != Loosening || ch.From != BlockAgents || !strings.Contains(ch.Sentence(), "Allow Bedrock agents and flows on account 730335604511") || !strings.Contains(ch.Sentence(), "isn't counted toward the AI caps") {
		t.Fatalf("allow: %+v %q", ch, ch.Sentence())
	}
	// While the allow waits, what applies keeps the block.
	if s, _ := Stricter(from, allow).AI.Scope(Target{Kind: "account", ID: "730335604511"}); strings.Join(s.Blocked, ",") != "agents" {
		t.Fatalf("stricter: %+v", s)
	}
	if s, _ := Stricter(from, add).AI.Scope(Target{Kind: "account", ID: "730335604511"}); strings.Join(s.Blocked, ",") != "agents,async_invoke" {
		t.Fatalf("stricter add: %+v", s)
	}
}

// State saved by v0.2.6 has no blocked field: it loads as nothing blocked,
// and equals a document that doesn't set it.
func TestBlockedAbsentFromOldState(t *testing.T) {
	doc := aiDoc(t, `{"scopes":[{"account_id":"111122223333","mode":"enforce"}]}`)
	raw, _ := json.Marshal(doc.AI)
	old := strings.Replace(string(raw), `,"blocked":null`, "", 1)
	var back ResolvedAI
	if err := json.Unmarshal([]byte(old), &back); err != nil {
		t.Fatal(err)
	}
	if !equalAI(doc.AI, &back) || len(Diff(&Resolved{AI: &back, PayerAccountID: doc.PayerAccountID}, doc)) != 0 {
		t.Fatalf("old state differs: %s", old)
	}
}
