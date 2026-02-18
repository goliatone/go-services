package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

type KMSEncryptRequest struct {
	KeyID      string
	KeyVersion int
	Plaintext  []byte
	Metadata   map[string]string
}

type KMSEncryptResponse struct {
	Ciphertext []byte
}

type KMSDecryptRequest struct {
	KeyID      string
	KeyVersion int
	Ciphertext []byte
	Metadata   map[string]string
}

type KMSDecryptResponse struct {
	Plaintext []byte
}

type KMSClient interface {
	Encrypt(ctx context.Context, req KMSEncryptRequest) (KMSEncryptResponse, error)
	Decrypt(ctx context.Context, req KMSDecryptRequest) (KMSDecryptResponse, error)
}

type KMSOption func(*KMSSecretProvider)

type kmsKeyRef struct {
	KeyID   string
	Version int
}

func (r kmsKeyRef) id() string {
	return fmt.Sprintf("%s:%d", r.KeyID, r.Version)
}

type KMSSecretProvider struct {
	client          KMSClient
	active          kmsKeyRef
	decryptAllowed  map[string]kmsKeyRef
	rotationWindows map[string]KeyRotationWindow
	allowAnyDecrypt bool
	metadata        map[string]string
	now             func() time.Time
}

func NewKMSSecretProvider(client KMSClient, keyID string, version int, opts ...KMSOption) (*KMSSecretProvider, error) {
	ref, err := newKMSKeyRef(keyID, version)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("security: kms client is required")
	}
	provider := &KMSSecretProvider{
		client:          client,
		active:          ref,
		decryptAllowed:  map[string]kmsKeyRef{ref.id(): ref},
		rotationWindows: map[string]KeyRotationWindow{},
		metadata:        map[string]string{},
		now:             func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(provider)
	}
	if provider.now == nil {
		provider.now = func() time.Time { return time.Now().UTC() }
	}
	return provider, nil
}

func WithKMSDecryptCompatibilityKey(keyID string, version int) KMSOption {
	return func(provider *KMSSecretProvider) {
		if provider == nil {
			return
		}
		ref, err := newKMSKeyRef(keyID, version)
		if err != nil {
			return
		}
		provider.decryptAllowed[ref.id()] = ref
	}
}

func WithKMSRotationWindow(keyID string, version int, window KeyRotationWindow) KMSOption {
	return func(provider *KMSSecretProvider) {
		if provider == nil {
			return
		}
		ref, err := newKMSKeyRef(keyID, version)
		if err != nil {
			return
		}
		provider.rotationWindows[ref.id()] = window
	}
}

func WithKMSAllowAnyDecryptKey(allow bool) KMSOption {
	return func(provider *KMSSecretProvider) {
		if provider == nil {
			return
		}
		provider.allowAnyDecrypt = allow
	}
}

func WithKMSMetadata(metadata map[string]string) KMSOption {
	return func(provider *KMSSecretProvider) {
		if provider == nil {
			return
		}
		provider.metadata = copyStringMap(metadata)
	}
}

func WithKMSClock(now func() time.Time) KMSOption {
	return func(provider *KMSSecretProvider) {
		if provider == nil {
			return
		}
		provider.now = now
	}
}

func (p *KMSSecretProvider) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("security: plaintext is required")
	}
	if !p.rotationWindowAllows(p.active) {
		return nil, fmt.Errorf("security: kms key %q version %d is outside the configured rotation window", p.active.KeyID, p.active.Version)
	}

	response, err := p.client.Encrypt(ctx, KMSEncryptRequest{
		KeyID:      p.active.KeyID,
		KeyVersion: p.active.Version,
		Plaintext:  append([]byte(nil), plaintext...),
		Metadata:   copyStringMap(p.metadata),
	})
	if err != nil {
		return nil, fmt.Errorf("security: kms encrypt: %w", err)
	}
	if len(response.Ciphertext) == 0 {
		return nil, fmt.Errorf("security: kms encrypt returned empty ciphertext")
	}
	return encodeEnvelope(envelope{
		KeyID:      p.active.KeyID,
		Version:    p.active.Version,
		Algorithm:  envelopeAlgorithmKMS,
		Ciphertext: encodeCiphertextPayload(response.Ciphertext),
		Metadata:   copyStringMap(p.metadata),
	})
}

func (p *KMSSecretProvider) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	env, _, err := decodeEnvelope(ciphertext, envelopeDecodeOptions{DefaultAlgorithm: envelopeAlgorithmKMS})
	if err != nil {
		return nil, err
	}
	if env.Algorithm != envelopeAlgorithmKMS {
		return nil, fmt.Errorf("security: unsupported envelope algorithm %q", env.Algorithm)
	}
	ref, err := newKMSKeyRef(env.KeyID, env.Version)
	if err != nil {
		return nil, err
	}
	if !p.allowAnyDecrypt {
		if _, ok := p.decryptAllowed[ref.id()]; !ok {
			return nil, fmt.Errorf("security: kms decrypt key %q version %d is not configured", ref.KeyID, ref.Version)
		}
	}
	if !p.rotationWindowAllows(ref) {
		return nil, fmt.Errorf("security: kms key %q version %d is outside the configured rotation window", ref.KeyID, ref.Version)
	}

	payload, err := decodeCiphertextPayload(env.Ciphertext)
	if err != nil {
		return nil, err
	}
	response, err := p.client.Decrypt(ctx, KMSDecryptRequest{
		KeyID:      ref.KeyID,
		KeyVersion: ref.Version,
		Ciphertext: payload,
		Metadata:   copyStringMap(env.Metadata),
	})
	if err != nil {
		return nil, fmt.Errorf("security: kms decrypt: %w", err)
	}
	if len(response.Plaintext) == 0 {
		return nil, fmt.Errorf("security: kms decrypt returned empty plaintext")
	}
	return response.Plaintext, nil
}

func (p *KMSSecretProvider) KeyID() string {
	if p == nil {
		return ""
	}
	return p.active.KeyID
}

func (p *KMSSecretProvider) Version() int {
	if p == nil {
		return 0
	}
	return p.active.Version
}

func (p *KMSSecretProvider) Metadata() (string, int) {
	return p.KeyID(), p.Version()
}

func (p *KMSSecretProvider) rotationWindowAllows(ref kmsKeyRef) bool {
	if p == nil {
		return false
	}
	window, ok := p.rotationWindows[ref.id()]
	if !ok {
		return true
	}
	now := p.now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return window.Allows(now())
}

func newKMSKeyRef(keyID string, version int) (kmsKeyRef, error) {
	trimmed := strings.TrimSpace(keyID)
	if trimmed == "" {
		return kmsKeyRef{}, fmt.Errorf("security: key id is required")
	}
	if version <= 0 {
		return kmsKeyRef{}, fmt.Errorf("security: key version must be greater than zero")
	}
	return kmsKeyRef{KeyID: trimmed, Version: version}, nil
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		output[trimmedKey] = strings.TrimSpace(value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

var _ core.SecretProvider = (*KMSSecretProvider)(nil)
