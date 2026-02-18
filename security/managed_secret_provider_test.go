package security

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeKMSClient struct {
	failEncrypt bool
	failDecrypt bool
}

func (c *fakeKMSClient) Encrypt(_ context.Context, req KMSEncryptRequest) (KMSEncryptResponse, error) {
	if c.failEncrypt {
		return KMSEncryptResponse{}, fmt.Errorf("kms unavailable")
	}
	if len(req.Plaintext) == 0 {
		return KMSEncryptResponse{}, fmt.Errorf("plaintext is required")
	}
	encoded := base64.StdEncoding.EncodeToString(req.Plaintext)
	wire := fmt.Sprintf("kms|%s|%d|%s", req.KeyID, req.KeyVersion, encoded)
	return KMSEncryptResponse{Ciphertext: []byte(wire)}, nil
}

func (c *fakeKMSClient) Decrypt(_ context.Context, req KMSDecryptRequest) (KMSDecryptResponse, error) {
	if c.failDecrypt {
		return KMSDecryptResponse{}, fmt.Errorf("kms unavailable")
	}
	parts := strings.Split(string(req.Ciphertext), "|")
	if len(parts) != 4 || parts[0] != "kms" {
		return KMSDecryptResponse{}, fmt.Errorf("invalid kms payload")
	}
	if parts[1] != req.KeyID {
		return KMSDecryptResponse{}, fmt.Errorf("kms key mismatch")
	}
	if fmt.Sprintf("%d", req.KeyVersion) != parts[2] {
		return KMSDecryptResponse{}, fmt.Errorf("kms version mismatch")
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return KMSDecryptResponse{}, err
	}
	return KMSDecryptResponse{Plaintext: decoded}, nil
}

type fakeVaultClient struct {
	failEncrypt bool
	failDecrypt bool
}

func (c *fakeVaultClient) Encrypt(_ context.Context, req VaultEncryptRequest) (VaultEncryptResponse, error) {
	if c.failEncrypt {
		return VaultEncryptResponse{}, fmt.Errorf("vault unavailable")
	}
	encoded := base64.StdEncoding.EncodeToString(req.Plaintext)
	wire := fmt.Sprintf("vault|%s|%d|%s", req.KeyPath, req.KeyVersion, encoded)
	return VaultEncryptResponse{Ciphertext: []byte(wire)}, nil
}

func (c *fakeVaultClient) Decrypt(_ context.Context, req VaultDecryptRequest) (VaultDecryptResponse, error) {
	if c.failDecrypt {
		return VaultDecryptResponse{}, fmt.Errorf("vault unavailable")
	}
	parts := strings.Split(string(req.Ciphertext), "|")
	if len(parts) != 4 || parts[0] != "vault" {
		return VaultDecryptResponse{}, fmt.Errorf("invalid vault payload")
	}
	if parts[1] != req.KeyPath {
		return VaultDecryptResponse{}, fmt.Errorf("vault path mismatch")
	}
	if fmt.Sprintf("%d", req.KeyVersion) != parts[2] {
		return VaultDecryptResponse{}, fmt.Errorf("vault version mismatch")
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return VaultDecryptResponse{}, err
	}
	return VaultDecryptResponse{Plaintext: decoded}, nil
}

func TestKMSSecretProvider_EncryptDecryptRoundTrip(t *testing.T) {
	provider, err := NewKMSSecretProvider(&fakeKMSClient{}, "kms-services", 3, WithKMSMetadata(map[string]string{"env": "test"}))
	if err != nil {
		t.Fatalf("new kms provider: %v", err)
	}
	plaintext := []byte("kms-secret-token")
	ciphertext, err := provider.Encrypt(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	metadata, err := ParseEnvelopeMetadata(ciphertext, false)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if metadata.Algorithm != envelopeAlgorithmKMS || metadata.KeyID != "kms-services" || metadata.Version != 3 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	decrypted, err := provider.Decrypt(context.Background(), ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("expected roundtrip plaintext")
	}
}

func TestVaultSecretProvider_EncryptDecryptRoundTrip(t *testing.T) {
	provider, err := NewVaultSecretProvider(&fakeVaultClient{}, "transit/services", 2)
	if err != nil {
		t.Fatalf("new vault provider: %v", err)
	}
	plaintext := []byte("vault-secret-token")
	ciphertext, err := provider.Encrypt(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	metadata, err := ParseEnvelopeMetadata(ciphertext, false)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if metadata.Algorithm != envelopeAlgorithmVault || metadata.KeyID != "transit/services" || metadata.Version != 2 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	decrypted, err := provider.Decrypt(context.Background(), ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("expected roundtrip plaintext")
	}
}

func TestKMSSecretProvider_RotationWindowAndLegacyDecryptCompatibility(t *testing.T) {
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	client := &fakeKMSClient{}
	legacyProvider, err := NewKMSSecretProvider(client, "kms-services", 1)
	if err != nil {
		t.Fatalf("new legacy provider: %v", err)
	}
	legacyCiphertext, err := legacyProvider.Encrypt(context.Background(), []byte("legacy-secret"))
	if err != nil {
		t.Fatalf("legacy encrypt: %v", err)
	}

	activeWindow := KeyRotationWindow{NotBefore: now.Add(-2 * time.Hour), NotAfter: now.Add(2 * time.Hour)}
	legacyWindow := KeyRotationWindow{NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(24 * time.Hour)}
	rotatedProvider, err := NewKMSSecretProvider(
		client,
		"kms-services",
		2,
		WithKMSClock(func() time.Time { return now }),
		WithKMSDecryptCompatibilityKey("kms-services", 1),
		WithKMSRotationWindow("kms-services", 2, activeWindow),
		WithKMSRotationWindow("kms-services", 1, legacyWindow),
	)
	if err != nil {
		t.Fatalf("new rotated provider: %v", err)
	}
	decrypted, err := rotatedProvider.Decrypt(context.Background(), legacyCiphertext)
	if err != nil {
		t.Fatalf("decrypt legacy ciphertext: %v", err)
	}
	if string(decrypted) != "legacy-secret" {
		t.Fatalf("expected legacy decrypt compatibility")
	}

	closedProvider, err := NewKMSSecretProvider(
		client,
		"kms-services",
		2,
		WithKMSClock(func() time.Time { return now }),
		WithKMSDecryptCompatibilityKey("kms-services", 1),
		WithKMSRotationWindow("kms-services", 1, KeyRotationWindow{NotAfter: now.Add(-time.Minute)}),
	)
	if err != nil {
		t.Fatalf("new closed provider: %v", err)
	}
	if _, err := closedProvider.Decrypt(context.Background(), legacyCiphertext); err == nil {
		t.Fatalf("expected decrypt to fail when compatibility window has closed")
	}
}

func TestFailoverSecretProvider_StrictPolicyRejectsFallback(t *testing.T) {
	fallback, err := NewAppKeySecretProviderFromString("fallback-key", WithKeyID("fallback"), WithVersion(1))
	if err != nil {
		t.Fatalf("new fallback app-key provider: %v", err)
	}
	provider, err := NewFailoverSecretProvider(
		&KMSSecretProvider{
			client: &fakeKMSClient{failEncrypt: true},
			active: kmsKeyRef{KeyID: "kms-services", Version: 2},
			now:    func() time.Time { return time.Now().UTC() },
		},
		WithFallbackSecretProvider(fallback),
		WithSecretProviderFailurePolicy(SecretProviderFailurePolicyStrict),
	)
	if err != nil {
		t.Fatalf("new failover provider: %v", err)
	}
	if _, err := provider.Encrypt(context.Background(), []byte("secret")); err == nil {
		t.Fatalf("expected strict policy to fail without fallback execution")
	}
}

func TestFailoverSecretProvider_FallbackPolicyAndDiagnostics(t *testing.T) {
	fallback, err := NewAppKeySecretProviderFromString("fallback-key", WithKeyID("fallback"), WithVersion(7))
	if err != nil {
		t.Fatalf("new fallback app-key provider: %v", err)
	}
	primary, err := NewKMSSecretProvider(&fakeKMSClient{failEncrypt: true, failDecrypt: true}, "kms-services", 2)
	if err != nil {
		t.Fatalf("new kms provider: %v", err)
	}
	events := []SecretProviderDiagnostic{}
	provider, err := NewFailoverSecretProvider(
		primary,
		WithFallbackSecretProvider(fallback),
		WithSecretProviderFailurePolicy(SecretProviderFailurePolicyFallback),
		WithSecretProviderDiagnostics(func(event SecretProviderDiagnostic) {
			events = append(events, event)
		}),
	)
	if err != nil {
		t.Fatalf("new failover provider: %v", err)
	}

	ciphertext, err := provider.Encrypt(context.Background(), []byte("payload"))
	if err != nil {
		t.Fatalf("encrypt with fallback policy: %v", err)
	}
	if _, version := provider.Metadata(); version != 7 {
		t.Fatalf("expected metadata to reflect fallback key after fallback encrypt")
	}
	decrypted, err := provider.Decrypt(context.Background(), ciphertext)
	if err != nil {
		t.Fatalf("decrypt with fallback policy: %v", err)
	}
	if string(decrypted) != "payload" {
		t.Fatalf("expected fallback decrypt payload")
	}
	if len(events) < 2 {
		t.Fatalf("expected diagnostic events for fallback flow")
	}
}

func TestFailoverSecretProvider_Migration_AppKeyToKMS(t *testing.T) {
	legacy, err := NewAppKeySecretProviderFromString("legacy-key", WithKeyID("app-v1"), WithVersion(1))
	if err != nil {
		t.Fatalf("new legacy provider: %v", err)
	}
	kmsProvider, err := NewKMSSecretProvider(&fakeKMSClient{}, "kms-services", 5)
	if err != nil {
		t.Fatalf("new kms provider: %v", err)
	}
	provider, err := NewFailoverSecretProvider(
		kmsProvider,
		WithFallbackSecretProvider(legacy),
		WithSecretProviderFailurePolicy(SecretProviderFailurePolicyFallback),
	)
	if err != nil {
		t.Fatalf("new migration provider: %v", err)
	}

	legacyCiphertext, err := legacy.Encrypt(context.Background(), []byte("legacy-token"))
	if err != nil {
		t.Fatalf("legacy encrypt: %v", err)
	}
	legacyDecrypted, err := provider.Decrypt(context.Background(), legacyCiphertext)
	if err != nil {
		t.Fatalf("migration decrypt legacy payload: %v", err)
	}
	if string(legacyDecrypted) != "legacy-token" {
		t.Fatalf("expected migration decrypt to recover legacy payload")
	}

	newCiphertext, err := provider.Encrypt(context.Background(), []byte("new-token"))
	if err != nil {
		t.Fatalf("migration encrypt new payload: %v", err)
	}
	metadata, err := ParseEnvelopeMetadata(newCiphertext, false)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if metadata.Algorithm != envelopeAlgorithmKMS {
		t.Fatalf("expected new encryptions to use kms algorithm")
	}
}

func TestFailoverSecretProvider_Migration_AppKeyToVault(t *testing.T) {
	legacy, err := NewAppKeySecretProviderFromString("legacy-key", WithKeyID("app-v1"), WithVersion(1))
	if err != nil {
		t.Fatalf("new legacy provider: %v", err)
	}
	vaultProvider, err := NewVaultSecretProvider(&fakeVaultClient{}, "transit/services", 9)
	if err != nil {
		t.Fatalf("new vault provider: %v", err)
	}
	provider, err := NewFailoverSecretProvider(
		vaultProvider,
		WithFallbackSecretProvider(legacy),
		WithSecretProviderFailurePolicy(SecretProviderFailurePolicyFallback),
	)
	if err != nil {
		t.Fatalf("new migration provider: %v", err)
	}
	legacyCiphertext, err := legacy.Encrypt(context.Background(), []byte("legacy-token"))
	if err != nil {
		t.Fatalf("legacy encrypt: %v", err)
	}
	if _, err := provider.Decrypt(context.Background(), legacyCiphertext); err != nil {
		t.Fatalf("vault migration decrypt legacy payload: %v", err)
	}
	newCiphertext, err := provider.Encrypt(context.Background(), []byte("new-token"))
	if err != nil {
		t.Fatalf("vault migration encrypt new payload: %v", err)
	}
	metadata, err := ParseEnvelopeMetadata(newCiphertext, false)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if metadata.Algorithm != envelopeAlgorithmVault {
		t.Fatalf("expected new encryptions to use vault algorithm")
	}
}
