# Identity Profile Resolution

This package now includes a generic identity helper in `identity` that normalizes user profile claims from:

1. `id_token` claims (when available)
2. OIDC `userinfo` endpoints
3. Provider-specific fallback endpoints (for built-ins, e.g. GitHub)

## API

Use `identity.ProfileResolver` implementations via `identity.NewResolver(...)` or `identity.DefaultResolver()`.

```go
resolver := identity.DefaultResolver()
profile, err := resolver.Resolve(ctx, providerID, credential, metadata)
```

For strict ID token verification, configure `identity.Config.IDTokenVerifier` when constructing the resolver.
If no verifier is configured, ID token claims are parsed without cryptographic verification.

`profile` includes normalized fields:
- `Issuer`, `Subject`
- `Email`, `EmailVerified`
- `Name`, `GivenName`, `FamilyName`
- `PictureURL`, `Locale`

Use `profile.ExternalAccountID()` for a stable account key (`issuer|subject` when issuer exists).

## OAuth2 Provider Integration

`providers.OAuth2Config` now supports:

```go
ProfileResolver identity.ProfileResolver
```

When `external_account_id` is not present in callback metadata, `OAuth2Provider.CompleteAuth(...)` will attempt to resolve identity through the configured `ProfileResolver` and derive `ExternalAccountID` automatically.

## Google Provider Defaults and Opt-Out

Built-in Google providers now include identity scopes by default:
- `openid`
- `profile`
- `email`

You can opt out per provider config:

```go
docs.New(docs.Config{
    ClientID: "client-id",
    ClientSecret: "client-secret",
    DisableIdentityScopes: true,
})
```

The `DisableIdentityScopes` option is available in:
- `providers/google/docs.Config`
- `providers/google/drive.Config`
- `providers/google/gmail.Config`
- `providers/google/calendar.Config`
