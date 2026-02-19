# Changelog

## [Unreleased]

### API Notes

- Planning update: enabled `go-services (commerce/social provider expansion)` backend workstream (Phase 25 through Phase 29) as an active execution target in `SERVICES_TSK.md`.
- Planning update: added Phase `25.0` pre-task for planned breaking-API alignment before provider-specific implementations.
- Design update: documented explicit Phase 25 through Phase 29 breaking window in `SERVICES_TDD.md` for high-level execution contracts:
  - canonical provider operation request/response envelope alignment
  - standardized capability invocation and error mapping surfaces
  - unified extension registration surfaces for provider packs and command/query bundles
  - breaking changes remain allowed pre-`v1.0.0` when they improve API/DX
- Policy update: clarified project-wide pre-`v1.0.0` DX-first API evolution rule in `SERVICES_TDD.md` and `SERVICES_TSK.md`:
  - intentional breaking changes are allowed when they improve API clarity/correctness/DX
  - no bridge code, legacy shims, compatibility flags, or dual-surface support
  - every API break must be captured in `CHANGELOG.md` API notes and corresponding task completion notes with cutover actions
- Added AWS SigV4 signing support in core:
  - New `core.AuthKindAWSSigV4` auth kind constant.
  - New `core.AWSSigV4Signer` with header/query signing modes, canonical request/signature generation, session token support, and optional unsigned payload mode.
  - `Service.SignRequest` and provider-operation signing now resolve auth-kind-aware signers (`api_key`, `pat`, `hmac`, `basic`, `mtls`, `aws_sigv4`) when default bearer signer is active.
- Added OAuth2+SigV4 strategy wiring:
  - New `auth.OAuth2SigV4Strategy` that composes OAuth2 client-credentials token issuance with SigV4 signing profile metadata for runtime request mutation.
- Added provider operation signing diagnostics metadata normalization:
  - Runtime metadata now captures SigV4 signing diagnostics (`signing_profile`, `signing_mode`, `signed_host`, `signed_service`, `signed_region`, `signed_headers`) plus `clock_skew_hint_seconds` when detectable from response `Date` headers.
- Added shared webhook verification/extraction templates:
  - New reusable template constructors for Shopify, Meta, TikTok, Pinterest, Google, and Amazon webhook/notification surfaces.
- Added `providers/devkit` conformance fixtures for:
  - SigV4 signing behavior and replay-window validation.
  - Multi-provider webhook template verification and delivery-id extraction patterns.
- Added callback URL resolver hook in core:
  - New types: `core.CallbackURLResolver`, `core.CallbackURLResolverFunc`, `core.CallbackURLResolveRequest`, `core.CallbackURLResolveFlow`.
  - New option: `core.WithCallbackURLResolver(...)` (also re-exported from `services.WithCallbackURLResolver(...)`).
- `core.ServiceDependencies` (and alias `services.ServiceDependencies`) now includes `CallbackURLResolver`.
- `Service.Connect` and `Service.StartReconsent` now resolve callback URL via the configured resolver when `RedirectURI` is empty.
- Breaking: `core.ConnectionStore` now requires `FindByScopeAndExternalAccount(...)`.
- Breaking: `Service.CompleteCallback` now requires `CompleteAuthResponse.ExternalAccountID` to be non-empty.
- Breaking: `providers.OAuth2Provider.CompleteAuth` no longer synthesizes fallback account ids; it now requires `req.Metadata["external_account_id"]`.
- Behavior change: strict scope-based connection resolution is now fail-closed when multiple active connections exist for the same provider+scope (`ConnectionResolutionAmbiguous`).
- Behavior change: `InvokeCapability` now honors explicit `InvokeCapabilityRequest.ConnectionID` and validates provider/scope consistency.
- Breaking: facade activity query wiring no longer uses reflection fallback (`resolveActivityReader` / `safeReflectCall` were removed).
  - `services.NewFacade(...)` now fails fast when `WithActivityReader(...)` is not provided.
- Breaking: auth and lifecycle update contracts now use typed string aliases:
  - `core.AuthKind` is now the canonical type for `Provider.AuthKind()`, `AuthStrategy.Type()`, and `ProviderOperationResult.AuthStrategy`.
  - lifecycle update methods now require typed statuses (`ConnectionStatus`, `SubscriptionStatus`, `InstallationStatus`) instead of raw `string` parameters.
- Breaking: external form-data scope enforcement contracts now require explicit provider/scope context for fail-closed access:
  - `core.MappingSpecStore` scoped lookups/state transitions (`GetVersion`, `GetLatest`, `SetStatus`, `PublishVersion`) now require `providerID + scope`.
  - `core.MappingSpecLifecycleService` read/state-transition methods (`MarkValidated`, `Publish`, `GetVersion`, `GetLatest`) now require `providerID + scope`.
  - `core.SyncCheckpointStore` lookup methods (`GetByID`, `GetLatest`) now require `providerID + scope`.
  - `core.SyncConflictStore` scoped methods (`Get`, `ListByBinding`, `Resolve`) now require `providerID + scope`.
  - `core.ResolveSyncConflictRequest` now requires `ProviderID` and `Scope`.

### Migration / Fix

- If you construct `ServiceDependencies` using unkeyed composite literals, this can now fail to compile due to the new field.
  - Fix: switch to keyed literals (recommended) or add the new `CallbackURLResolver` positional field.
- If your connect/re-consent calls rely on empty `RedirectURI`, configure `WithCallbackURLResolver(...)` or continue passing explicit `RedirectURI` per request.
- If you implement `core.ConnectionStore`, add `FindByScopeAndExternalAccount(ctx, providerID, scope, externalAccountID) (Connection, bool, error)`.
- Ensure every auth strategy/provider sets a stable non-empty `ExternalAccountID` on `CompleteAuthResponse`.
- For OAuth2 providers using the shared `providers.OAuth2Provider`, set `external_account_id` in callback metadata before `CompleteAuth` is called.
- If you invoke capabilities by provider+scope only and a user can have multiple linked accounts, pass `InvokeCapabilityRequest.ConnectionID` to disambiguate account selection.
- If you construct facades, always pass an explicit activity reader:
  - `services.NewFacade(service, services.WithActivityReader(activityReader))`.
- If you implement `core.Provider` or `core.AuthStrategy`, update signatures:
  - `AuthKind() core.AuthKind` and `Type() core.AuthKind`.
- If you implement status-update store/service interfaces, update method signatures to typed statuses:
  - `ConnectionStore.UpdateStatus(..., core.ConnectionStatus, ...)`
  - `SubscriptionStore.UpdateState(..., core.SubscriptionStatus, ...)`
  - `InstallationStore.UpdateStatus(..., core.InstallationStatus, ...)`
- If you implement external form-data stores/services, update signatures and callsites for scoped access:
  - implement the new `providerID + scope` parameters on `MappingSpecStore`, `SyncCheckpointStore`, and `SyncConflictStore`.
  - pass `providerID + scope` when calling mapping lifecycle methods (`MarkValidated`, `Publish`, `GetVersion`, `GetLatest`).
  - populate `ResolveSyncConflictRequest.ProviderID` and `ResolveSyncConflictRequest.Scope`.

# [0.1.0](https://github.com/goliatone/go-services/tree/v0.1.0) - (2026-02-18)

## <!-- 1 -->🐛 Bug Fixes

- Oauth callback state is no longer burned on validation mismatch ([1d8c489](https://github.com/goliatone/go-services/commit/1d8c489401d837abff0ae9e41d57ddea65645bd4))  - (goliatone)
- Idenpotency key generation inlcude canonical query params ([31ae666](https://github.com/goliatone/go-services/commit/31ae666bf42b9180a4646cffdfccdce51bd875b1))  - (goliatone)
- Oauth state store bound and proactively cleaned ([cbc5d88](https://github.com/goliatone/go-services/commit/cbc5d8846f28cdf12e89f30a0fe5f8d31ed794cc))  - (goliatone)
- Service account jwt refresh support metadata provided signing key ([e113e4a](https://github.com/goliatone/go-services/commit/e113e4a2620ca40c7e7997e95c8417f2d5016f1b))  - (goliatone)
- Propagate claim delivery fail persistence errors ([2ba8a29](https://github.com/goliatone/go-services/commit/2ba8a29f2c272fa9dda3ac83c007ba6f511b8c06))  - (goliatone)
- Enforce lifecycle status in write paths ([0ff9fd1](https://github.com/goliatone/go-services/commit/0ff9fd162d81b6c710d5c789f6b0f08f3f4d1584))  - (goliatone)
- Harden oauth state generation ([8bbdaa5](https://github.com/goliatone/go-services/commit/8bbdaa560a997121c2dfee04b97c2d8f527cccd5))  - (goliatone)
- Harden redirect stricness override ([c5cf61d](https://github.com/goliatone/go-services/commit/c5cf61d30080d601e492d77c19d679fec2ea6291))  - (goliatone)
- Include require callback redirect ([39aa073](https://github.com/goliatone/go-services/commit/39aa073d704423499491769f33fd2edbd37e39b1))  - (goliatone)
- Proper token rotation ([dd27ded](https://github.com/goliatone/go-services/commit/dd27ded5bd48bc31c2fd17422409819b3966f187))  - (goliatone)
- Include signer in payload check ([ecddc15](https://github.com/goliatone/go-services/commit/ecddc15f2702695e4df77d8ee720a4255bd62526))  - (goliatone)
- Incldue callback redirect in config layer ([37230c1](https://github.com/goliatone/go-services/commit/37230c12c9d687945534a17e98eac04d6dd4d1ad))  - (goliatone)
- Return errors in dispatch ([598bb0a](https://github.com/goliatone/go-services/commit/598bb0ae7a96fa2f984cb499ddb9c8b330b998f0))  - (goliatone)
- Fix add rest body size limits ([f3b7ce6](https://github.com/goliatone/go-services/commit/f3b7ce635c9f1724ad1add8d1d1aa66917f38f72))  - (goliatone)
- Add default timeout to rest client ([df1a8d3](https://github.com/goliatone/go-services/commit/df1a8d3287b3c29e1f6bbaeeadc91d4077c3445b))  - (goliatone)
- Address reflection call in facade ([4c60ae2](https://github.com/goliatone/go-services/commit/4c60ae2daabc046580eb151d6eb6f8afb3275325))  - (goliatone)
- Go-command adapter ([327055a](https://github.com/goliatone/go-services/commit/327055a13abbb6f3d763d6be79f56bef82b277be))  - (goliatone)
- Token signing ([7dec7d9](https://github.com/goliatone/go-services/commit/7dec7d9ec3dab474b6a8da6dd7d97cbe3466f1fe))  - (goliatone)
- Dispatcher use explicit TTL ([89fce90](https://github.com/goliatone/go-services/commit/89fce90ca3cf90756cd6415a97f3f81a2995a625))  - (goliatone)
- Jwt signing ([1a16b40](https://github.com/goliatone/go-services/commit/1a16b40b7a4672c518e3e42b2dae29c034a0ff89))  - (goliatone)

## <!-- 13 -->📦 Bumps

- Bump version: v0.1.0 ([3bb2053](https://github.com/goliatone/go-services/commit/3bb20531552a8adee515d83228eb159ae7a8aa31))  - (goliatone)

## <!-- 16 -->➕ Add

- Credential freshness ([e1940cb](https://github.com/goliatone/go-services/commit/e1940cb861c2d2b16c0d1f193f404bc1622239f0))  - (goliatone)
- Cached rate limit state store ([a23f5d3](https://github.com/goliatone/go-services/commit/a23f5d321cb2d8567724014b7fdbc5e8b34242b3))  - (goliatone)
- Cache support for rate limit store ([d56fcd7](https://github.com/goliatone/go-services/commit/d56fcd75670c3d637f0b38624c2d91fc26b0242c))  - (goliatone)
- Rate limit state cached store ([049f0f1](https://github.com/goliatone/go-services/commit/049f0f138c37e0415328bd3493cedc9bd9fc2da1))  - (goliatone)
- Migrations for rate limit ([95ec0c1](https://github.com/goliatone/go-services/commit/95ec0c10c63eef5520d87bbbfe80e91c2e256aaa))  - (goliatone)
- Implement client creds token exchange ([99cfe98](https://github.com/goliatone/go-services/commit/99cfe98c7c0f7f36913ec515e99919b74167c2bc))  - (goliatone)
- Models for installations and rate limiting ([74aaadb](https://github.com/goliatone/go-services/commit/74aaadbb23049d7ba01afa138aec03f61988ba14))  - (goliatone)
- Default noop factory imp ([a4aa840](https://github.com/goliatone/go-services/commit/a4aa84083db2fa48a270b10792b18653308b9003))  - (goliatone)
- Secret provider support for v2 ([2cd8727](https://github.com/goliatone/go-services/commit/2cd8727d26ffde77acac5ac688accb42d1dd6231))  - (goliatone)
- New query handlers ([12bb4ff](https://github.com/goliatone/go-services/commit/12bb4ffdbd3182e43fea6274232e5db1a5365ad8))  - (goliatone)
- Manage installations ([cea9120](https://github.com/goliatone/go-services/commit/cea912023b942ecad93a3f7971d15affbf7db794))  - (goliatone)
- Transport resolvers and ratelimit policy ([694b623](https://github.com/goliatone/go-services/commit/694b623bdecc9216a62ca7c533960a768d487a75))  - (goliatone)
- Transport resolvers ([2f63938](https://github.com/goliatone/go-services/commit/2f63938fa71b197cb904e505bbffd21707e676ff))  - (goliatone)
- New errors ([1506d69](https://github.com/goliatone/go-services/commit/1506d69dacd1628092d682f8c0f353c885a65b70))  - (goliatone)
- Provider structs ([b4c0070](https://github.com/goliatone/go-services/commit/b4c0070e3354a8288e5d07cf1bdde7bf2e5290b2))  - (goliatone)
- New commands ([b7b1eec](https://github.com/goliatone/go-services/commit/b7b1eecc6daf76445fee80e39a79ed4f39522c55))  - (goliatone)
- Service installations ([f1da1fa](https://github.com/goliatone/go-services/commit/f1da1fad22472f161d87dea2042cd37b006c2c5c))  - (goliatone)
- Provider operation runtime ([fa58228](https://github.com/goliatone/go-services/commit/fa58228da9896c5333c793d0d0bacd68ad4175dd))  - (goliatone)
- Devkit providers ([d237110](https://github.com/goliatone/go-services/commit/d237110cb42b79ba5248e55f21f73d3d95aa530e))  - (goliatone)
- Store installation store ([9e1bdcb](https://github.com/goliatone/go-services/commit/9e1bdcb573e31a84a933532f9b24eefb261bbbcc))  - (goliatone)
- Rate limit state store ([85c862b](https://github.com/goliatone/go-services/commit/85c862bab09890eca9ff8a77de3a7fa5109ac0b4))  - (goliatone)
- Noop adapter transport ([b7dadcd](https://github.com/goliatone/go-services/commit/b7dadcda6d61703d9a6257348d327e0682459625))  - (goliatone)
- Extension hooks ([0e2ad9f](https://github.com/goliatone/go-services/commit/0e2ad9fc02b23bc94a35ca7add85ce40444d531f))  - (goliatone)
- Max response body bytes ([886d9c2](https://github.com/goliatone/go-services/commit/886d9c262d26f94faceb9232cfd85e20f14035c2))  - (goliatone)
- Resolve external account ID ([b72f1ce](https://github.com/goliatone/go-services/commit/b72f1ce24ee1a938206be0349ee8516bb9b8b7ca))  - (goliatone)
- Query compile checks ([05e2758](https://github.com/goliatone/go-services/commit/05e27580432423d71eebbd5fe9aa4e07ecc9d427))  - (goliatone)
- Command compile checks ([2b327ea](https://github.com/goliatone/go-services/commit/2b327ea5a65fd3a359af56880442d9a6584d24b7))  - (goliatone)
- Facade to expose comamnds and services ([aa5a408](https://github.com/goliatone/go-services/commit/aa5a4085183ce8ba54ec254e897978b561789663))  - (goliatone)
- Command implementation ([d60df2f](https://github.com/goliatone/go-services/commit/d60df2f607f8a64823fe0fe305a130cfad6e6769))  - (goliatone)
- Query implementation ([4861d54](https://github.com/goliatone/go-services/commit/4861d548aed31ca89955cbb99c4afe651c8c39d8))  - (goliatone)
- Auth to handle different service authentications ([79a5f29](https://github.com/goliatone/go-services/commit/79a5f2901179eed28acd1a3c306372bdda7a6247))  - (goliatone)
- Migrations for service credentail and grants ([7d6e80b](https://github.com/goliatone/go-services/commit/7d6e80ba220180e9144b62e69a4e187b59235c10))  - (goliatone)
- Provider baseline, production hardening ([35810e7](https://github.com/goliatone/go-services/commit/35810e76397d8276ad02687c3fc1990fbb83ad4c))  - (goliatone)
- Runtime interoperability, lifecyle projecttion ([dadbc32](https://github.com/goliatone/go-services/commit/dadbc32ca85033c8b26b11f642204089245da680))  - (goliatone)
- Subscriptions, webhooks, sync and inbound surface ([9486b49](https://github.com/goliatone/go-services/commit/9486b49a5eb165ec2ab5d804f05f7e851e247522))  - (goliatone)
- Oauth2, grants, and capability enforcement ([460c874](https://github.com/goliatone/go-services/commit/460c87423da53258ae558eab015e6db183662e5b))  - (goliatone)
- Core initial iteration ([3bfe320](https://github.com/goliatone/go-services/commit/3bfe32028fcd44659c60084112ff0ea7cae02474))  - (goliatone)
- Migrations ([267caf6](https://github.com/goliatone/go-services/commit/267caf6ced9a31b48e3f6def8410cc51dc6d7a53))  - (goliatone)
- Sql store ([7f0f806](https://github.com/goliatone/go-services/commit/7f0f8061e06ce93e4de22ad8bac9833a8ae17b45))  - (goliatone)
- Security secret provider ([88e4a7f](https://github.com/goliatone/go-services/commit/88e4a7f9be5c2fd167d1137496aca1c736e51d8a))  - (goliatone)
- Initial package structure ([10d8612](https://github.com/goliatone/go-services/commit/10d8612b9596c8ae2ce19ba7a8f31137132046a1))  - (goliatone)
- Installations package ([47a9486](https://github.com/goliatone/go-services/commit/47a948632a1e363eb54c51aca885961a67d57c97))  - (goliatone)
- Inbound package ([13847ec](https://github.com/goliatone/go-services/commit/13847ec1b1661b8e09c63a8106cc1c176fb54577))  - (goliatone)
- Auth package ([32c9728](https://github.com/goliatone/go-services/commit/32c9728e5cb6005c5387cdf65b58a5351604190e))  - (goliatone)
- Core services ([5535bf0](https://github.com/goliatone/go-services/commit/5535bf0c11d8c8bbffc5263e4220037f96f8904f))  - (goliatone)
- Adapters ([cd9af97](https://github.com/goliatone/go-services/commit/cd9af976990d7a2edaabb7f56d0c0485c52b7e26))  - (goliatone)

## <!-- 2 -->🚜 Refactor

- Rate limit store to be Repository compliant ([81e607c](https://github.com/goliatone/go-services/commit/81e607c584207baf680198f70b5f3bc026b5b195))  - (goliatone)
- Harden structural apis to handle auth ([067a290](https://github.com/goliatone/go-services/commit/067a290eeb2b522487d60d5911d40e842288dfa3))  - (goliatone)

## <!-- 30 -->📝 Other

- New commands ([0c9f085](https://github.com/goliatone/go-services/commit/0c9f08520ecf58461d72bfb2456e412e83b27869))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Udpate tests ([a4f17db](https://github.com/goliatone/go-services/commit/a4f17db1397d56451606c8d2c0dec31a27f48ac3))  - (goliatone)
- Update deps ([0fbcf13](https://github.com/goliatone/go-services/commit/0fbcf1386bf0efd5c4880fa00f34a2b0d9bca6d6))  - (goliatone)
- Update test ([ecb494a](https://github.com/goliatone/go-services/commit/ecb494a0a3c4783d30320e93725c02a76ccec16f))  - (goliatone)
- Update docs ([4a6a3b2](https://github.com/goliatone/go-services/commit/4a6a3b258a73d56b14344d2208c8fcfaccd5bdc5))  - (goliatone)
- Update tasks ([f392e29](https://github.com/goliatone/go-services/commit/f392e29375aff5028b765ee2e14edf40ce20d34a))  - (goliatone)
- Initial commit ([654b36d](https://github.com/goliatone/go-services/commit/654b36d1f22be1ea4b973f9e2f55b0b63e77faa9))  - (goliatone)
