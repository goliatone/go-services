# Services Failure-Mode Runbooks

This runbook defines detection, triage, mitigation, and recovery actions for common production failure modes in `go-services`.

## Provider Outage

### Signals
- Elevated `services.refresh.total{status=failure}`.
- Increased worker retries for provider-dependent jobs.
- HTTP 5xx spikes from provider APIs.

### Immediate Actions
1. Confirm outage scope: provider-global vs endpoint-specific.
2. Reduce job concurrency for affected provider queue routes.
3. Keep latest active credentials; avoid forced revokes during outage.
4. Increase retry intervals via adaptive backoff policy.

### Recovery
1. Re-enable normal concurrency gradually.
2. Replay deferred jobs from queue/backlog.
3. Validate refresh and capability success rates return to baseline.

## Token Revocation / Invalid Refresh Token

### Signals
- Repeated refresh failures with invalid grant/token errors.
- Connection state transitioning to `pending_reauth` or `needs_reconsent`.

### Immediate Actions
1. Mark impacted connection as `pending_reauth`.
2. Block write capabilities requiring revoked grants.
3. Surface re-consent action to operators.

### Recovery
1. Trigger re-consent flow and callback completion.
2. Verify new active credential version exists.
3. Confirm capability invocation success post re-consent.

## Webhook Replay / Duplicate Delivery

### Signals
- Rising webhook dedupe counters.
- Repeated delivery identifiers for same provider channel.

### Immediate Actions
1. Ensure dedupe ledger is healthy and writable.
2. Validate signature verification remains enabled.
3. Confirm replayed deliveries are acknowledged without duplicate side effects.

### Recovery
1. Audit dedupe TTL/window configuration.
2. Tune optional coalescing/debouncing windows for bursty sources.
3. Re-run targeted webhook integration tests if config changed.

## Outbox Lag / Dispatcher Backlog

### Signals
- Outbox depth and event lag increasing.
- Lifecycle projector delivery delay alarms.

### Immediate Actions
1. Check dispatcher worker health and claim loop errors.
2. Increase dispatcher workers if downstream dependencies are healthy.
3. Keep durable outbox writes enabled; do not bypass outbox path.

### Recovery
1. Replay pending outbox events until lag normalizes.
2. Validate projector idempotency ledger prevents duplicates.
3. Confirm activity and notification projections are caught up.

## Notification Projector Failure

### Signals
- Notification send failures/retries rising.
- `service_notification_dispatches` failure states increasing.

### Immediate Actions
1. Keep core transaction path unchanged (post-commit projector failures must not rollback core state).
2. Retry through async projector with idempotency key.
3. Route failures to dead-letter handling after max attempts.

### Recovery
1. Replay failed notification dispatches from ledger.
2. Verify recipient resolver and definition mapping outputs.
3. Confirm successful sends and retry drain completion.

## Escalation and Ownership

- Primary owner: integrations backend on-call.
- Secondary owner: platform runtime/worker on-call.
- Escalate to provider support when external outage or API behavior change is confirmed.

## Verification Checklist (Post-Incident)

1. Error/latency metrics returned to pre-incident range.
2. No stuck `pending_reauth` connections without operator task.
3. Webhook dedupe and outbox lag within SLO thresholds.
4. Notification failure backlog drained.
