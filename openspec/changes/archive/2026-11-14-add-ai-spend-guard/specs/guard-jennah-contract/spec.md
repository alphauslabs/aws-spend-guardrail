# Spec Delta

## ADDED Requirements

### Requirement: AI events and summaries reach the inbox
The guard SHALL write to the inbox an event for each AI cutoff and would-cut-off (cutoff id, account, the cap or tripwire, totals, and the top callers and models), each lift, and each stalled or resumed trail. Once an hour it SHALL write a usage summary per account and model with calls, input and output tokens, and estimated USD for that hour. It SHALL NOT write raw trail records or prompts to Jennah. The layout version SHALL stay the same, since existing events keep their shape.

#### Scenario: An hour of usage
- **WHEN** an account called two models during an hour
- **THEN** the inbox gets two summaries for that hour, one per model, with calls, tokens, and estimated USD

#### Scenario: A cutoff
- **WHEN** the AI pack cuts off an account
- **THEN** the inbox gets one cutoff event with its id, the reason, the totals, and the top callers and models
