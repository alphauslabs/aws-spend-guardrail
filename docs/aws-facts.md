# AWS facts

What the guard relies on, as confirmed in a test organization. Each entry says how it was checked. Entries marked "to confirm" are still assumptions.

| Fact | Status | How it was checked |
|---|---|---|
| SCPs must be enabled on the organization root before any freeze policy can be attached. A new organization may have them off. | confirmed | The test organization's root had no policy types. `enable-policy-type` moved it from `PENDING_ENABLE` to `ENABLED` within seconds and attached `FullAWSAccess` to the accounts. |
| An SCP rejects a wildcard in the service part of an action, such as `*:Describe*` in `NotAction`. | confirmed | Creating freeze style B with `*:Describe*`, `*:Get*` and the like failed: "The provided policy document does not meet the requirements of the specified policy type." Style B now lists exceptions per service. |
| An SCP accepts a wildcard after the service, such as `ec2:Describe*`, and accepts the per-service style B list (273 entries, 4,883 characters). | confirmed | The policy was created with `create-policy` in the test organization, then deleted. |
| SCP size limit 5,120 characters; at most 5 SCPs attached directly to one target. | to confirm | AWS documentation; style B is within the size limit as created above. |
| A Budgets action of type `APPLY_SCP_POLICY` can target a member account. | to confirm | |
| A `MANUAL` Budgets action stays pending without attaching. | to confirm | |
| `ExecuteBudgetAction` with `REVERSE_BUDGET_ACTION` detaches the policy. | to confirm | |
| What happens to an executed action at the start of a new budget period. | to confirm | |
| Actions per budget, and budgets per account. | to confirm | |
| How often Budgets refreshes actual cost. | to confirm | |
| The text of a budget notification and a budget action notification on SNS. | to confirm | The guard reads the budget name from a `Budget Name:` line or a `budgetName` JSON field. |
