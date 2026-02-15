package security

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAppKeySecretProvider_EncryptDecryptRoundTrip(t *testing.T) {
	provider, err := NewAppKeySecretProviderFromString("super-secret-test-key", WithKeyID("services-v1"), WithVersion(3))
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	plaintext := []byte("token-value-123")
	encrypted, err := provider.Encrypt(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Equal(encrypted, plaintext) {
		t.Fatalf("expected encrypted payload to differ from plaintext")
	}
	if !bytes.HasPrefix(encrypted, []byte(envelopePrefix)) {
		t.Fatalf("expected envelope prefix")
	}

	decrypted, err := provider.Decrypt(context.Background(), encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("expected roundtrip plaintext; got %q", string(decrypted))
	}
}

func TestAppKeySecretProvider_RejectsMetadataMismatch(t *testing.T) {
	issuer, err := NewAppKeySecretProviderFromString("super-secret-test-key", WithKeyID("services-v1"), WithVersion(1))
	if err != nil {
		t.Fatalf("new issuer provider: %v", err)
	}
	receiver, err := NewAppKeySecretProviderFromString("super-secret-test-key", WithKeyID("services-v2"), WithVersion(2))
	if err != nil {
		t.Fatalf("new receiver provider: %v", err)
	}

	encrypted, err := issuer.Encrypt(context.Background(), []byte("payload"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := receiver.Decrypt(context.Background(), encrypted); err == nil {
		t.Fatalf("expected metadata mismatch error")
	}
}

func TestAppKeySecretProvider_RejectsCiphertextWithoutEnvelopePrefixByDefault(t *testing.T) {
	provider, err := NewAppKeySecretProviderFromString("super-secret-test-key")
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.Decrypt(context.Background(), []byte(`{"nonce":"x","ciphertext":"y"}`)); err == nil {
		t.Fatalf("expected strict envelope prefix validation")
	}
}

func TestAppKeySecretProvider_LegacyDecryptOptIn(t *testing.T) {
	issuer, err := NewAppKeySecretProviderFromString("super-secret-test-key", WithKeyID("services-v1"), WithVersion(1))
	if err != nil {
		t.Fatalf("new issuer provider: %v", err)
	}
	receiver, err := NewAppKeySecretProviderFromString(
		"super-secret-test-key",
		WithKeyID("services-v1"),
		WithVersion(1),
		WithAllowLegacyDecrypt(true),
	)
	if err != nil {
		t.Fatalf("new receiver provider: %v", err)
	}

	encrypted, err := issuer.Encrypt(context.Background(), []byte("payload"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	legacyEncrypted := []byte(strings.TrimPrefix(string(encrypted), envelopePrefix))
	if _, err := receiver.Decrypt(context.Background(), legacyEncrypted); err != nil {
		t.Fatalf("expected legacy decrypt override to allow prefix-less envelope: %v", err)
	}
}

func TestAppKeySecretProvider_RejectsEnvelopeAlgorithmTampering(t *testing.T) {
	provider, err := NewAppKeySecretProviderFromString("super-secret-test-key", WithKeyID("services-v1"), WithVersion(1))
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	encrypted, err := provider.Encrypt(context.Background(), []byte("payload"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	payload := strings.TrimPrefix(string(encrypted), envelopePrefix)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		t.Fatalf("decode envelope json: %v", err)
	}
	parsed["alg"] = "none"
	tamperedRaw, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("encode tampered envelope: %v", err)
	}
	tampered := append([]byte(envelopePrefix), tamperedRaw...)
	if _, err := provider.Decrypt(context.Background(), tampered); err == nil {
		t.Fatalf("expected tampered algorithm to be rejected")
	}
}
