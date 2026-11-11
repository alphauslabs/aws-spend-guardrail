package awsx

import (
	"context"

	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// SNSSubscriptionsAPI is the part of the SNS client that manages the alert
// topic's subscriptions.
type SNSSubscriptionsAPI interface {
	ListSubscriptionsByTopic(context.Context, *sns.ListSubscriptionsByTopicInput, ...func(*sns.Options)) (*sns.ListSubscriptionsByTopicOutput, error)
	Subscribe(context.Context, *sns.SubscribeInput, ...func(*sns.Options)) (*sns.SubscribeOutput, error)
	Unsubscribe(context.Context, *sns.UnsubscribeInput, ...func(*sns.Options)) (*sns.UnsubscribeOutput, error)
}

// pendingARN is what SNS lists as the ARN of a subscription awaiting
// confirmation.
const pendingARN = "PendingConfirmation"

// AlertSubscriptions implements guard.AlertSubscriptions on the stack's local
// alert topic. It handles email subscriptions only.
type AlertSubscriptions struct {
	API      SNSSubscriptionsAPI
	TopicARN string
}

// List returns the topic's email subscriptions, pending ones included.
func (a *AlertSubscriptions) List(ctx context.Context) ([]guard.Subscription, error) {
	var out []guard.Subscription
	var next *string
	for {
		page, err := a.API.ListSubscriptionsByTopic(ctx, &sns.ListSubscriptionsByTopicInput{TopicArn: aws.String(a.TopicARN), NextToken: next})
		if err != nil {
			return nil, err
		}
		for _, s := range page.Subscriptions {
			if aws.ToString(s.Protocol) != "email" {
				continue
			}
			arn := aws.ToString(s.SubscriptionArn)
			sub := guard.Subscription{Address: aws.ToString(s.Endpoint), ARN: arn}
			if arn == pendingARN || arn == "" {
				sub.ARN, sub.Pending = "", true
			}
			out = append(out, sub)
		}
		if page.NextToken == nil || *page.NextToken == "" {
			return out, nil
		}
		next = page.NextToken
	}
}

// Subscribe asks SNS to subscribe an address by email, which sends it a
// confirmation link.
func (a *AlertSubscriptions) Subscribe(ctx context.Context, address string) error {
	_, err := a.API.Subscribe(ctx, &sns.SubscribeInput{TopicArn: aws.String(a.TopicARN), Protocol: aws.String("email"), Endpoint: aws.String(address)})
	return err
}

// Unsubscribe removes a confirmed subscription by its ARN.
func (a *AlertSubscriptions) Unsubscribe(ctx context.Context, arn string) error {
	_, err := a.API.Unsubscribe(ctx, &sns.UnsubscribeInput{SubscriptionArn: aws.String(arn)})
	return err
}

var _ guard.AlertSubscriptions = (*AlertSubscriptions)(nil)
