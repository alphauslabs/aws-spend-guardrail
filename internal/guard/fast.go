package guard

import (
	"context"
	"fmt"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/packs/ai"
)

// FastPath is the guard's trail-reading fast path (the AI pack). Nil
// disables it.
type FastPath struct {
	Engine *activity.Engine
	Prices *ai.Table
	// AIPolicyID is the AI deny SCP the stack created.
	AIPolicyID string
}

// Trail runs one tick of the fast path on the config in force. With no AI
// section in force, it does nothing.
func (g *Guard) Trail(ctx context.Context) error {
	if g.Fast == nil {
		return nil
	}
	// Write a heartbeat if this run changed the guard's state (add-heartbeat-state).
	defer g.heartbeatIfChanged(ctx)
	st, err := g.State.Load(ctx)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if st.Effective == nil || st.Effective.AI == nil {
		return nil
	}
	cfg := st.Effective.AI
	caps, err := g.aiCaps(ctx, cfg)
	if err != nil {
		return err
	}
	lifts := map[string]bool{}
	for _, l := range cfg.Lifts {
		lifts[l] = true
	}
	e := g.Fast.Engine
	e.Emit = func(ctx context.Context, kind, id, scope string, detail map[string]any) {
		g.emit(ctx, kind, id, scope, st.EffectiveVersion, detail)
	}
	e.Alert = g.alert
	if e.Now == nil && g.Now != nil {
		e.Now = g.Now
	}
	shared, priceVersion := g.sharedAIPrices(ctx)
	run := activity.PackRun{
		Name:           aiPack,
		Pack:           ai.NewShared(g.Fast.Prices, shared, cfg),
		Detail:         map[string]any{"price_version": priceVersion},
		PolicyID:       g.Fast.AIPolicyID,
		CapsFor:        func(a string) (activity.Caps, bool) { c, ok := caps[a]; return c, ok },
		DefaultEnforce: cfg.Mode == config.ModeEnforce,
		Lifts:          lifts,
	}
	return e.Tick(ctx, []activity.PackRun{run})
}

// aiCaps maps each covered account to its caps. An account scope beats an OU
// scope; OU scopes cover every account in the OU and the OUs below it.
func (g *Guard) aiCaps(ctx context.Context, cfg *config.ResolvedAI) (map[string]activity.Caps, error) {
	out := map[string]activity.Caps{}
	toCaps := func(s config.ResolvedAIScope) activity.Caps {
		return activity.Caps{Hourly: s.HourlyUSD, Daily: s.DailyUSD, Monthly: s.MonthlyUSD, Enforce: s.Mode == config.ModeEnforce}
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
			out[m] = toCaps(s)
		}
	}
	for _, s := range cfg.Scopes {
		if s.Target.Kind == "account" {
			out[s.Target.ID] = toCaps(s)
		}
	}
	return out, nil
}
