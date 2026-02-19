# go-services

`go-services` is an integration runtime for Go applications that need to connect to third-party providers (OAuth2, API key/PAT, HMAC, mTLS/basic, AWS SigV4), persist credentials safely, execute provider operations, and process webhook/sync lifecycles.

It is designed as a backend package, not as a standalone API server.

## Status

- Current release: `v0.1.0`
- API stability: pre-`v1.0.0` (breaking changes are still possible)

## What This Package Does

- Manages connection lifecycle: `Connect`, callback completion, refresh, revoke, and re-consent.
- Persists integration state: connections, credentials, subscriptions, sync cursors, installations, grant snapshots/events, lifecycle outbox, and webhook deliveries.
- Enforces capability access based on granted permissions.
- Executes provider operations with transport abstraction, request signing, idempotency keys, retry policies, and optional adaptive rate-limit policy.
- Supports inbound webhook dispatch with claim/complete/fail idempotency semantics.
- Exposes command/query handlers and facade wiring for integration with `go-command`.

## When To Use It

Use `go-services` when you need one place to standardize:

- Multi-provider auth and credential lifecycle.
- Secure credential storage and key rotation compatibility.
- Provider call execution behavior (timeouts, retries, signing, rate-limit state).
- Webhook and sync processing with durable state and recovery.
- Integration-specific observability signals.

## When Not To Use It

- You only need direct one-off HTTP calls to a single provider.
- You do not want database-backed integration state.
- You want a fully hosted integration platform instead of embedding runtime primitives in your app.

## Security and Secrets

Credential payloads are codec-encoded and encrypted before persistence. A `SecretProvider` is required for credential persistence operations.

Included secret provider implementations:

- `security.AppKeySecretProvider`: local AES-GCM encryption with key id/version metadata.
- `security.KMSSecretProvider`: envelope encryption via external KMS client.
- `security.VaultSecretProvider`: envelope encryption via external Vault client.
- `security.FailoverSecretProvider`: primary/fallback provider policy with diagnostics (`strict_fail` or `fallback_allowed`).
- Key rotation windows and decrypt-compatibility controls are available for KMS/Vault providers.

## Observability and Reliability

- Structured operation logging and metrics are emitted by core service paths.
- Default metric names include:
  - `services.<operation>.total`
  - `services.<operation>.duration_ms`
- Common operation tags include `operation`, `status`, `provider_id`, `scope_type`, `scope_id`, `connection_id`.
- Lifecycle outbox dispatcher supports claim/ack/retry with bounded backoff and max attempts.
- Webhook delivery processing uses explicit claim state transitions (`pending/retry_ready -> processing -> processed|dead`) to support retry-safe recovery.
- Runbook: `docs/runbooks/services_failure_modes.md`

## Built-in Providers

Factory functions are exported at package root:

- `GitHubProvider`
- `GmailProvider`
- `DriveProvider`
- `DocsProvider`
- `CalendarProvider`
- `ShopifyProvider`
- `InstagramProvider`
- `FacebookProvider`
- `TikTokProvider`
- `PinterestProvider`
- `GoogleShoppingProvider`
- `AmazonProvider`
- `SalesforceProvider`
- `WorkdayProvider`

You can also register custom providers through `core.Registry`.

## Install

```bash
go get github.com/goliatone/go-services@latest
```

## Quick Start

### 1. Apply embedded migrations

`go-services` embeds Postgres and SQLite migrations:

- `services.GetMigrationsFS()`
- `migrations.Filesystems()`
- `migrations.Register(...)`

```go
import (
	"context"
	"io/fs"

	servicemigrations "github.com/goliatone/go-services/migrations"
)

_, err := servicemigrations.Register(
	context.Background(),
	func(ctx context.Context, dialect string, source string, fsys fs.FS) error {
		return myMigrationRunner.Register(ctx, dialect, source, fsys)
	},
)
```

### 2. Build a service with stores, secrets, and providers

```go
package main

import (
	"context"
	"log"

	services "github.com/goliatone/go-services"
	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/github"
	"github.com/goliatone/go-services/security"
	sqlstore "github.com/goliatone/go-services/store/sql"
	"github.com/uptrace/bun"
)

func newService(db *bun.DB) (*services.Service, error) {
	secretProvider, err := security.NewAppKeySecretProviderFromString("replace-with-secret-key-material")
	if err != nil {
		return nil, err
	}

	repositoryFactory, err := sqlstore.NewRepositoryFactoryFromDB(db)
	if err != nil {
		return nil, err
	}

	githubProvider, err := services.GitHubProvider(github.Config{
		ClientID:     "github-client-id",
		ClientSecret: "github-client-secret",
	})
	if err != nil {
		return nil, err
	}

	registry := core.NewProviderRegistry()
	if err := registry.Register(githubProvider); err != nil {
		return nil, err
	}

	return services.NewService(
		services.DefaultConfig(),
		services.WithPersistenceClient(db),
		services.WithRepositoryFactory(repositoryFactory),
		services.WithSecretProvider(secretProvider),
		services.WithRegistry(registry),
	)
}

func main() {
	var db *bun.DB // initialize this with your Bun database connection

	svc, err := newService(db)
	if err != nil {
		log.Fatal(err)
	}

	begin, err := svc.Connect(context.Background(), services.ConnectRequest{
		ProviderID:  "github",
		Scope:       core.ScopeRef{Type: "user", ID: "usr_123"},
		RedirectURI: "https://app.example.com/integrations/callback",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("send user to provider auth URL: %s", begin.URL)
}
```

## Package Map

- `core`: domain contracts, service orchestration, permission/rate-limit/runtime logic.
- `providers`: built-in provider implementations.
- `store/sql`: Bun-backed repositories and stores.
- `security`: encryption/secret providers and key-rotation helpers.
- `transport`: REST/GraphQL/protocol transport adapters and resolver registry.
- `webhooks`, `inbound`, `sync`: webhook processing and sync orchestration.
- `command`, `query`, `facade`: command/query handlers and grouped facade access.
- `adapters`: compatibility adapters for `go-command`, `go-job`, and `go-logger`.
- `migrations`: migration filesystem discovery and registration helpers.

## Additional Docs

- `CHANGELOG.md`
- `docs/identity_profiles.md`
- `docs/runbooks/services_failure_modes.md`
