# AWS facts

What the guard relies on, as confirmed in a test organization. Each entry says how it was checked. Entries marked "to confirm" are still assumptions.

| Fact | Status | How it was checked |
|---|---|---|
| SCPs must be enabled on the organization root before any freeze policy can be attached. A new organization may have them off. | confirmed | The test organization's root had no policy types. `enable-policy-type` moved it from `PENDING_ENABLE` to `ENABLED` within seconds and attached `FullAWSAccess` to the accounts. |
| An SCP rejects a wildcard in the service part of an action, such as `*:Describe*` in `NotAction`. | confirmed | Creating freeze style B with `*:Describe*`, `*:Get*` and the like failed: "The provided policy document does not meet the requirements of the specified policy type." Style B now lists exceptions per service. |
| An SCP accepts a wildcard after the service, such as `ec2:Describe*`, and accepts the per-service style B list (273 entries, 4,883 characters). | confirmed | The policy was created with `create-policy` in the test organization, then deleted. |
| A CloudFormation `Rules` assertion using `AWS::AccountId` refuses a stack created in the wrong account before any resource is created. Rules are not evaluated when a change set is created, only when the stack is. | confirmed | `create-stack` with `ExpectedPayerAccountId` set to another account failed with "Parameter validation failed: assertion error" and no resource events. CREATE change sets with the same value were created without error. |
| Without a CloudTrail trail, Organizations account events (such as `MoveAccount`) do not reach EventBridge, although CloudTrail's event history records them. | confirmed | In an organization with no trail, an account was moved into a temporary OU and back. Both `MoveAccount` calls appeared in CloudTrail event history; an EventBridge rule on `source: aws.organizations` in us-east-1 matched nothing in 15 minutes (no matched events, no invocations, no failed deliveries). |
| With a trail recording management events, which EventBridge events arrive for account creation, invitation and moves. | to confirm | Needed before relying on `ReactToAccountEvents`. |
| SCP size limit 10,240 characters; at most 10 SCPs attached directly to one root, OU, or account. | confirmed | AWS Organizations quotas documentation, checked 2026-11-11 (the limits were 5,120 and 5 earlier). Style B is within the size limit as created above. |
| An attached SCP takes effect within seconds. | confirmed | A deny on Bedrock invoke calls attached to a member account refused calls 1.3 to 3.4 seconds after `AttachPolicy` returned, and calls succeeded again as fast after `DetachPolicy`, over four rounds in the test organization on 2026-11-11. |
| A Budgets action of type `APPLY_SCP_POLICY` can target a member account. | to confirm | |
| A `MANUAL` Budgets action stays pending without attaching. | to confirm | |
| `ExecuteBudgetAction` with `REVERSE_BUDGET_ACTION` detaches the policy. | to confirm | |
| What happens to an executed action at the start of a new budget period. | to confirm | |
| Actions per budget, and budgets per account. | to confirm | |
| How often Budgets refreshes actual cost. | to confirm | |
| The text of a budget notification and a budget action notification on SNS. | to confirm | The guard reads the budget name from a `Budget Name:` line or a `budgetName` JSON field. |
| Bedrock runtime calls (`InvokeModel`, `InvokeModelWithResponseStream`, `Converse`, `ConverseStream`) are CloudTrail management events, free and recorded by default. | confirmed | AWS Bedrock CloudTrail documentation, and calls from a member account seen in event history 3 to 4 minutes later, 2026-11-11. Bedrock Agents, knowledge bases, flows, and async invokes are data events. |
| Non-streaming Bedrock calls carry `inputTokens`, `outputTokens`, and `inferenceRegion` in `additionalEventData`; streaming calls carry token counts on a separate completion record (`eventType: AwsServiceEvent`). | confirmed | Recorded calls in the test organization, 2026-11-11. |
| A call made with a Bedrock API key has `additionalEventData.callWithBearerToken: true`, with the key's IAM user as the caller, also through an organization trail. | confirmed | A temporary API key in the test organization, 2026-11-11. |
| An organization trail's S3 files hold member calls 0.5 to 5 minutes after the call. Through the trail's CloudWatch Logs group, 2 to 2.5 minutes. | confirmed | Four samples each, temporary organization trails, 2026-11-11. |
| CloudTrail events reach only the EventBridge bus of the account where the call happened, even with an organization trail, and no bus without a trail. | confirmed | EventBridge rules in the management and a member account, 2026-11-11. |
| A trail can't be narrowed to Bedrock management events: `eventSource` and `eventName` selectors are refused for management events; only KMS and RDS Data API can be excluded. | confirmed | `put-event-selectors` on a test trail, 2026-11-11. |
| Bedrock token-per-minute quotas can't be lowered below AWS's default. | confirmed | `request-service-quota-increase` with a lower value was refused, 2026-11-11. |
| AgentCore runtime, code interpreter, and browser calls are not management events. Only AgentCore Gateway is documented as a data event (`AWS::BedrockAgentCore::Gateway`). | confirmed | A code interpreter session (start, invoke, stop) in the test organization did not appear in event history after 4 minutes, 2026-11-11; AgentCore CloudTrail documentation. |
| Bedrock logging-configuration changes are management events named `PutModelInvocationLoggingConfiguration` and `DeleteModelInvocationLoggingConfiguration`, recorded also when refused. | confirmed | Event history in the test organization, 2026-11-11. |
