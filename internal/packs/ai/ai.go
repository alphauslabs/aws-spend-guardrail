// Package ai is the AI pack: it prices Amazon Bedrock runtime calls from
// their token counts and trips on calls that shouldn't happen at all.
package ai

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
)

//go:embed prices.json
var pricesJSON []byte

// Table is a price table: the release's, or one region's shared chunk.
type Table struct {
	Source    string `json:"source"`
	Published string `json:"published"`
	Basis     string `json:"basis"`
	// Models are a chunk's prices. In the release's table they are the
	// us-east-1 prices plus models not offered there at their highest, as
	// published for guards of earlier releases (add-regional-prices D2).
	Models map[string]config.ModelPrice `json:"models"`
	// Regions are the release's prices per region (add-regional-prices D1).
	// A table without them is us-east-1's.
	Regions map[string]map[string]config.ModelPrice `json:"regions,omitempty"`
	// Tiers are a chunk's service tier prices, by model and tier, beside
	// Models' standard prices (add-tier-pricing D2).
	Tiers map[string]map[string]config.ModelPrice `json:"tiers,omitempty"`
	// RegionTiers are the release's tier prices per region, by model and
	// tier.
	RegionTiers map[string]map[string]map[string]config.ModelPrice `json:"region_tiers,omitempty"`
}

// DefaultRegion is the region a table without regions prices.
const DefaultRegion = "us-east-1"

// TiersByRegion returns the table's tier prices per region, by model and
// tier (add-tier-pricing D2).
func (t *Table) TiersByRegion() map[string]map[string]map[string]config.ModelPrice {
	if len(t.RegionTiers) > 0 {
		return t.RegionTiers
	}
	if len(t.Tiers) > 0 {
		return map[string]map[string]map[string]config.ModelPrice{DefaultRegion: t.Tiers}
	}
	return nil
}

// ByRegion returns the table's prices per region.
func (t *Table) ByRegion() map[string]map[string]config.ModelPrice {
	if len(t.Regions) > 0 {
		return t.Regions
	}
	return map[string]map[string]config.ModelPrice{DefaultRegion: t.Models}
}

// Prices returns the price table shipped with this release.
func Prices() (*Table, error) {
	var t Table
	if err := json.Unmarshal(pricesJSON, &t); err != nil {
		return nil, fmt.Errorf("price table: %w", err)
	}
	return &t, nil
}

// The Bedrock runtime calls that are priced, and the logging-configuration
// calls that trip.
var (
	// ChatCompletions and Responses are the OpenAI-compatible API on
	// bedrock-runtime (add-tier-pricing D8).
	runtimeCalls = map[string]bool{"InvokeModel": true, "InvokeModelWithResponseStream": true, "Converse": true, "ConverseStream": true, "ChatCompletions": true, "Responses": true}
	streaming    = map[string]bool{"InvokeModelWithResponseStream": true, "ConverseStream": true}
	loggingCalls = map[string]bool{"PutModelInvocationLoggingConfiguration": true, "DeleteModelInvocationLoggingConfiguration": true}

	// versionSuffix is a model id's version: -v1:0, -1:0, or
	// -20251001-v1:0 (add-mantle-usage D4).
	versionSuffix = regexp.MustCompile(`-(\d{8}-)?v?\d+:\d+$`)
	geoPrefix     = regexp.MustCompile(`^(us|eu|apac|ap|jp|au|ca|global|us-gov)\.`)
	ctxSuffix     = regexp.MustCompile(`:[0-9]+k$`)
	assumedArn    = regexp.MustCompile(`^arn:(aws[a-z-]*):sts::([0-9]{12}):assumed-role/([^/]+)/`)
)

// ModelKey reduces a model id, inference profile id, or ARN to the model id
// the price table uses. An application inference profile ARN can't be
// resolved from the trail and is returned whole.
func ModelKey(id string) string {
	if strings.HasPrefix(id, "arn:") {
		if strings.Contains(id, ":application-inference-profile/") {
			return id
		}
		id = id[strings.LastIndex(id, "/")+1:]
	}
	id = geoPrefix.ReplaceAllString(id, "")
	return ctxSuffix.ReplaceAllString(id, "")
}

// callerMatches reports whether a call's caller is one of the allowed IAM
// users or roles; an assumed-role session matches its role.
func callerMatches(caller string, allowed []string) bool {
	role := caller
	if m := assumedArn.FindStringSubmatch(caller); m != nil {
		role = fmt.Sprintf("arn:%s:iam::%s:role/%s", m[1], m[2], m[3])
	}
	for _, a := range allowed {
		if a == caller || a == role {
			return true
		}
	}
	return false
}

// Pack prices and checks Bedrock calls against one config.
type Pack struct {
	release map[string]map[string]config.ModelPrice // per region: the release's prices
	shared  map[string]map[string]config.ModelPrice // per region: the enterprise's shared prices
	// releaseTiers and sharedTiers are the tier prices per region, model,
	// and tier (add-tier-pricing D2, D4).
	releaseTiers, sharedTiers map[string]map[string]map[string]config.ModelPrice
	// inRelease lists every model the release prices in some region.
	inRelease map[string]bool
	// floors are per region and provider, and per provider in floorsAny: the
	// release's highest prices (add-shared-prices D3, add-regional-prices D4).
	floors    map[string]map[string]config.ModelPrice
	floorsAny map[string]config.ModelPrice
	ceiling   config.ModelPrice // the release's highest prices overall
	// highest is each model's highest in-region price across regions, the
	// price where its call's region has none (add-regional-prices D4).
	highest map[string]config.ModelPrice
	// unknown is what a call to an unpriced model is counted at under
	// ai.unknown_models count: the highest input and output prices known from
	// the release, shared, and override tables in any region
	// (add-unknown-model-policy D2).
	unknown config.ModelPrice
	cfg     *config.ResolvedAI
	allowed map[string]bool // normalized model allowlist; nil means any priced model
	// allowedBase is the allowlist without version suffixes, which mantle
	// model ids lack (add-mantle-usage D4).
	allowedBase map[string]bool
}

// New returns the AI pack for a config in force, pricing from the release
// table only.
func New(t *Table, cfg *config.ResolvedAI) *Pack { return NewShared(t, nil, cfg) }

// NewShared returns the AI pack with the enterprise's shared prices, per
// region, merged tighten-only (design D3 of add-shared-prices): a shared price
// can raise a release price but never lower it, and a model only the shared
// tables list is priced at no less than its provider's highest release price.
func NewShared(t *Table, shared map[string]map[string]config.ModelPrice, cfg *config.ResolvedAI) *Pack {
	return NewTiered(t, shared, nil, cfg)
}

// NewTiered is NewShared with the shared tier prices, per region, model, and
// tier, merged tighten-only per tier like standard prices (add-tier-pricing
// D4).
func NewTiered(t *Table, shared map[string]map[string]config.ModelPrice, sharedTiers map[string]map[string]map[string]config.ModelPrice, cfg *config.ResolvedAI) *Pack {
	if cfg == nil {
		cfg = &config.ResolvedAI{}
	}
	p := &Pack{release: t.ByRegion(), shared: shared, releaseTiers: t.TiersByRegion(), sharedTiers: sharedTiers, cfg: cfg, inRelease: map[string]bool{},
		floors: map[string]map[string]config.ModelPrice{}, floorsAny: map[string]config.ModelPrice{}, highest: map[string]config.ModelPrice{}}
	for region, models := range p.release {
		p.floors[region] = map[string]config.ModelPrice{}
		for id, v := range models {
			p.inRelease[id] = true
			p.floors[region][provider(id)] = higher(p.floors[region][provider(id)], v)
			p.floorsAny[provider(id)] = higher(p.floorsAny[provider(id)], v)
			p.ceiling = higher(p.ceiling, v)
		}
	}
	regions := map[string]bool{}
	for r := range p.release {
		regions[r] = true
	}
	for r := range shared {
		regions[r] = true
	}
	for r := range regions {
		ids := map[string]bool{}
		for id := range p.release[r] {
			ids[id] = true
		}
		for id := range shared[r] {
			ids[id] = true
		}
		for id := range ids {
			if v, ok := p.inRegion(id, r); ok {
				p.highest[id] = higher(p.highest[id], v)
			}
		}
	}
	p.unknown = p.ceiling
	for _, v := range p.highest {
		p.unknown = higher(p.unknown, v)
	}
	for _, v := range cfg.Prices {
		p.unknown = higher(p.unknown, v)
	}
	if cfg.Models != nil {
		p.allowed, p.allowedBase = map[string]bool{}, map[string]bool{}
		for _, m := range cfg.Models {
			p.allowed[ModelKey(m)] = true
			p.allowedBase[versionSuffix.ReplaceAllString(ModelKey(m), "")] = true
		}
	}
	return p
}

// higher is the higher of two prices, input and output separately.
func higher(a, b config.ModelPrice) config.ModelPrice {
	return config.ModelPrice{InputPer1K: max(a.InputPer1K, b.InputPer1K), OutputPer1K: max(a.OutputPer1K, b.OutputPer1K)}
}

// Floor is the least a guard of this release charges in us-east-1 for a model
// only the shared table lists (design D3 of add-shared-prices): the highest
// input and output prices of the model's provider in us-east-1, else in any
// region, else of the whole table. Publishing uses it to warn when a manual
// price is below what guards will use.
func (t *Table) Floor(id string) config.ModelPrice {
	var inDefault, anyRegion, all config.ModelPrice
	foundDefault, foundAny := false, false
	for region, models := range t.ByRegion() {
		for m, v := range models {
			all = higher(all, v)
			if provider(m) != provider(id) {
				continue
			}
			foundAny = true
			anyRegion = higher(anyRegion, v)
			if region == DefaultRegion {
				foundDefault = true
				inDefault = higher(inDefault, v)
			}
		}
	}
	switch {
	case foundDefault:
		return inDefault
	case foundAny:
		return anyRegion
	}
	return all
}

// provider is the part of a model id before its first dot.
func provider(id string) string {
	if i := strings.Index(id, "."); i > 0 {
		return id[:i]
	}
	return id
}

// inRegion is a model's price in one region from the release and shared
// tables, merged tighten-only; a model only the shared tables list is raised
// to its provider's floor in that region, or in any region.
func (p *Pack) inRegion(key, region string) (config.ModelPrice, bool) {
	r, inRelease := p.release[region][key]
	s, inShared := p.shared[region][key]
	switch {
	case inRelease && inShared:
		return higher(r, s), true
	case inRelease:
		return r, true
	case inShared:
		if p.inRelease[key] {
			return s, true // the release prices it elsewhere; this is its regional price
		}
		floor, ok := p.floors[region][provider(key)]
		if !ok {
			if floor, ok = p.floorsAny[provider(key)]; !ok {
				floor = p.ceiling
			}
		}
		return higher(s, floor), true
	}
	return config.ModelPrice{}, false
}

// KnownPrice is a model's price from the release and shared tables, without
// the provider floor or the config's ai.prices: in each region that lists
// it, merged tighten-only, then the lowest input and the lowest output across
// those regions (add-price-override-check D1). ok is false when no table
// lists the model.
func (p *Pack) KnownPrice(key string) (config.ModelPrice, bool) {
	var low config.ModelPrice
	found := false
	regions := map[string]bool{}
	for r := range p.release {
		regions[r] = true
	}
	for r := range p.shared {
		regions[r] = true
	}
	for r := range regions {
		rel, inRelease := p.release[r][key]
		sh, inShared := p.shared[r][key]
		if !inRelease && !inShared {
			continue
		}
		v := higher(rel, sh)
		if !found {
			low, found = v, true
			continue
		}
		low = config.ModelPrice{InputPer1K: min(low.InputPer1K, v.InputPer1K), OutputPer1K: min(low.OutputPer1K, v.OutputPer1K)}
	}
	return low, found
}

// PriceIn returns a model's price for a call made in a region
// (add-regional-prices D4): the payer org's config override first (each org
// approves its own), then the release and shared prices in that region, then
// the model's highest price in any region.
func (p *Pack) PriceIn(key, region string) (config.ModelPrice, bool) {
	for k, v := range p.cfg.Prices {
		if ModelKey(k) == key {
			return v, true
		}
	}
	if v, ok := p.inRegion(key, region); ok {
		return v, true
	}
	v, ok := p.highest[key]
	return v, ok
}

// tierIn is a model's price for one tier in a region, from the release and
// shared tier prices merged tighten-only (add-tier-pricing D4).
func (p *Pack) tierIn(key, region, tier string) (config.ModelPrice, bool) {
	r, inRelease := p.releaseTiers[region][key][tier]
	s, inShared := p.sharedTiers[region][key][tier]
	switch {
	case inRelease && inShared:
		return higher(r, s), true
	case inRelease:
		return r, true
	case inShared:
		return s, true
	}
	return config.ModelPrice{}, false
}

// tiersOf lists the tiers a model has prices for in a region, or, when it has
// none there, in any region.
func (p *Pack) tiersOf(key, region string) []string {
	seen := map[string]bool{}
	for _, all := range []map[string]map[string]map[string]config.ModelPrice{p.releaseTiers, p.sharedTiers} {
		for t := range all[region][key] {
			seen[t] = true
		}
	}
	if len(seen) == 0 {
		for _, all := range []map[string]map[string]map[string]config.ModelPrice{p.releaseTiers, p.sharedTiers} {
			for _, models := range all {
				for t := range models[key] {
					seen[t] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// tierPrice is a model's price for a tier in a region, or, when it has none
// there, its highest for that tier in any region.
func (p *Pack) tierPrice(key, region, tier string) (config.ModelPrice, bool) {
	if v, ok := p.tierIn(key, region, tier); ok {
		return v, true
	}
	var hi config.ModelPrice
	found := false
	regions := map[string]bool{}
	for _, all := range []map[string]map[string]map[string]config.ModelPrice{p.releaseTiers, p.sharedTiers} {
		for r := range all {
			regions[r] = true
		}
	}
	for r := range regions {
		if v, ok := p.tierIn(key, r, tier); ok {
			hi, found = higher(hi, v), true
		}
	}
	return hi, found
}

// TierUse says how a call's tier is known (add-tier-pricing D3, D4).
type TierUse int

const (
	// TierNamed: the record names the tier, or names none, which is the
	// standard tier.
	TierNamed TierUse = iota
	// TierUnknown: the record can't show the tier, as for usage read from
	// metrics, or a stream completion whose call record wasn't seen.
	TierUnknown
)

// PriceFor returns a model's price for a call made in a region on a tier
// (add-tier-pricing D4). With no tier, it is PriceIn. A named tier the tables
// price for the model is that tier's price, raised to at least the org's
// ai.prices entry; a tier the call can't show, or one the tables don't price
// for the model, is the highest of its standard and tier prices, and
// unpricedTier says the latter. ok is false when the model has no price.
func (p *Pack) PriceFor(key, region, tier string, use TierUse) (price config.ModelPrice, ok, unpricedTier bool) {
	std, ok := p.PriceIn(key, region)
	if !ok {
		return config.ModelPrice{}, false, false
	}
	if use == TierNamed && tier == "" {
		return std, true, false
	}
	override, hasOverride := p.override(key)
	if use == TierNamed {
		if v, found := p.tierPrice(key, region, tier); found {
			if hasOverride {
				v = higher(v, override)
			}
			return v, true, false
		}
		unpricedTier = true
	}
	hi := std
	for _, t := range p.tiersOf(key, region) {
		if v, found := p.tierPrice(key, region, t); found {
			hi = higher(hi, v)
		}
	}
	return hi, true, unpricedTier
}

// override is the org's ai.prices entry for a model.
func (p *Pack) override(key string) (config.ModelPrice, bool) {
	for k, v := range p.cfg.Prices {
		if ModelKey(k) == key {
			return v, true
		}
	}
	return config.ModelPrice{}, false
}

// Price returns a model's price in us-east-1, or its highest price in any
// region when it has none there.
func (p *Pack) Price(key string) (config.ModelPrice, bool) {
	return p.PriceIn(key, DefaultRegion)
}

// SharedEffect counts how a shared table changes the release's prices:
// models it raises, models it adds, and models it lists lower (ignored).
func SharedEffect(release, shared map[string]config.ModelPrice) (raised, added, ignored int) {
	for id, s := range shared {
		r, ok := release[id]
		switch {
		case !ok:
			added++
		case s.InputPer1K > r.InputPer1K || s.OutputPer1K > r.OutputPer1K:
			raised++
		case s.InputPer1K < r.InputPer1K || s.OutputPer1K < r.OutputPer1K:
			ignored++
		}
	}
	return raised, added, ignored
}

// Wants reports whether a record is a Bedrock runtime or logging call. It
// skips data events: a trail that logs Bedrock model data events records each
// runtime call twice, as a management and a data event with the same tokens,
// and the guard requires management events, so the management copy is always
// there (fix-data-event-double-count D1).
func (p *Pack) Wants(r activity.Record) bool {
	if r.EventCategory == activity.CategoryMetric {
		return r.EventSource == activity.MantleSource // mantle usage read from metrics (add-mantle-usage D1)
	}
	if r.EventCategory == activity.CategoryData {
		return false
	}
	return r.EventSource == "bedrock.amazonaws.com" && (runtimeCalls[r.EventName] || loggingCalls[r.EventName])
}

// Evaluate prices a call, or fires a tripwire.
func (p *Pack) Evaluate(r activity.Record) (*activity.Usage, *activity.Hit) {
	hit := func(rule, detail string) *activity.Hit {
		return &activity.Hit{Account: r.Account, Time: r.EventTime, Rule: rule, Detail: detail}
	}
	if loggingCalls[r.EventName] {
		if r.ErrorCode != "" {
			return nil, nil // refused; nothing changed
		}
		return nil, hit("logging_changed", fmt.Sprintf("%s by %s in %s", r.EventName, r.CallerARN, r.Region))
	}
	if r.ErrorCode != "" {
		return nil, nil // refused calls cost nothing
	}
	key := ModelKey(r.ModelID)
	if r.EventCategory == activity.CategoryMetric {
		return p.evaluateMantle(r, key, hit)
	}
	if r.EventType == "AwsApiCall" {
		// Tripwires look at the call itself, once, not its completion record.
		if r.BearerToken && !callerMatches(r.CallerARN, p.cfg.APIKeyCallers) {
			return nil, hit("api_key_not_allowed", fmt.Sprintf("Bedrock API key used by %s in %s", r.CallerARN, r.Region))
		}
		if p.cfg.Regions != nil && !contains(p.cfg.Regions, r.Region) {
			return nil, hit("region_not_allowed", fmt.Sprintf("Bedrock call in %s by %s", r.Region, r.CallerARN))
		}
		if p.allowed != nil && !p.allowed[key] {
			return nil, hit("model_not_allowed", fmt.Sprintf("model %s by %s", r.ModelID, r.CallerARN))
		}
		if _, ok := p.PriceIn(key, r.Region); !ok && !p.cfg.CountsUnknown() {
			return nil, hit("unknown_model", fmt.Sprintf("model %s has no price; allowlist and price it in config to allow it", r.ModelID))
		}
		if streaming[r.EventName] || r.Stream {
			return nil, nil // priced from its completion record
		}
	} else if r.EventType != "AwsServiceEvent" || !streaming[r.EventName] {
		return nil, nil
	}
	use := TierNamed
	if r.TierUnknown {
		use = TierUnknown
	}
	price, ok, unpricedTier := p.PriceFor(key, r.Region, r.ServiceTier, use)
	unpriced := false
	if ok && r.InputTokens == 0 && r.OutputTokens == 0 {
		// A call whose records carry no token counts, such as a streamed
		// Chat Completions call, can't be measured (add-tier-pricing D9).
		if !p.cfg.AllowsUnmeasured() {
			return nil, hit("unmeasured_call", fmt.Sprintf("%s by %s in %s left no token counts, so its spend can't be measured; set ai.unmeasured_calls to allow to count such calls at $0", r.ModelID, r.CallerARN, r.Region))
		}
		return &activity.Usage{Account: r.Account, Time: r.EventTime, Model: key, Caller: r.CallerARN, Region: r.Region,
			Calls: 1, Unmeasured: true}, nil
	}
	if !ok {
		if !p.cfg.CountsUnknown() {
			return nil, nil // the call record already tripped
		}
		// Counted at the highest known price (D3). A call with no token
		// counts can't be estimated at all, so it still trips: a model priced
		// per image, second, or search must not run uncapped at $0.
		if r.InputTokens == 0 && r.OutputTokens == 0 {
			return nil, hit("unknown_model", fmt.Sprintf("model %s has no price, and its call has no token counts to estimate from", r.ModelID))
		}
		price, unpriced = p.unknown, true
	}
	usd := float64(r.InputTokens)/1000*price.InputPer1K + float64(r.OutputTokens)/1000*price.OutputPer1K
	u := &activity.Usage{Account: r.Account, Time: r.EventTime, Model: key, Caller: r.CallerARN, Region: r.Region,
		Calls: 1, In: r.InputTokens, Out: r.OutputTokens, USD: usd, Unpriced: unpriced}
	if unpricedTier {
		u.UnpricedTier = r.ServiceTier
	}
	return u, nil
}

// evaluateMantle prices a minute's mantle usage of one account and model, read
// from metrics, or fires a tripwire (add-mantle-usage D1, D4). The usage names
// no caller, so the API-key tripwire doesn't apply.
func (p *Pack) evaluateMantle(r activity.Record, key string, hit func(rule, detail string) *activity.Hit) (*activity.Usage, *activity.Hit) {
	if p.cfg.Regions != nil && !contains(p.cfg.Regions, r.Region) {
		return nil, hit("region_not_allowed", fmt.Sprintf("Bedrock use through mantle in %s", r.Region))
	}
	if p.allowed != nil && !p.allowed[key] && !p.allowedBase[key] {
		return nil, hit("model_not_allowed", fmt.Sprintf("model %s through mantle in %s", r.ModelID, r.Region))
	}
	// Mantle usage names no tier, so it is priced at the model's highest
	// tier (add-tier-pricing D3, D4).
	price, ok, _ := p.PriceFor(key, r.Region, "", TierUnknown)
	unpriced := false
	if !ok {
		if !p.cfg.CountsUnknown() {
			return nil, hit("unknown_model", fmt.Sprintf("model %s through mantle in %s has no price; allowlist and price it in config to allow it", r.ModelID, r.Region))
		}
		if r.InputTokens == 0 && r.OutputTokens == 0 {
			return nil, nil // the tokens come with a later read of the same minute
		}
		price, unpriced = p.unknown, true
	}
	usd := float64(r.InputTokens)/1000*price.InputPer1K + float64(r.OutputTokens)/1000*price.OutputPer1K
	return &activity.Usage{Account: r.Account, Time: r.EventTime, Model: key, Region: r.Region,
		Calls: r.Calls, In: r.InputTokens, Out: r.OutputTokens, USD: usd, Unpriced: unpriced}, nil
}

// Priced reports whether a model has a price, from config, the release, or
// shared prices. The engine uses it to forget unpriced models once they are
// priced (add-unknown-model-policy D5).
func (p *Pack) Priced(model string) bool {
	_, ok := p.Price(ModelKey(model))
	return ok
}

// UnknownPrice is what a call to an unpriced model is counted at under
// ai.unknown_models count.
func (p *Pack) UnknownPrice() config.ModelPrice { return p.unknown }

// UnknownRates is UnknownPrice for the engine, which doesn't import config.
func (p *Pack) UnknownRates() (inPer1K, outPer1K float64) {
	return p.unknown.InputPer1K, p.unknown.OutputPer1K
}

var _ activity.UnpricedPack = (*Pack)(nil)

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var _ activity.Pack = (*Pack)(nil)
