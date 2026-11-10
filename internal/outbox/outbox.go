// Package outbox delivers guard events to Jennah through an SQS queue, so an
// event survives Jennah being unreachable and is written at most once.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
)

// Sender is the part of the SQS client the producer uses.
type Sender interface {
	SendMessage(ctx context.Context, in *sqs.SendMessageInput, opts ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

// Producer queues events. It implements guard.EventSink.
type Producer struct {
	SQS      Sender
	QueueURL string
}

// Emit queues one event. The event keeps its id and time through every retry.
func (p *Producer) Emit(ctx context.Context, e jennah.Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = p.SQS.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.QueueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("queue event %s: %w", e.ID, err)
	}
	return nil
}

// Committer writes one event to the inbox. It is the guard's writer
// credential.
type Committer interface {
	CommitEvent(ctx context.Context, datasetID string, e jennah.Event) error
}

// Consumer writes queued events to Jennah.
type Consumer struct {
	Jennah    Committer
	DatasetID string
}

// Handle writes each message's event and reports the ones that failed, so SQS
// redelivers only those. After the queue's retries they move to the
// dead-letter queue, whose alarm alerts locally.
func (c *Consumer) Handle(ctx context.Context, ev events.SQSEvent) (events.SQSEventResponse, error) {
	var resp events.SQSEventResponse
	for _, m := range ev.Records {
		var e jennah.Event
		if err := json.Unmarshal([]byte(m.Body), &e); err != nil || e.ID == "" {
			// A message that isn't an event will never succeed; let it go to
			// the dead-letter queue for a human to look at.
			resp.BatchItemFailures = append(resp.BatchItemFailures, events.SQSBatchItemFailure{ItemIdentifier: m.MessageId})
			continue
		}
		if err := c.Jennah.CommitEvent(ctx, c.DatasetID, e); err != nil {
			resp.BatchItemFailures = append(resp.BatchItemFailures, events.SQSBatchItemFailure{ItemIdentifier: m.MessageId})
		}
	}
	return resp, nil
}
