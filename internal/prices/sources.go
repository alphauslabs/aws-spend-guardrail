package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
)

// emptyPayloadHash is the SHA-256 of an empty body, for signing a GET.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// NewSources returns the sources of a regional build from an AWS config
// (add-regional-prices D5). The Price List client retries adaptively, as the
// API throttles.
func NewSources(cfg aws.Config) Sources {
	pc := cfg.Copy()
	pc.Region = "us-east-1" // the Price List API's endpoint
	pc.Retryer = func() aws.Retryer {
		return retry.NewAdaptiveMode(func(o *retry.AdaptiveModeOptions) {
			o.StandardOptions = append(o.StandardOptions, func(so *retry.StandardOptions) { so.MaxAttempts = 10 })
		})
	}
	httpc := &http.Client{Timeout: 15 * time.Second}
	return Sources{
		Pricing: pricing.NewFromConfig(pc),
		Models: func(region string) ModelsAPI {
			c := cfg.Copy()
			c.Region = region
			return bedrock.NewFromConfig(c)
		},
		Mantle: func(ctx context.Context, region string) ([]string, bool, error) {
			return mantleModels(ctx, cfg, httpc, region)
		},
	}
}

// mantleModels lists the bedrock-mantle endpoint's models in a region with a
// SigV4-signed GET /v1/models. A region whose endpoint host doesn't resolve
// has no mantle endpoint.
func mantleModels(ctx context.Context, cfg aws.Config, httpc *http.Client, region string) ([]string, bool, error) {
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://bedrock-mantle."+region+".api.aws/v1/models", nil)
	if err != nil {
		return nil, false, err
	}
	if err := v4.NewSigner().SignHTTP(ctx, creds, req, emptyPayloadHash, "bedrock-mantle", region, time.Now()); err != nil {
		return nil, false, err
	}
	resp, err := httpc.Do(req)
	if err != nil {
		var dns *net.DNSError
		if errors.As(err, &dns) && dns.IsNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, body)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, false, fmt.Errorf("mantle model list: %w", err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, d := range out.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	return ids, true, nil
}
