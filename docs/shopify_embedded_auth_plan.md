# Shopify Embedded Auth Implementation Plan

Last updated: 2026-02-20
Owner: `codex` (implementation handoff document)
Status: `completed`

## 1) Scope and Intent

Implement missing Shopify embedded-app auth primitives in `go-services`:

1. App Bridge session JWT validation.
2. Shopify session-token exchange.
3. Replay protection ledger (`jti` + TTL).
4. Transport-agnostic orchestration service.
5. Typed contracts and typed/sentinel errors.
6. Full unit/integration coverage.

This repository is a library (not an API server). No HTTP route/server code should be added.

## 2) Key Decisions (Locked)

1. `jti` is consumed even when token exchange fails.
2. Add a dedicated embedded-auth contract now (not metadata-based hacks).
3. Introduce a domain `ReplayLedger` interface with atomic claim semantics.
4. Provide default in-memory replay ledger implementation.
5. Keep OAuth auth-code behavior intact.
6. Prefer explicit typed structs over `map[string]any` for request/response paths.
7. API/contracts may be broken now if needed for long-term design quality.

## 3) Existing Code Baseline

Relevant existing files/patterns:

- OAuth provider shape:
  - `providers/shopify/provider.go`
  - `providers/oauth2_provider.go`
- Auth strategy lifecycle:
  - `core/auth_strategy_runtime.go`
  - `core/service.go`
  - `core/contracts.go`
- Replay/TTL patterns:
  - `providers/shopify/webhook.go`
  - `core/oauth_state.go`
  - `inbound/dispatcher.go`
- JWT support utilities:
  - `auth/jwt_support.go`

Baseline test health (verified): `"/Users/goliatone/.g/go/bin/go test ./..."` passes.

## 4) Target Architecture

### 4.1 Core contracts (new/updated)

Introduce dedicated embedded auth contract in `core`:

- `EmbeddedRequestedTokenType` enum (`offline`, `online`)
- `EmbeddedAuthRequest`
- `EmbeddedSessionClaims`
- `EmbeddedAccessToken`
- `EmbeddedAuthResult`
- `EmbeddedAuthService` interface

Supporting provider-neutral primitives:

- `ReplayLedger` interface:
  - `Claim(ctx, key, ttl) (accepted bool, err error)` (atomic claim)
- `SessionTokenValidator` interface
- `SessionTokenExchanger` interface

Potential runtime surface additions:

- `core.Service.AuthenticateEmbedded(ctx, req EmbeddedAuthRequest) (EmbeddedAuthResult, error)`
- `core.IntegrationService` updated accordingly.

### 4.2 Shopify embedded implementation package

Create package: `providers/shopify/embedded`

Components:

1. `validator.go`
   - Parse JWT.
   - Enforce `HS256` only.
   - Verify signature with app secret.
   - Validate claims: `iss`, `dest`, `aud`, `exp`, `nbf`, `iat` (sanity), `jti`.
   - Extract and normalize shop from `dest`.
   - Optional expected-shop check.
   - Configurable clock skew tolerance.

2. `exchange_client.go`
   - POST `https://{shop}/admin/oauth/access_token`
   - Form body:
     - `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`
     - `subject_token=<session_jwt>`
     - `subject_token_type=urn:ietf:params:oauth:token-type:id_token`
     - `requested_token_type`:
       - default offline: `urn:shopify:params:oauth:token-type:offline-access-token`
       - optional online: `urn:shopify:params:oauth:token-type:online-access-token`
     - `client_id`, `client_secret`
   - Return typed exchange result (`access_token`, scope, expiry metadata, raw metadata).

3. `replay_ledger.go`
   - Interface may reuse `core.ReplayLedger` or local alias if staged.
   - Default in-memory implementation:
     - key = `provider + ":" + shop + ":" + jti`
     - TTL-aware
     - pruning
     - capacity limits
     - deterministic replay error handling.

4. `service.go`
   - Orchestrator:
     1. Validate session JWT.
     2. Build replay key and claim.
     3. Exchange session JWT for access token.
     4. Return normalized `EmbeddedAuthResult` + `core.ActiveCredential`.
   - Transport-agnostic; injected HTTP client, clock, validator, exchanger, ledger.

### 4.3 Error model

Add typed/sentinel errors in `providers/shopify/embedded/errors.go`, and wire into `core` error mapping if surfaced via `core.Service`:

- `ErrInvalidSessionToken`
- `ErrUnsupportedJWTAlgorithm`
- `ErrInvalidAudience`
- `ErrInvalidDestination`
- `ErrMissingJTI`
- `ErrReplayDetected`
- `ErrTokenExchangeFailed`

Optional typed wrappers:

- `ValidationError{Code, Field, Cause}`
- `ExchangeError{StatusCode, ErrorCode, Description, Cause}`

## 5) Replay-Ledger and Generic Cache Guidance

Do not use generic `Get/Set` cache as replay authority unless it supports atomic set-if-absent.

Required semantics for replay safety:

- Single operation: claim key iff absent + set TTL.
- No `Get` + `Set` race.

If integrating with shared cache package later, add adapter only when backend provides atomic semantics (`SetIfAbsent`/`Add`/`SET NX` equivalent).

## 6) Implementation Phases and Checklist

Use this section as the execution tracker.

### Phase A: Contracts and Service Surface

- [x] Add embedded auth contracts to `core/contracts.go`.
- [x] Add/adjust service interface exposure (`IntegrationService`, `services.go` aliases).
- [x] Add service method skeleton in `core` (if included in this iteration).
- [x] Add baseline contract tests in `core`.

### Phase B: Shopify JWT Validator

- [x] Add validator config + claim structs.
- [x] Implement HS256 verification and alg rejection.
- [x] Implement claim validation (`iss`, `dest`, `aud`, `exp`, `nbf`, `iat`, `jti`).
- [x] Implement shop extraction from `dest`.
- [x] Implement expected-shop match option.
- [x] Add clock skew handling.
- [x] Add unit tests:
  - [x] valid token
  - [x] bad signature
  - [x] bad audience
  - [x] expired token
  - [x] `nbf` in future
  - [x] invalid `dest`/`iss`
  - [x] missing `jti`

### Phase C: Replay Ledger

- [x] Define replay-ledger contract.
- [x] Implement in-memory store with:
  - [x] TTL expiration
  - [x] pruning
  - [x] capacity bound/eviction
  - [x] deterministic replay result
- [x] Add unit tests:
  - [x] first claim accepted
  - [x] replay rejected
  - [x] accepted after TTL expiry

### Phase D: Token Exchange Client

- [x] Implement typed request/response.
- [x] Implement form encoding and headers.
- [x] Implement offline default and online optional mode.
- [x] Implement error payload mapping.
- [x] Add unit tests:
  - [x] expected request body + headers
  - [x] offline/online requested token type
  - [x] error mapping

### Phase E: Orchestrator Service

- [x] Implement orchestrator service with DI dependencies.
- [x] Ensure replay is claimed before exchange and stays consumed on exchange failure.
- [x] Map token exchange response into normalized auth result + active credential.
- [x] Add unit/integration tests with mocked HTTP + ledger.

### Phase F: Wiring and Docs

- [x] Wire into provider/service surface intended for consumers.
- [x] Ensure OAuth auth-code behavior remains unchanged.
- [x] Add docs in `README.md` and/or `docs/`.
- [x] Run full test suite.

## 7) File Plan

Expected files to create/update:

- New:
  - `providers/shopify/embedded/contracts.go`
  - `providers/shopify/embedded/errors.go`
  - `providers/shopify/embedded/validator.go`
  - `providers/shopify/embedded/exchange_client.go`
  - `providers/shopify/embedded/replay_ledger.go`
  - `providers/shopify/embedded/service.go`
  - `providers/shopify/embedded/*_test.go`
- Update:
  - `core/contracts.go`
  - `core/service.go` (if exposing embedded auth through core service)
  - `services.go` (alias exposure)
  - `README.md`
  - `provider_factories.go` (only if factory exposure is desired)

## 8) API and Migration Notes

Given no external consumers, prefer contract quality over compatibility.

Possible breaking updates to make now:

- Add explicit embedded auth contracts in `core`.
- Optionally tighten auth-related map-based metadata usage where typed alternatives are introduced.
- Keep existing OAuth2 auth-code flow behavior unchanged to avoid regressions.

## 9) Test Matrix (Must Pass)

1. Package-level:
   - `"/Users/goliatone/.g/go/bin/go" test ./providers/shopify/embedded -v`
2. Affected broader packages:
   - `"/Users/goliatone/.g/go/bin/go" test ./providers/shopify ./core ./auth ./inbound ./webhooks`
3. Full suite:
   - `"/Users/goliatone/.g/go/bin/go" test ./...`

## 10) Fresh Session Bootstrap

For a new Codex session, do this first:

1. Read this file: `docs/shopify_embedded_auth_plan.md`
2. Check repo status:
   - `git status --short`
3. Run baseline tests:
   - `"/Users/goliatone/.g/go/bin/go" test ./...`
4. Continue from first unchecked phase item.
5. After each completed task, tick its checkbox and append a session note below.

## 11) Session Log

Use this log to preserve continuity.

- [2026-02-20] Plan created. Decisions locked:
  - `jti` consumed even on exchange failure.
  - dedicated embedded auth service contract.
  - domain replay ledger with atomic claim semantics.
- [2026-02-20] Implementation completed end-to-end:
  - Added embedded auth contracts and service method in `core`.
  - Added generic replay ledger interface + default in-memory implementation with tests.
  - Added `providers/shopify/embedded` validator, exchange client, orchestrator, typed errors, and tests.
  - Wired Shopify provider and `core.Service.AuthenticateEmbedded`.
  - Updated README docs and verified with `"/Users/goliatone/.g/go/bin/go" test ./...`.
