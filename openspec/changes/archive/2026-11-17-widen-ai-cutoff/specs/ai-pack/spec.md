## MODIFIED Requirements

### Requirement: The cutoff denies AI access only
The AI deny policy SHALL deny every Bedrock and AgentCore action that starts paid model work or a new AI spend commitment, through either the `bedrock-runtime` or the `bedrock-mantle` endpoint, and Bedrock API key use, and SHALL deny nothing else: no action outside Bedrock and AgentCore, and no action that only reads, lists, or stops work.

#### Scenario: A cut-off account keeps its other services
- **WHEN** an account is cut off by the AI pack
- **THEN** its Bedrock and AgentCore calls are refused, and its EC2, S3, and other calls are not

#### Scenario: The OpenAI-compatible endpoint
- **WHEN** a cut-off account calls `/v1/chat/completions` on the `bedrock-mantle` endpoint
- **THEN** the call is refused

#### Scenario: Agents, knowledge bases, and batch jobs
- **WHEN** a cut-off account calls `InvokeAgent`, `RetrieveAndGenerate`, or `CreateModelInvocationJob`
- **THEN** each call is refused

#### Scenario: Looking and cleaning up still work
- **WHEN** a cut-off account lists its models, reads a batch job's status, or deletes a provisioned throughput
- **THEN** each call is allowed

## ADDED Requirements

### Requirement: Accounts already cut off get a widened cutoff in place
When a release widens the AI deny policy, updating the stack SHALL apply the wider policy to accounts already cut off, without a new cutoff, a lift, or a new cutoff id.

#### Scenario: An update during a cutoff
- **WHEN** an account is cut off and the stack is updated to a release whose cutoff also denies mantle inference
- **THEN** the account's mantle calls are refused from then on, and its cutoff id and record are unchanged
