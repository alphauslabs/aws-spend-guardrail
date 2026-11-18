// Package activity is the guard's fast path: it reads the organization
// CloudTrail trail as it lands, hands records to packs, keeps per-account
// totals, and cuts off an account with a pack's own deny policy.
package activity

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Record is the part of a CloudTrail record the packs use.
type Record struct {
	EventTime   time.Time
	EventSource string
	EventName   string
	EventType   string // AwsApiCall, AwsServiceEvent, ...
	// EventCategory is Management or Data. A trail that logs a service's
	// data events can record one call as both (fix-data-event-double-count).
	EventCategory string
	Region        string
	Account       string // the account the call was made in
	CallerARN     string
	CallerType    string
	ErrorCode     string
	// ModelID is requestParameters.modelId, or for a record without request
	// parameters (a streaming completion) the first resource ARN.
	ModelID      string
	InputTokens  int64
	OutputTokens int64
	BearerToken  bool // additionalEventData.callWithBearerToken
	// Calls is how many calls the record stands for: 1 for a trail record,
	// and a minute's growth in inferences for mantle usage read from metrics
	// (add-mantle-usage D1).
	Calls int64
}

// The CloudTrail event categories the guard tells apart.
const (
	CategoryManagement = "Management"
	CategoryData       = "Data"
	// CategoryMetric marks usage read from metrics, not the trail
	// (add-mantle-usage D1).
	CategoryMetric = "Metric"
)

type rawRecord struct {
	EventTime    string `json:"eventTime"`
	EventSource  string `json:"eventSource"`
	EventName    string `json:"eventName"`
	EventType    string `json:"eventType"`
	EventCat     string `json:"eventCategory"`
	AWSRegion    string `json:"awsRegion"`
	RecipientAcc string `json:"recipientAccountId"`
	ErrorCode    string `json:"errorCode"`
	UserIdentity struct {
		Type string `json:"type"`
		ARN  string `json:"arn"`
	} `json:"userIdentity"`
	RequestParameters struct {
		ModelID string `json:"modelId"`
	} `json:"requestParameters"`
	AdditionalEventData struct {
		InputTokens         int64 `json:"inputTokens"`
		OutputTokens        int64 `json:"outputTokens"`
		CallWithBearerToken bool  `json:"callWithBearerToken"`
	} `json:"additionalEventData"`
	ServiceEventDetails struct {
		AdditionalEventData struct {
			AdditionalEntries struct {
				InputTokens  int64 `json:"inputTokens"`
				OutputTokens int64 `json:"outputTokens"`
			} `json:"additionalEntries"`
		} `json:"AdditionalEventData"`
	} `json:"serviceEventDetails"`
	Resources []struct {
		ARN string `json:"ARN"`
	} `json:"resources"`
}

// ParseFile reads one gzipped CloudTrail log file.
func ParseFile(gz []byte) ([]Record, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	var f struct {
		Records []json.RawMessage `json:"Records"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse trail file: %w", err)
	}
	out := make([]Record, 0, len(f.Records))
	for _, m := range f.Records {
		var r rawRecord
		if err := json.Unmarshal(m, &r); err != nil {
			continue // one odd record doesn't spoil the file
		}
		t, err := time.Parse(time.RFC3339, r.EventTime)
		if err != nil {
			continue
		}
		rec := Record{
			EventTime:     t.UTC(),
			EventSource:   r.EventSource,
			EventName:     r.EventName,
			EventType:     r.EventType,
			EventCategory: r.EventCat,
			Region:        r.AWSRegion,
			Account:       r.RecipientAcc,
			CallerARN:     r.UserIdentity.ARN,
			CallerType:    r.UserIdentity.Type,
			ErrorCode:     r.ErrorCode,
			ModelID:       r.RequestParameters.ModelID,
			InputTokens:   r.AdditionalEventData.InputTokens,
			OutputTokens:  r.AdditionalEventData.OutputTokens,
			BearerToken:   r.AdditionalEventData.CallWithBearerToken,
			Calls:         1,
		}
		if rec.EventCategory == "" {
			rec.EventCategory = CategoryManagement // management records always carried one; old fixtures don't
		}
		if e := r.ServiceEventDetails.AdditionalEventData.AdditionalEntries; e.InputTokens != 0 || e.OutputTokens != 0 {
			rec.InputTokens, rec.OutputTokens = e.InputTokens, e.OutputTokens
		}
		if rec.ModelID == "" && len(r.Resources) > 0 {
			rec.ModelID = r.Resources[0].ARN
		}
		out = append(out, rec)
	}
	return out, nil
}
