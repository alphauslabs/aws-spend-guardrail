# Tasks

## 1. Records

- [ ] 1.1 Add `EventCategory` to `activity.Record`, parsed from `eventCategory`; a record without it parses as `Management` (design D1, D2)
- [ ] 1.2 Add a trail-file fixture to `internal/activity/testdata` built from the 2026-11-17 test records, scrubbed of real account and caller ids: the management and data copies of one `Converse` call, a management-only `Converse`, and one data-only `InvokeModelWithBidirectionalStream`; test that `ParseFile` keeps each record's category

## 2. AI pack

- [ ] 2.1 In `internal/packs/ai/ai.go`, make `Wants` return false for a record whose category is `Data`, with a comment citing this change (design D1, D2)
- [ ] 2.2 Test with the fixture through the engine: the double-logged call adds one call and its tokens once; a model not on the allowlist, logged twice, cuts off once with one event; the management-only call is counted as before

## 3. Docs and build

- [ ] 3.1 Update `docs/ai-guard.md` (each call is counted once whether or not the trail logs Bedrock data events) and `docs/aws-facts.md` (with model data events on, a runtime call is logged as both a management and a data event, same request id, confirmed 2026-11-17); add a CHANGELOG `Fixed` entry with an Upgrade section (update the stack's package; no template change; rollback brings back the double count on such trails) and Security-relevant: None; follow the docs' style (no em-dashes, Oxford comma)
- [ ] 3.2 Run `goimports`, `make`, `go vet ./...`, `go test ./...`, and cfn-lint; verify all pass

## 4. Live check

- [ ] 4.1 With the test organization trail still logging `AWS::Bedrock::Model` data events, deploy this build to e2e with the AI guard on and `TrailBucket` set to the test trail's bucket, then from e2enormal make one `Converse` and one `ConverseStream` call; confirm in the trail files that the `ConverseStream` completion record is not only a data event (design D3), and that the account's AI totals grew by exactly two calls with their token counts
- [ ] 4.2 Remove the test setup: turn the AI guard off and restore `TrailBucket`, delete the test trail and its bucket, disable CloudTrail's organization access, and delete the `p2-async-730335604511` bucket in e2enormal
