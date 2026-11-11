// Package ai is the AI pack: it prices Amazon Bedrock runtime calls from
// their token counts and trips on calls that shouldn't happen at all.
package ai

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
)

//go:embed prices.json
var pricesJSON []byte

// Table is the shipped price table.
type Table struct {
	Source    string                       `json:"source"`
	Published string                       `json:"published"`
	Basis     string                       `json:"basis"`
	Models    map[string]config.ModelPrice `json:"models"`
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
	runtimeCalls = map[string]bool{"InvokeModel": true, "InvokeModelWithResponseStream": true, "Converse": true, "ConverseStream": true}
	streaming    = map[string]bool{"InvokeModelWithResponseStream": true, "ConverseStream": true}
	loggingCalls = map[string]bool{"PutModelInvocationLoggingConfiguration": true, "DeleteModelInvocationLoggingConfiguration": true}

	geoPrefix  = regexp.MustCompile(`^(us|eu|apac|ap|jp|au|ca|global|us-gov)\.`)
	ctxSuffix  = regexp.MustCompile(`:[0-9]+k$`)
	assumedArn = regexp.MustCompile(`^arn:(aws[a-z-]*):sts::([0-9]{12}):assumed-role/([^/]+)/`)
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
	table   map[string]config.ModelPrice
	cfg     *config.ResolvedAI
	allowed map[string]bool // normalized model allowlist; nil means any priced model
}

// New returns the AI pack for a config in force.
func New(t *Table, cfg *config.ResolvedAI) *Pack {
	if cfg == nil {
		cfg = &config.ResolvedAI{}
	}
	p := &Pack{table: t.Models, cfg: cfg}
	if cfg.Models != nil {
		p.allowed = map[string]bool{}
		for _, m := range cfg.Models {
			p.allowed[ModelKey(m)] = true
		}
	}
	return p
}

// Price returns a model's price: a config override first, then the table.
func (p *Pack) Price(key string) (config.ModelPrice, bool) {
	for k, v := range p.cfg.Prices {
		if ModelKey(k) == key {
			return v, true
		}
	}
	v, ok := p.table[key]
	return v, ok
}

// Wants reports whether a record is a Bedrock runtime or logging call.
func (p *Pack) Wants(r activity.Record) bool {
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
		if _, ok := p.Price(key); !ok {
			return nil, hit("unknown_model", fmt.Sprintf("model %s has no price; allowlist and price it in config to allow it", r.ModelID))
		}
		if streaming[r.EventName] {
			return nil, nil // priced from its completion record
		}
	} else if r.EventType != "AwsServiceEvent" || !streaming[r.EventName] {
		return nil, nil
	}
	price, ok := p.Price(key)
	if !ok {
		return nil, nil // the call record already tripped
	}
	usd := float64(r.InputTokens)/1000*price.InputPer1K + float64(r.OutputTokens)/1000*price.OutputPer1K
	return &activity.Usage{Account: r.Account, Time: r.EventTime, Model: key, Caller: r.CallerARN, Region: r.Region,
		Calls: 1, In: r.InputTokens, Out: r.OutputTokens, USD: usd}, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var _ activity.Pack = (*Pack)(nil)
