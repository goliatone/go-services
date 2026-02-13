package core

import (
	"context"
	"testing"
	"time"
)

func TestJSONCredentialCodec_RoundTrip(t *testing.T) {
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	rotates := now.Add(30 * time.Minute)

	codec := JSONCredentialCodec{}
	encoded, err := codec.Encode(ActiveCredential{
		ConnectionID:    "conn_1",
		TokenType:       "Bearer",
		AccessToken:     "access-1",
		RefreshToken:    "refresh-1",
		RequestedScopes: []string{"repo:read", "repo:write"},
		GrantedScopes:   []string{"repo:read"},
		ExpiresAt:       &expires,
		Refreshable:     true,
		RotatesAt:       &rotates,
		Metadata: map[string]any{
			"source": "test",
		},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.AccessToken != "access-1" {
		t.Fatalf("expected access token roundtrip")
	}
	if decoded.RefreshToken != "refresh-1" {
		t.Fatalf("expected refresh token roundtrip")
	}
	if decoded.ExpiresAt == nil || !decoded.ExpiresAt.UTC().Equal(expires.UTC()) {
		t.Fatalf("expected expires_at roundtrip")
	}
	if decoded.RotatesAt == nil || !decoded.RotatesAt.UTC().Equal(rotates.UTC()) {
		t.Fatalf("expected rotates_at roundtrip")
	}
}

func TestServiceCredentialToActive_UsesExplicitCodecFormat(t *testing.T) {
	ctx := context.Background()
	secret := testSecretProvider{}

	codec := JSONCredentialCodec{}
	plaintext, err := codec.Encode(ActiveCredential{
		ConnectionID: "conn_1",
		TokenType:    "Bearer",
		AccessToken:  "access-json",
		RefreshToken: "refresh-json",
		Refreshable:  true,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	ciphertext, err := secret.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	svc, err := NewService(Config{},
		WithSecretProvider(secret),
		WithCredentialCodec(codec),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	active, err := svc.credentialToActive(ctx, Credential{
		ConnectionID:     "conn_1",
		EncryptedPayload: ciphertext,
		PayloadFormat:    codec.Format(),
		PayloadVersion:   codec.Version(),
	})
	if err != nil {
		t.Fatalf("credentialToActive: %v", err)
	}
	if active.AccessToken != "access-json" {
		t.Fatalf("expected access token from json codec")
	}
	if active.RefreshToken != "refresh-json" {
		t.Fatalf("expected refresh token from json codec")
	}
}

func TestServiceCredentialToActive_RejectsUnknownCodec(t *testing.T) {
	ctx := context.Background()
	secret := testSecretProvider{}
	ciphertext, err := secret.Encrypt(ctx, []byte("payload"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	svc, err := NewService(Config{}, WithSecretProvider(secret))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.credentialToActive(ctx, Credential{
		ConnectionID:     "conn_1",
		EncryptedPayload: ciphertext,
		PayloadFormat:    "unknown",
		PayloadVersion:   1,
	})
	if err == nil {
		t.Fatalf("expected unsupported codec error")
	}
}
