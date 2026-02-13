// Package webhooks contains webhook verification and dispatch components.
//
// Delivery processing is driven by a claim lifecycle:
// pending/retry_ready -> processing -> processed|dead.
// This makes retries and crash-recovery explicit and prevents transient
// failures from being deduped as permanently processed.
package webhooks
