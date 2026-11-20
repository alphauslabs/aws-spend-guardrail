## 1. Consumer

- [x] 1.1 `outbox.Consumer` gains an inbox lookup (`HasEvent(ctx, datasetID, id) (bool, error)`, on the reader client) and a logger
- [x] 1.2 On an `InvalidArgument` write error, look up the id: found counts as delivered, not found or a failed lookup is a batch item failure
- [x] 1.3 Log each failed write and each already-delivered event per design D4, without the detail
- [x] 1.4 `jennah.Client.HasEvent`: a query on the inbox events table by `event_id`, limit 1

## 2. Outbox function

- [x] 2.1 `guard/main.go`: with `OUTBOX_ONLY=true`, read only the writer and reader secrets, build only the consumer, and refuse every other input
- [x] 2.2 `guard/template.yaml`: `OutboxFunction` (same package, `OUTBOX_ONLY`, 30-second timeout), `OutboxRole`, `OutboxLogGroup`; `OutboxFunctionEventSource` (a new mapping replacing `OutboxEventSource`) on the new function with `ScalingConfig.MaximumConcurrency: 2`; drop SQS receive and delete from `GuardRole`; `TEMPLATE_REVISION` 8
- [x] 2.3 `MinTemplateRevision` 8 in `internal/guard/deployment.go`
- [x] 2.4 cfn-lint passes

## 3. Daily heartbeat id

- [x] 3.1 `Daily` emits `heartbeat:<date>:<digest>`, the digest from the heartbeat's detail

## 4. Tests

- [x] 4.1 Consumer: an `InvalidArgument` write whose id the inbox holds is delivered and logged; one whose id it doesn't hold is a failure; a failed lookup is a failure; other errors are failures without a lookup
- [x] 4.2 Consumer logs carry the id, kind, and error, and never the detail
- [x] 4.3 Two daily runs on one day with different state emit two heartbeats with different ids; the same state gives the same id
- [x] 4.4 `route`/outbox-only mode: an outbox event is handled, a schedule input is refused
- [x] 4.5 `go vet ./...` and `go test ./...` pass

## 5. Docs and release

- [x] 5.1 `docs/operating.md`: the daily heartbeat id, and where outbox failures are logged
- [x] 5.2 CHANGELOG `[Unreleased]`: the fix; Upgrade: update the stack with the release's template and package (revision 8); rollback needs the previous template; Security-relevant: the new function and role, and `GuardRole` losing SQS receive and delete
- [x] 5.3 Release as a patch and deploy to e2e; check a pull's `config_applied` reaches the inbox within 5 minutes while the trail task runs, a second daily run writes its own heartbeat, and the outbox log shows no failures
