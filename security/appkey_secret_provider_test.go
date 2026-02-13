package security

import (
	"bytes"
	"context"
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
