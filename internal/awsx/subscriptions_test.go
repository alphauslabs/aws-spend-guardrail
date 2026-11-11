package awsx

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

type fakeSNSSubs struct {
	pages        [][]snstypes.Subscription
	subscribed   []*sns.SubscribeInput
	unsubscribed []string
}

func (f *fakeSNSSubs) ListSubscriptionsByTopic(_ context.Context, in *sns.ListSubscriptionsByTopicInput, _ ...func(*sns.Options)) (*sns.ListSubscriptionsByTopicOutput, error) {
	i := 0
	if in.NextToken != nil {
		i = int(aws.ToString(in.NextToken)[0] - '0')
	}
	out := &sns.ListSubscriptionsByTopicOutput{Subscriptions: f.pages[i]}
	if i+1 < len(f.pages) {
		out.NextToken = aws.String(string(rune('0' + i + 1)))
	}
	return out, nil
}

func (f *fakeSNSSubs) Subscribe(_ context.Context, in *sns.SubscribeInput, _ ...func(*sns.Options)) (*sns.SubscribeOutput, error) {
	f.subscribed = append(f.subscribed, in)
	return &sns.SubscribeOutput{SubscriptionArn: aws.String("pending confirmation")}, nil
}

func (f *fakeSNSSubs) Unsubscribe(_ context.Context, in *sns.UnsubscribeInput, _ ...func(*sns.Options)) (*sns.UnsubscribeOutput, error) {
	f.unsubscribed = append(f.unsubscribed, aws.ToString(in.SubscriptionArn))
	return &sns.UnsubscribeOutput{}, nil
}

func sub(protocol, endpoint, arn string) snstypes.Subscription {
	return snstypes.Subscription{Protocol: aws.String(protocol), Endpoint: aws.String(endpoint), SubscriptionArn: aws.String(arn)}
}

func TestAlertSubscriptionsList(t *testing.T) {
	f := &fakeSNSSubs{pages: [][]snstypes.Subscription{
		{sub("email", "oncall@example.com", "arn:aws:sns:us-east-1:123456789012:alerts:1"), sub("https", "https://hook.example.com", "arn:x")},
		{sub("email", "finops@example.com", "PendingConfirmation")},
	}}
	a := &AlertSubscriptions{API: f, TopicARN: "arn:aws:sns:us-east-1:123456789012:alerts"}
	got, err := a.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 email subscriptions across pages, got %+v", got)
	}
	if got[0].Pending || got[0].ARN != "arn:aws:sns:us-east-1:123456789012:alerts:1" {
		t.Errorf("confirmed: %+v", got[0])
	}
	if !got[1].Pending || got[1].ARN != "" || got[1].Address != "finops@example.com" {
		t.Errorf("pending: %+v", got[1])
	}
}

func TestAlertSubscriptionsSubscribeAndUnsubscribe(t *testing.T) {
	f := &fakeSNSSubs{}
	a := &AlertSubscriptions{API: f, TopicARN: "arn:aws:sns:us-east-1:123456789012:alerts"}
	if err := a.Subscribe(context.Background(), "finops@example.com"); err != nil {
		t.Fatal(err)
	}
	in := f.subscribed[0]
	if aws.ToString(in.Protocol) != "email" || aws.ToString(in.Endpoint) != "finops@example.com" || aws.ToString(in.TopicArn) != a.TopicARN {
		t.Fatalf("subscribe input: %+v", in)
	}
	if err := a.Unsubscribe(context.Background(), "arn:aws:sns:us-east-1:123456789012:alerts:1"); err != nil {
		t.Fatal(err)
	}
	if len(f.unsubscribed) != 1 || f.unsubscribed[0] != "arn:aws:sns:us-east-1:123456789012:alerts:1" {
		t.Fatalf("unsubscribed: %v", f.unsubscribed)
	}
}
