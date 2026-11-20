// Package outbox delivers guard events to Jennah through an SQS queue, so an
// event survives Jennah being unreachable and is written at most once.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
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

// Finder looks up an event in the inbox by id. It is the guard's reader
// credential, since the writer key can't read.
type Finder interface {
	HasEvent(ctx context.Context, datasetID, id string) (bool, error)
}

// Consumer writes queued events to Jennah.
type Consumer struct {
	Jennah    Committer
	Inbox     Finder
	DatasetID string
	// Logf logs a failed write; log.Printf when nil. Lines carry the event's
	// id and kind, never its detail (fix-outbox-delivery D4).
	Logf func(format string, args ...any)
}

func (c *Consumer) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Handle writes each message's event and reports the ones that failed, so SQS
// redelivers only those. After the queue's retries they move to the
// dead-letter queue, whose alarm alerts locally. A write refused as invalid
// counts as delivered when the inbox already holds the event's id: the first
// event written under an id stands (fix-outbox-delivery D2).
func (c *Consumer) Handle(ctx context.Context, ev events.SQSEvent) (events.SQSEventResponse, error) {
	var resp events.SQSEventResponse
	fail := func(m events.SQSMessage) {
		resp.BatchItemFailures = append(resp.BatchItemFailures, events.SQSBatchItemFailure{ItemIdentifier: m.MessageId})
	}
	for _, m := range ev.Records {
		var e jennah.Event
		if err := json.Unmarshal([]byte(m.Body), &e); err != nil || e.ID == "" {
			// A message that isn't an event will never succeed; let it go to
			// the dead-letter queue for a human to look at.
			c.logf("outbox: message %s is not an event", m.MessageId)
			fail(m)
			continue
		}
		err := c.Jennah.CommitEvent(ctx, c.DatasetID, e)
		if err == nil {
			continue
		}
		if jennah.IsInvalid(err) && c.Inbox != nil {
			found, ferr := c.Inbox.HasEvent(ctx, c.DatasetID, e.ID)
			if ferr == nil && found {
				c.logf("outbox: %s (%s) already in the inbox; delivered", e.ID, e.Kind)
				continue
			}
			if ferr != nil {
				err = fmt.Errorf("%w; and %v", err, ferr)
			}
		}
		c.logf("outbox: write %s (%s) failed: %v", e.ID, e.Kind, err)
		fail(m)
	}
	return resp, nil
}
