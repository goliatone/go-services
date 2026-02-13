// Package inbound contains inbound surface handling abstractions.
//
// Provider-originated inbound paths use claim/complete/fail idempotency
// semantics so transient handler failures remain retryable.
package inbound
