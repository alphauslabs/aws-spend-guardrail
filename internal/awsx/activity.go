package awsx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ActivityDynamoAPI is the part of DynamoDB the activity store uses.
type ActivityDynamoAPI interface {
	DynamoAPI
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

// UsageRetention is how long usage counters are kept, a month plus margin.
const UsageRetention = 40 * 24 * time.Hour

// ActivityStore implements activity.Store on the guard's state table.
//
// Items, all under the table's "pk"/"sk" keys:
//
//	usage#<pack>#<account>        m#<YYYYMMDDHHMM>   usd            (minute)
//	usage#<pack>#<account>        d#<YYYYMMDD>       usd            (day)
//	usage#<pack>#<account>        M#<YYYYMM>         usd            (month)
//	usageh#<pack>#<YYYYMMDDHH>    <account>#<model>  calls,in,out,usd
//	cutoff#<pack>                 <account>          doc
//	activity                      <name>             doc
//
// Counters are atomic ADDs and expire after UsageRetention through the
// table's "expires" attribute.
type ActivityStore struct {
	API   ActivityDynamoAPI
	Table string
}

func (s *ActivityStore) add(ctx context.Context, pk, sk string, vals map[string]float64, expires time.Time) error {
	expr := []string{}
	names := map[string]string{"#e": "expires"}
	values := map[string]ddbtypes.AttributeValue{":e": &ddbtypes.AttributeValueMemberN{Value: strconv.FormatInt(expires.Unix(), 10)}}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		n, v := fmt.Sprintf("#a%d", i), fmt.Sprintf(":a%d", i)
		names[n] = k
		values[v] = &ddbtypes.AttributeValueMemberN{Value: strconv.FormatFloat(vals[k], 'f', -1, 64)}
		expr = append(expr, n+" "+v)
	}
	_, err := s.API.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.Table),
		Key:                       key(pk, sk),
		UpdateExpression:          aws.String("ADD " + strings.Join(expr, ", ") + " SET #e = :e"),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
	})
	return err
}

// AddUsage adds a priced call to its counters.
func (s *ActivityStore) AddUsage(ctx context.Context, pack string, u activity.Usage) error {
	t := u.Time.UTC()
	exp := t.Add(UsageRetention)
	pk := "usage#" + pack + "#" + u.Account
	for _, sk := range []string{"m#" + t.Format("200601021504"), "d#" + t.Format("20060102"), "M#" + t.Format("200601")} {
		if err := s.add(ctx, pk, sk, map[string]float64{"usd": u.USD}, exp); err != nil {
			return fmt.Errorf("add usage: %w", err)
		}
	}
	return s.add(ctx, "usageh#"+pack+"#"+t.Format("2006010215"), u.Account+"#"+u.Model,
		map[string]float64{"calls": float64(u.Calls), "in": float64(u.In), "out": float64(u.Out), "usd": u.USD}, exp)
}

func num(item map[string]ddbtypes.AttributeValue, name string) float64 {
	if v, ok := item[name].(*ddbtypes.AttributeValueMemberN); ok {
		f, _ := strconv.ParseFloat(v.Value, 64)
		return f
	}
	return 0
}

func (s *ActivityStore) query(ctx context.Context, in *dynamodb.QueryInput) ([]map[string]ddbtypes.AttributeValue, error) {
	in.TableName = aws.String(s.Table)
	in.ConsistentRead = aws.Bool(true)
	var out []map[string]ddbtypes.AttributeValue
	for {
		page, err := s.API.Query(ctx, in)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if len(page.LastEvaluatedKey) == 0 {
			return out, nil
		}
		in.ExclusiveStartKey = page.LastEvaluatedKey
	}
}

// Totals sums the last 60 minute counters, and reads today's and this
// month's.
func (s *ActivityStore) Totals(ctx context.Context, pack, account string, now time.Time) (activity.Totals, error) {
	now = now.UTC()
	pk := "usage#" + pack + "#" + account
	items, err := s.query(ctx, &dynamodb.QueryInput{
		KeyConditionExpression: aws.String("pk = :pk AND sk BETWEEN :a AND :b"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":pk": &ddbtypes.AttributeValueMemberS{Value: pk},
			":a":  &ddbtypes.AttributeValueMemberS{Value: "m#" + now.Add(-59*time.Minute).Format("200601021504")},
			":b":  &ddbtypes.AttributeValueMemberS{Value: "m#" + now.Format("200601021504")},
		},
	})
	if err != nil {
		return activity.Totals{}, fmt.Errorf("read usage: %w", err)
	}
	var t activity.Totals
	for _, it := range items {
		t.Hour += num(it, "usd")
	}
	for _, c := range []struct {
		sk  string
		dst *float64
	}{{"d#" + now.Format("20060102"), &t.Day}, {"M#" + now.Format("200601"), &t.Month}} {
		out, err := s.API.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.Table), Key: key(pk, c.sk), ConsistentRead: aws.Bool(true)})
		if err != nil {
			return activity.Totals{}, fmt.Errorf("read usage: %w", err)
		}
		*c.dst = num(out.Item, "usd")
	}
	return t, nil
}

// HourSummaries lists one hour's usage per account and model.
func (s *ActivityStore) HourSummaries(ctx context.Context, pack string, hour time.Time) ([]activity.Summary, error) {
	items, err := s.query(ctx, &dynamodb.QueryInput{
		KeyConditionExpression:    aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":pk": &ddbtypes.AttributeValueMemberS{Value: "usageh#" + pack + "#" + hour.UTC().Format("2006010215")}},
	})
	if err != nil {
		return nil, fmt.Errorf("read hour usage: %w", err)
	}
	out := make([]activity.Summary, 0, len(items))
	for _, it := range items {
		sk, _ := it["sk"].(*ddbtypes.AttributeValueMemberS)
		if sk == nil {
			continue
		}
		acct, model, _ := strings.Cut(sk.Value, "#")
		out = append(out, activity.Summary{Account: acct, Model: model, Calls: int64(num(it, "calls")), In: int64(num(it, "in")), Out: int64(num(it, "out")), USD: num(it, "usd")})
	}
	return out, nil
}

func (s *ActivityStore) doc(ctx context.Context, pk, sk string, v any) (bool, error) {
	return (&State{API: s.API, Table: s.Table}).get(ctx, pk, sk, v)
}

func (s *ActivityStore) putDoc(ctx context.Context, pk, sk string, v any) error {
	return (&State{API: s.API, Table: s.Table}).put(ctx, pk, sk, v)
}

// Cutoffs lists a pack's cutoffs.
func (s *ActivityStore) Cutoffs(ctx context.Context, pack string) ([]activity.Cutoff, error) {
	items, err := s.query(ctx, &dynamodb.QueryInput{
		KeyConditionExpression:    aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":pk": &ddbtypes.AttributeValueMemberS{Value: "cutoff#" + pack}},
	})
	if err != nil {
		return nil, fmt.Errorf("read cutoffs: %w", err)
	}
	var out []activity.Cutoff
	for _, it := range items {
		d, ok := it["doc"].(*ddbtypes.AttributeValueMemberS)
		if !ok {
			continue
		}
		var c activity.Cutoff
		if err := json.Unmarshal([]byte(d.Value), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// PutCutoff records a cutoff.
func (s *ActivityStore) PutCutoff(ctx context.Context, c activity.Cutoff) error {
	return s.putDoc(ctx, "cutoff#"+c.Pack, c.Account, c)
}

// DeleteCutoff forgets a cutoff.
func (s *ActivityStore) DeleteCutoff(ctx context.Context, pack, account string) error {
	_, err := s.API.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.Table), Key: key("cutoff#"+pack, account)})
	return err
}

// GetMeta reads engine state.
func (s *ActivityStore) GetMeta(ctx context.Context, name string, v any) (bool, error) {
	return s.doc(ctx, "activity", name, v)
}

// PutMeta writes engine state.
func (s *ActivityStore) PutMeta(ctx context.Context, name string, v any) error {
	return s.putDoc(ctx, "activity", name, v)
}

// TrailS3API is the part of S3 the trail reader uses.
type TrailS3API interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// TrailBucket implements activity.Bucket on the organization trail's bucket.
type TrailBucket struct {
	API  TrailS3API
	Name string
}

// List returns the keys under prefix after startAfter, in order.
func (b *TrailBucket) List(ctx context.Context, prefix, startAfter string) ([]activity.Object, error) {
	in := &s3.ListObjectsV2Input{Bucket: aws.String(b.Name), Prefix: aws.String(prefix)}
	if startAfter != "" {
		in.StartAfter = aws.String(startAfter)
	}
	var out []activity.Object
	for {
		page, err := b.API.ListObjectsV2(ctx, in)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			out = append(out, activity.Object{Key: aws.ToString(o.Key), LastModified: aws.ToTime(o.LastModified)})
		}
		if !aws.ToBool(page.IsTruncated) {
			return out, nil
		}
		in.ContinuationToken = page.NextContinuationToken
	}
}

// Dirs returns the common prefixes one level below prefix.
func (b *TrailBucket) Dirs(ctx context.Context, prefix string) ([]string, error) {
	in := &s3.ListObjectsV2Input{Bucket: aws.String(b.Name), Prefix: aws.String(prefix), Delimiter: aws.String("/")}
	var out []string
	for {
		page, err := b.API.ListObjectsV2(ctx, in)
		if err != nil {
			return nil, err
		}
		for _, p := range page.CommonPrefixes {
			out = append(out, aws.ToString(p.Prefix))
		}
		if !aws.ToBool(page.IsTruncated) {
			return out, nil
		}
		in.ContinuationToken = page.NextContinuationToken
	}
}

// Get reads one object.
func (b *TrailBucket) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := b.API.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(b.Name), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// PolicyAttachAPI is the part of Organizations that attaches policies.
type PolicyAttachAPI interface {
	AttachPolicy(context.Context, *organizations.AttachPolicyInput, ...func(*organizations.Options)) (*organizations.AttachPolicyOutput, error)
	DetachPolicy(context.Context, *organizations.DetachPolicyInput, ...func(*organizations.Options)) (*organizations.DetachPolicyOutput, error)
}

// PackPolicies implements activity.Policies. Attaching a policy that is
// already attached, or detaching one that isn't, is not an error.
type PackPolicies struct {
	API PolicyAttachAPI
}

// Attach attaches a pack's deny policy to an account.
func (p *PackPolicies) Attach(ctx context.Context, policyID, account string) error {
	_, err := p.API.AttachPolicy(ctx, &organizations.AttachPolicyInput{PolicyId: aws.String(policyID), TargetId: aws.String(account)})
	if err != nil && strings.Contains(err.Error(), "DuplicatePolicyAttachment") {
		return nil
	}
	return err
}

// Detach detaches a pack's deny policy from an account.
func (p *PackPolicies) Detach(ctx context.Context, policyID, account string) error {
	_, err := p.API.DetachPolicy(ctx, &organizations.DetachPolicyInput{PolicyId: aws.String(policyID), TargetId: aws.String(account)})
	if err != nil && strings.Contains(err.Error(), "PolicyNotAttached") {
		return nil
	}
	return err
}

var (
	_ activity.Store    = (*ActivityStore)(nil)
	_ activity.Bucket   = (*TrailBucket)(nil)
	_ activity.Policies = (*PackPolicies)(nil)
)
