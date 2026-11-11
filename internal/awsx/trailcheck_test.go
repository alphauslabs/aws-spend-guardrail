package awsx

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cttypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
)

type fakeCT struct {
	trails  []cttypes.Trail
	logging bool
	sel     cloudtrail.GetEventSelectorsOutput
}

func (f *fakeCT) DescribeTrails(context.Context, *cloudtrail.DescribeTrailsInput, ...func(*cloudtrail.Options)) (*cloudtrail.DescribeTrailsOutput, error) {
	return &cloudtrail.DescribeTrailsOutput{TrailList: f.trails}, nil
}
func (f *fakeCT) GetTrailStatus(context.Context, *cloudtrail.GetTrailStatusInput, ...func(*cloudtrail.Options)) (*cloudtrail.GetTrailStatusOutput, error) {
	return &cloudtrail.GetTrailStatusOutput{IsLogging: aws.Bool(f.logging)}, nil
}
func (f *fakeCT) GetEventSelectors(context.Context, *cloudtrail.GetEventSelectorsInput, ...func(*cloudtrail.Options)) (*cloudtrail.GetEventSelectorsOutput, error) {
	return &f.sel, nil
}

func orgTrail(kms string, multi bool) cttypes.Trail {
	t := cttypes.Trail{TrailARN: aws.String("arn:aws:cloudtrail:us-east-1:123456789012:trail/org"), S3BucketName: aws.String("trail-bucket"),
		IsOrganizationTrail: aws.Bool(true), IsMultiRegionTrail: aws.Bool(multi)}
	if kms != "" {
		t.KmsKeyId = aws.String(kms)
	}
	return t
}

func allMgmt() cloudtrail.GetEventSelectorsOutput {
	return cloudtrail.GetEventSelectorsOutput{EventSelectors: []cttypes.EventSelector{{IncludeManagementEvents: aws.Bool(true), ReadWriteType: cttypes.ReadWriteTypeAll}}}
}

func TestCheckTrail(t *testing.T) {
	const key = "arn:aws:kms:us-east-1:999988887777:key/abc"
	cases := []struct {
		name string
		ct   *fakeCT
		kms  string
		want string // substring of a problem; "" for none
	}{
		{"usable", &fakeCT{trails: []cttypes.Trail{orgTrail("", true)}, logging: true, sel: allMgmt()}, "", ""},
		{"no trail", &fakeCT{}, "", "no organization trail"},
		{"one region", &fakeCT{trails: []cttypes.Trail{orgTrail("", false)}, logging: true, sel: allMgmt()}, "", "multi-region"},
		{"stopped", &fakeCT{trails: []cttypes.Trail{orgTrail("", true)}, sel: allMgmt()}, "", "not logging"},
		{"write only", &fakeCT{trails: []cttypes.Trail{orgTrail("", true)}, logging: true, sel: cloudtrail.GetEventSelectorsOutput{EventSelectors: []cttypes.EventSelector{{IncludeManagementEvents: aws.Bool(true), ReadWriteType: cttypes.ReadWriteTypeWriteOnly}}}}, "", "read and write"},
		{"encrypted, no key given", &fakeCT{trails: []cttypes.Trail{orgTrail(key, true)}, logging: true, sel: allMgmt()}, "", "TrailKmsKeyArn"},
		{"encrypted, key given", &fakeCT{trails: []cttypes.Trail{orgTrail(key, true)}, logging: true, sel: allMgmt()}, key, ""},
		{"advanced selectors", &fakeCT{trails: []cttypes.Trail{orgTrail("", true)}, logging: true, sel: cloudtrail.GetEventSelectorsOutput{AdvancedEventSelectors: []cttypes.AdvancedEventSelector{{FieldSelectors: []cttypes.AdvancedFieldSelector{{Field: aws.String("eventCategory"), Equals: []string{"Management"}}}}}}}, "", ""},
	}
	for _, c := range cases {
		_, probs, err := CheckTrail(context.Background(), c.ct, TrailCheck{Bucket: "trail-bucket", KMSKeyARN: c.kms})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := strings.Join(probs, " | ")
		if (c.want == "" && got != "") || (c.want != "" && !strings.Contains(got, c.want)) {
			t.Errorf("%s: problems %q, want %q", c.name, got, c.want)
		}
	}
}
