package awsx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cttypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
)

// CloudTrailAPI is the part of CloudTrail the AI install check uses.
type CloudTrailAPI interface {
	DescribeTrails(context.Context, *cloudtrail.DescribeTrailsInput, ...func(*cloudtrail.Options)) (*cloudtrail.DescribeTrailsOutput, error)
	GetTrailStatus(context.Context, *cloudtrail.GetTrailStatusInput, ...func(*cloudtrail.Options)) (*cloudtrail.GetTrailStatusOutput, error)
	GetEventSelectors(context.Context, *cloudtrail.GetEventSelectorsInput, ...func(*cloudtrail.Options)) (*cloudtrail.GetEventSelectorsOutput, error)
}

// TrailCheck is what the AI guard needs to know about the organization trail.
type TrailCheck struct {
	Bucket, Prefix, KMSKeyARN string
}

// CheckTrail finds the organization trail writing to the bucket and returns
// what's wrong with it for the AI guard; none means it is usable. It also
// returns the trail's name when found.
func CheckTrail(ctx context.Context, api CloudTrailAPI, c TrailCheck) (string, []string, error) {
	out, err := api.DescribeTrails(ctx, &cloudtrail.DescribeTrailsInput{IncludeShadowTrails: aws.Bool(true)})
	if err != nil {
		return "", nil, fmt.Errorf("describe trails: %w", err)
	}
	var trail *cttypes.Trail
	for i, t := range out.TrailList {
		if aws.ToBool(t.IsOrganizationTrail) && aws.ToString(t.S3BucketName) == c.Bucket && strings.Trim(aws.ToString(t.S3KeyPrefix), "/") == strings.Trim(c.Prefix, "/") {
			trail = &out.TrailList[i]
			break
		}
	}
	if trail == nil {
		return "", []string{fmt.Sprintf("no organization trail writes to bucket %s with prefix %q; create one, or set the bucket and prefix of the existing one", c.Bucket, c.Prefix)}, nil
	}
	name := aws.ToString(trail.TrailARN)
	var probs []string
	if !aws.ToBool(trail.IsMultiRegionTrail) {
		probs = append(probs, "the trail records one region only; the AI guard needs a multi-region trail to see Bedrock calls in every region")
	}
	st, err := api.GetTrailStatus(ctx, &cloudtrail.GetTrailStatusInput{Name: trail.TrailARN})
	if err != nil {
		return name, nil, fmt.Errorf("trail status: %w", err)
	}
	if !aws.ToBool(st.IsLogging) {
		probs = append(probs, "the trail is not logging")
	}
	sel, err := api.GetEventSelectors(ctx, &cloudtrail.GetEventSelectorsInput{TrailName: trail.TrailARN})
	if err != nil {
		return name, nil, fmt.Errorf("event selectors: %w", err)
	}
	if p := managementProblem(sel); p != "" {
		probs = append(probs, p)
	}
	key := aws.ToString(trail.KmsKeyId)
	switch {
	case key != "" && c.KMSKeyARN == "":
		probs = append(probs, fmt.Sprintf("the trail encrypts its files with %s; set TrailKmsKeyArn (and --trail-kms-key) to it", key))
	case key != "" && key != c.KMSKeyARN:
		probs = append(probs, fmt.Sprintf("the trail encrypts with %s, not %s", key, c.KMSKeyARN))
	}
	return name, probs, nil
}

// managementProblem says why the selectors don't record every management
// event, or "" when they do. Streaming Bedrock calls are read events and the
// others write events, so both are needed.
func managementProblem(sel *cloudtrail.GetEventSelectorsOutput) string {
	for _, s := range sel.EventSelectors {
		if aws.ToBool(s.IncludeManagementEvents) {
			if s.ReadWriteType != cttypes.ReadWriteTypeAll {
				return "the trail records only " + string(s.ReadWriteType) + " management events; the AI guard needs both read and write events"
			}
			return ""
		}
	}
	for _, a := range sel.AdvancedEventSelectors {
		mgmt, rw := false, false
		for _, f := range a.FieldSelectors {
			switch aws.ToString(f.Field) {
			case "eventCategory":
				for _, v := range f.Equals {
					mgmt = mgmt || v == "Management"
				}
			case "readOnly":
				rw = true
			}
		}
		if mgmt && !rw {
			return ""
		}
		if mgmt {
			return "the trail records only read or only write management events; the AI guard needs both"
		}
	}
	return "the trail records no management events; the AI guard reads Bedrock calls from them"
}

// RecentTrailDay is the key prefix of a day's files for one account and
// region, for reading one recent file.
func RecentTrailDay(base, account, region string, day time.Time) string {
	return fmt.Sprintf("%s%s/CloudTrail/%s/%s", base, account, region, day.UTC().Format("2006/01/02/"))
}
