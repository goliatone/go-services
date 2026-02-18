// Package core contains canonical services domain contracts, entities, and
// orchestration logic. Lower-level adapters must depend on this package; core
// must not depend on provider-specific or transport-specific adapters.
//
// Credential persistence is codec-driven (format + version) so active
// credential payloads can roundtrip safely across schema/runtime upgrades.
// Grant persistence separates snapshot state from immutable grant events, and
// supports transactional snapshot+event writes for strict reconciliation paths.
// Callback URL resolution is pluggable via CallbackURLResolver so consumers can
// enforce app-specific callback URL schemas without provider-level branching.
package core
