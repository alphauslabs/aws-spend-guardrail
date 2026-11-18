package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// BlockPolicy is the stack's AI block SCP (add-ai-blocked D3): its content,
// and its attachment to the organization root.
type BlockPolicy interface {
	Content(ctx context.Context) (string, error)
	Update(ctx context.Context, content string) error
	AttachedToRoot(ctx context.Context) (bool, error)
	AttachToRoot(ctx context.Context) error
	DetachFromRoot(ctx context.Context) error
}

// MaxSCPChars is AWS's limit on an SCP's content.
const MaxSCPChars = 5120

// BlockPlaceholder is the content that denies nothing, as the stack creates
// the policy and as the guard leaves it when nothing is blocked.
const BlockPlaceholder = `{"Version":"2012-10-17","Statement":[{"Sid":"GuardrailAIBlockedNothing","Effect":"Allow","Action":"*","Resource":"*"}]}`

// What each blocked kind denies (add-ai-blocked D3, confirmed in task 1.1).
// Kinds with the same resources share a statement.
type blockGroup struct {
	name      string
	actions   []string
	resources []string
}

var blockGroups = map[string]blockGroup{
	config.BlockAgents:               {"Actions", []string{"bedrock:InvokeAgent", "bedrock:InvokeInlineAgent", "bedrock:InvokeFlow"}, []string{"*"}},
	config.BlockKnowledgeBases:       {"Actions", []string{"bedrock:Retrieve", "bedrock:RetrieveAndGenerate", "bedrock:AgenticRetrieveStream"}, []string{"*"}},
	config.BlockAsyncInvoke:          {"Async", []string{"bedrock:InvokeModel"}, []string{"arn:aws:bedrock:*:*:async-invoke/*"}},
	config.BlockBidirectionalStreams: {"Sonic", []string{"bedrock:InvokeModel", "bedrock:InvokeModelWithResponseStream"}, []string{"arn:aws:bedrock:*::foundation-model/*sonic*", "arn:aws:bedrock:*:*:inference-profile/*sonic*"}},
}

type scpStatement struct {
	Sid       string         `json:"Sid"`
	Effect    string         `json:"Effect"`
	Action    []string       `json:"Action"`
	Resource  []string       `json:"Resource"`
	Condition map[string]any `json:"Condition,omitempty"`
}

// blockContent builds the policy for each account's blocked kinds: one Deny
// statement per distinct set of kinds and resource group, each limited to its
// accounts. With nothing blocked it is the placeholder.
func blockContent(sets map[string][]string) string {
	bySet := map[string][]string{}
	for acct, kinds := range sets {
		if len(kinds) == 0 {
			continue
		}
		k := strings.Join(kinds, ",")
		bySet[k] = append(bySet[k], acct)
	}
	if len(bySet) == 0 {
		return BlockPlaceholder
	}
	keys := make([]string, 0, len(bySet))
	for k := range bySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var stmts []scpStatement
	for i, k := range keys {
		accts := bySet[k]
		sort.Strings(accts)
		groups := map[string]*blockGroup{}
		var order []string
		for _, kind := range strings.Split(k, ",") {
			g := blockGroups[kind]
			if have, ok := groups[g.name]; ok {
				have.actions = append(have.actions, g.actions...)
				continue
			}
			c := g
			c.actions = append([]string(nil), g.actions...)
			groups[g.name] = &c
			order = append(order, g.name)
		}
		sort.Strings(order)
		for _, name := range order {
			g := groups[name]
			stmts = append(stmts, scpStatement{
				Sid: fmt.Sprintf("GuardrailAIBlocked%d%s", i+1, name), Effect: "Deny", Action: uniqSorted(g.actions), Resource: g.resources,
				Condition: map[string]any{"StringEquals": map[string]any{"aws:PrincipalAccount": accts}},
			})
		}
	}
	raw, _ := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": stmts})
	return string(raw)
}

func uniqSorted(in []string) []string {
	set := map[string]bool{}
	for _, v := range in {
		set[v] = true
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// aiBlockedSets maps each account an enforce AI scope covers to its blocked
// kinds, with the account-beats-OU rule of aiCaps.
func (g *Guard) aiBlockedSets(ctx context.Context, cfg *config.ResolvedAI) (map[string][]string, error) {
	out := map[string][]string{}
	if cfg == nil {
		return out, nil
	}
	blocked := func(s config.ResolvedAIScope) []string {
		var kinds []string
		for _, k := range s.Blocked {
			if s.Blocks(k) {
				kinds = append(kinds, k)
			}
		}
		return kinds
	}
	for _, s := range cfg.Scopes {
		if s.Target.Kind != "ou" {
			continue
		}
		members, err := g.Org.AccountsInOU(ctx, s.Target.ID)
		if err != nil {
			return nil, fmt.Errorf("list accounts in %s: %w", s.Target.ID, err)
		}
		for _, m := range members {
			out[m] = blocked(s)
		}
	}
	for _, s := range cfg.Scopes {
		if s.Target.Kind == "account" {
			out[s.Target.ID] = blocked(s)
		}
	}
	for a, k := range out {
		if len(k) == 0 {
			delete(out, a)
		}
	}
	return out, nil
}

// metaBlocked is what the guard last wrote to the block policy.
const metaBlocked = "ai.blocked"

type blockedState struct {
	Hash     string              `json:"hash,omitempty"`
	Attached bool                `json:"attached"`
	Sets     map[string][]string `json:"sets,omitempty"`
	Failing  string              `json:"failing,omitempty"`
	// TooLarge is how many accounts didn't fit when the policy was too large.
	TooLarge int `json:"too_large,omitempty"`
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// reconcileBlocked keeps the block policy in step with the config in force
// (add-ai-blocked D5): rewrites it when what is blocked changed, attaches it
// to the root while something is blocked and detaches it otherwise. Failures
// are alerted once per failing stretch, and retried on every pull.
func (g *Guard) reconcileBlocked(ctx context.Context, eff *config.Resolved, version int64) {
	if g.Blocks == nil {
		return
	}
	var st blockedState
	if _, err := g.State.Meta(ctx, metaBlocked, &st); err != nil {
		return
	}
	var cfg *config.ResolvedAI
	if eff != nil {
		cfg = eff.AI
	}
	sets, err := g.aiBlockedSets(ctx, cfg)
	if err != nil {
		g.blockedFailed(ctx, &st, "The guard can't work out which accounts to block.", err)
		return
	}
	content := blockContent(sets)
	if len(content) > MaxSCPChars {
		missing := 0
		for a := range sets {
			if _, ok := st.Sets[a]; !ok {
				missing++
			}
		}
		if st.TooLarge == 0 {
			g.alert(ctx, "guardrail: AI block policy too large",
				fmt.Sprintf("Blocking what the config asks would take a policy of %d characters, over AWS's limit of %d, so the guard keeps the block policy as it was. %d account(s) with blocks aren't added. Block by OU or for fewer accounts (see docs/ai-guard.md).", len(content), MaxSCPChars, missing))
		}
		st.TooLarge = max(missing, 1)
		_ = g.State.SetMeta(ctx, metaBlocked, st)
		return
	}
	st.TooLarge = 0
	want := len(sets) > 0
	h := hashOf(content)
	if h != st.Hash {
		if err := g.Blocks.Update(ctx, content); err != nil {
			g.blockedFailed(ctx, &st, "The guard can't update the AI block policy.", err)
			return
		}
	}
	if want != st.Attached || h != st.Hash {
		var err error
		if want {
			err = g.Blocks.AttachToRoot(ctx)
		} else {
			err = g.Blocks.DetachFromRoot(ctx)
		}
		if err != nil {
			g.blockedFailed(ctx, &st, "The guard can't attach the AI block policy to the organization root, or detach it.", err)
			return
		}
	}
	if h != st.Hash || want != st.Attached {
		g.emit(ctx, schema.KindAIBlocked, fmt.Sprintf("%s:%s", schema.KindAIBlocked, h[:12]), "", version,
			map[string]any{"blocked": sets, "added": blockedDiff(sets, st.Sets), "removed": blockedDiff(st.Sets, sets), "attached": want})
	}
	if st.Failing != "" {
		g.alert(ctx, "guardrail: AI block policy updated again", "The guard keeps the AI block policy in step with the config again.")
	}
	st.Hash, st.Attached, st.Sets, st.Failing = h, want, sets, ""
	_ = g.State.SetMeta(ctx, metaBlocked, st)
}

func (g *Guard) blockedFailed(ctx context.Context, st *blockedState, why string, err error) {
	if st.Failing == "" {
		g.alert(ctx, "guardrail: AI block policy not updated",
			withError(why+" What the config blocks may not be denied, or what it allows may still be denied. The guard retries on every pull.", err))
	}
	st.Failing = why
	_ = g.State.SetMeta(ctx, metaBlocked, *st)
}

// blockedDiff lists, per account, the kinds in a that aren't in b.
func blockedDiff(a, b map[string][]string) map[string][]string {
	out := map[string][]string{}
	for acct, kinds := range a {
		for _, k := range kinds {
			if !hasKind(b[acct], k) {
				out[acct] = append(out[acct], k)
			}
		}
	}
	return out
}

// checkBlocked, daily, compares the live policy with what the guard wrote, and
// restores it, alerting once, when it was changed by hand (add-ai-blocked D5).
func (g *Guard) checkBlocked(ctx context.Context) error {
	if g.Blocks == nil {
		return nil
	}
	var st blockedState
	if ok, err := g.State.Meta(ctx, metaBlocked, &st); err != nil || !ok || st.Hash == "" {
		return err
	}
	live, err := g.Blocks.Content(ctx)
	if err != nil {
		return fmt.Errorf("read the AI block policy: %w", err)
	}
	attached, err := g.Blocks.AttachedToRoot(ctx)
	if err != nil {
		return fmt.Errorf("read the AI block policy's attachment: %w", err)
	}
	if canonicalHash(live) == canonicalHash(blockContent(st.Sets)) && attached == st.Attached {
		return nil
	}
	var errs []error
	if err := g.Blocks.Update(ctx, blockContent(st.Sets)); err != nil {
		errs = append(errs, err)
	}
	if st.Attached && !attached {
		errs = append(errs, g.Blocks.AttachToRoot(ctx))
	} else if !st.Attached && attached {
		errs = append(errs, g.Blocks.DetachFromRoot(ctx))
	}
	g.alert(ctx, "guardrail: AI block policy changed outside the guard",
		"The AI block policy's content or its attachment to the organization root no longer matched what the config blocks, so the guard restored it. Change what is blocked through the config instead.")
	return errors.Join(errs...)
}

// canonicalHash hashes a policy's JSON after re-encoding it, since AWS may
// return it with other spacing.
func canonicalHash(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		if b, err := json.Marshal(v); err == nil {
			s = string(b)
		}
	}
	return hashOf(s)
}

func hasKind(list []string, k string) bool {
	for _, v := range list {
		if v == k {
			return true
		}
	}
	return false
}
