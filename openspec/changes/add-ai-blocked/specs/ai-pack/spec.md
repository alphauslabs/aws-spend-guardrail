## ADDED Requirements

### Requirement: Blocked use is denied in enforce scopes
For each account covered by an AI scope in `enforce` mode, the guard SHALL deny the kinds of use blocked for it, within 5 minutes of the config applying, and SHALL stop denying a kind within 5 minutes of an approved change allowing it. Blocking SHALL deny only that kind of use. A `dry-run` scope SHALL block nothing. The guard SHALL record each change to what is blocked where as an inbox event.

#### Scenario: Agents blocked
- **WHEN** account 730335604511's enforce scope blocks `agents`
- **THEN** its `InvokeInlineAgent` calls are refused, and its `Converse` calls still work

#### Scenario: Async invokes blocked
- **WHEN** an account's enforce scope blocks `async_invoke`
- **THEN** its `StartAsyncInvoke` calls are refused, and its `InvokeModel` calls to the same model still work

#### Scenario: A dry-run scope
- **WHEN** an account's scope is in dry-run and blocks `agents`
- **THEN** its agent calls are not refused

#### Scenario: Allowed again
- **WHEN** an approved change removes `agents` from the account's blocked list and the delay has passed
- **THEN** within 5 minutes its agent calls work again

### Requirement: The block policy stays in step and within limits
The guard SHALL keep one block policy, attached to the organization root only while some account has something blocked, listing each blocked account under the kinds blocked for it. When the policy would exceed AWS's size limit, the guard SHALL keep the last policy it wrote and alert once. When the policy can't be attached or updated, the guard SHALL alert once, retry on each pull, and alert when it succeeds.

#### Scenario: Nothing blocked any more
- **WHEN** the last blocked kind is allowed for the last account
- **THEN** the guard detaches the block policy from the root

#### Scenario: Too many accounts
- **WHEN** blocking would list more accounts than fit in a 5,120-character policy
- **THEN** the guard keeps the policy as it was, and alerts once naming how many accounts it couldn't add
