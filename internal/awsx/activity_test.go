package awsx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeDDB is a tiny DynamoDB: items by pk+sk, ADD on numbers, BETWEEN and
// equality queries.
type fakeDDB struct {
	items map[string]map[string]ddbtypes.AttributeValue
}

func newFakeDDB() *fakeDDB { return &fakeDDB{items: map[string]map[string]ddbtypes.AttributeValue{}} }

func sval(v ddbtypes.AttributeValue) string            { return v.(*ddbtypes.AttributeValueMemberS).Value }
func idOf(k map[string]ddbtypes.AttributeValue) string { return sval(k["pk"]) + "|" + sval(k["sk"]) }

func (f *fakeDDB) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return &dynamodb.GetItemOutput{Item: f.items[idOf(in.Key)]}, nil
}
func (f *fakeDDB) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.items[idOf(in.Item)] = in.Item
	return &dynamodb.PutItemOutput{}, nil
}
func (f *fakeDDB) DeleteItem(_ context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	delete(f.items, idOf(in.Key))
	return &dynamodb.DeleteItemOutput{}, nil
}
func (f *fakeDDB) UpdateItem(_ context.Context, in *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	id := idOf(in.Key)
	it := f.items[id]
	if it == nil {
		it = map[string]ddbtypes.AttributeValue{"pk": in.Key["pk"], "sk": in.Key["sk"]}
		f.items[id] = it
	}
	expr := strings.TrimPrefix(aws.ToString(in.UpdateExpression), "ADD ")
	adds, set, _ := strings.Cut(expr, " SET ")
	for _, part := range strings.Split(adds, ", ") {
		n, v, _ := strings.Cut(part, " ")
		name := in.ExpressionAttributeNames[n]
		add, _ := strconv.ParseFloat(in.ExpressionAttributeValues[v].(*ddbtypes.AttributeValueMemberN).Value, 64)
		it[name] = &ddbtypes.AttributeValueMemberN{Value: strconv.FormatFloat(num(it, name)+add, 'f', -1, 64)}
	}
	if n, v, ok := strings.Cut(set, " = "); ok {
		it[in.ExpressionAttributeNames[n]] = in.ExpressionAttributeValues[v]
	}
	return &dynamodb.UpdateItemOutput{}, nil
}
func (f *fakeDDB) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	pk := sval(in.ExpressionAttributeValues[":pk"])
	a, hasA := in.ExpressionAttributeValues[":a"]
	var out []map[string]ddbtypes.AttributeValue
	for _, it := range f.items {
		if sval(it["pk"]) != pk {
			continue
		}
		sk := sval(it["sk"])
		if hasA && (sk < sval(a) || sk > sval(in.ExpressionAttributeValues[":b"])) {
			continue
		}
		out = append(out, it)
	}
	return &dynamodb.QueryOutput{Items: out}, nil
}

func TestActivityStoreCounters(t *testing.T) {
	ctx := context.Background()
	db := newFakeDDB()
	s := &ActivityStore{API: db, Table: "t"}
	at := time.Date(2026, 11, 11, 9, 59, 30, 0, time.UTC)
	for _, u := range []activity.Usage{
		{Account: "111122223333", Time: at, Model: "m1", Calls: 1, In: 10, Out: 5, USD: 1.5},
		{Account: "111122223333", Time: at, Model: "m1", Calls: 1, In: 10, Out: 5, USD: 1.5},
		{Account: "111122223333", Time: at.Add(-2 * time.Hour), Model: "m2", Calls: 1, USD: 4},
	} {
		if err := s.AddUsage(ctx, "ai", u); err != nil {
			t.Fatal(err)
		}
	}
	tot, err := s.Totals(ctx, "ai", "111122223333", at.Add(30*time.Second))
	if err != nil || tot.Hour != 3 || tot.Day != 7 || tot.Month != 7 {
		t.Fatalf("totals %+v %v", tot, err)
	}
	it := db.items["usage#ai#111122223333|m#202611110959"]
	exp, _ := strconv.ParseInt(it["expires"].(*ddbtypes.AttributeValueMemberN).Value, 10, 64)
	if want := at.Add(UsageRetention).Unix(); exp != want {
		t.Fatalf("expires %d, want %d", exp, want)
	}
	sums, _ := s.HourSummaries(ctx, "ai", at.Truncate(time.Hour))
	if len(sums) != 1 || sums[0].Model != "m1" || sums[0].Calls != 2 || sums[0].In != 20 || sums[0].USD != 3 {
		t.Fatalf("summaries %+v", sums)
	}
}

func TestActivityStoreCutoffsAndMeta(t *testing.T) {
	ctx := context.Background()
	s := &ActivityStore{API: newFakeDDB(), Table: "t"}
	c := activity.Cutoff{ID: "ai:111122223333:20261111T1000", Pack: "ai", Account: "111122223333", Rule: "hourly_cap", Attached: true}
	if err := s.PutCutoff(ctx, c); err != nil {
		t.Fatal(err)
	}
	cuts, _ := s.Cutoffs(ctx, "ai")
	if len(cuts) != 1 || cuts[0].ID != c.ID {
		t.Fatalf("cutoffs %+v", cuts)
	}
	_ = s.DeleteCutoff(ctx, "ai", "111122223333")
	if cuts, _ := s.Cutoffs(ctx, "ai"); len(cuts) != 0 {
		t.Fatal("not deleted")
	}
	_ = s.PutMeta(ctx, "x", map[string]int{"a": 1})
	var m map[string]int
	if ok, _ := s.GetMeta(ctx, "x", &m); !ok || m["a"] != 1 {
		t.Fatalf("meta %v", m)
	}
}

type fakeS3 struct {
	pages [][]string
	dirs  []string
	asked []*s3.ListObjectsV2Input
}

func (f *fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	f.asked = append(f.asked, in)
	if in.Delimiter != nil {
		var cps []s3types.CommonPrefix
		for _, d := range f.dirs {
			cps = append(cps, s3types.CommonPrefix{Prefix: aws.String(d)})
		}
		return &s3.ListObjectsV2Output{CommonPrefixes: cps}, nil
	}
	i := 0
	if in.ContinuationToken != nil {
		i, _ = strconv.Atoi(*in.ContinuationToken)
	}
	out := &s3.ListObjectsV2Output{}
	for _, k := range f.pages[i] {
		out.Contents = append(out.Contents, s3types.Object{Key: aws.String(k), LastModified: aws.Time(time.Unix(0, 0))})
	}
	if i+1 < len(f.pages) {
		out.IsTruncated = aws.Bool(true)
		out.NextContinuationToken = aws.String(strconv.Itoa(i + 1))
	}
	return out, nil
}
func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader([]byte(aws.ToString(in.Key))))}, nil
}

func TestTrailBucketPages(t *testing.T) {
	f := &fakeS3{pages: [][]string{{"a", "b"}, {"c"}}, dirs: []string{"AWSLogs/o-x/111122223333/"}}
	b := &TrailBucket{API: f, Name: "trail"}
	objs, err := b.List(context.Background(), "p/", "after")
	if err != nil || len(objs) != 3 || aws.ToString(f.asked[0].StartAfter) != "after" {
		t.Fatalf("objs %v err %v", objs, err)
	}
	dirs, _ := b.Dirs(context.Background(), "AWSLogs/o-x/")
	if len(dirs) != 1 {
		t.Fatalf("dirs %v", dirs)
	}
	body, _ := b.Get(context.Background(), "k")
	if string(body) != "k" {
		t.Fatalf("body %q", body)
	}
}

type fakeAttach struct{ attachErr, detachErr error }

func (f *fakeAttach) AttachPolicy(context.Context, *organizations.AttachPolicyInput, ...func(*organizations.Options)) (*organizations.AttachPolicyOutput, error) {
	return &organizations.AttachPolicyOutput{}, f.attachErr
}
func (f *fakeAttach) DetachPolicy(context.Context, *organizations.DetachPolicyInput, ...func(*organizations.Options)) (*organizations.DetachPolicyOutput, error) {
	return &organizations.DetachPolicyOutput{}, f.detachErr
}

func TestPackPoliciesAreIdempotent(t *testing.T) {
	p := &PackPolicies{API: &fakeAttach{attachErr: errors.New("DuplicatePolicyAttachmentException: already"), detachErr: errors.New("PolicyNotAttachedException: no")}}
	if err := p.Attach(context.Background(), "p", "a"); err != nil {
		t.Fatal(err)
	}
	if err := p.Detach(context.Background(), "p", "a"); err != nil {
		t.Fatal(err)
	}
	p = &PackPolicies{API: &fakeAttach{attachErr: errors.New("AccessDeniedException")}}
	if err := p.Attach(context.Background(), "p", "a"); err == nil {
		t.Fatal("a real error must surface")
	}
}
