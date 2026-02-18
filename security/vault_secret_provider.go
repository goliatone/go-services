package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

type VaultEncryptRequest struct {
	KeyPath    string
	KeyVersion int
	Plaintext  []byte
	Metadata   map[string]string
}

type VaultEncryptResponse struct {
	Ciphertext []byte
}

type VaultDecryptRequest struct {
	KeyPath    string
	KeyVersion int
	Ciphertext []byte
	Metadata   map[string]string
}

type VaultDecryptResponse struct {
	Plaintext []byte
}

type VaultClient interface {
	Encrypt(ctx context.Context, req VaultEncryptRequest) (VaultEncryptResponse, error)
	Decrypt(ctx context.Context, req VaultDecryptRequest) (VaultDecryptResponse, error)
}

type VaultOption func(*VaultSecretProvider)

type vaultKeyRef struct {
	Path    string
	Version int
}

func (r vaultKeyRef) id() string {
	return fmt.Sprintf("%s:%d", r.Path, r.Version)
}

type VaultSecretProvider struct {
	client          VaultClient
	active          vaultKeyRef
	decryptAllowed  map[string]vaultKeyRef
	rotationWindows map[string]KeyRotationWindow
	allowAnyDecrypt bool
	metadata        map[string]string
	now             func() time.Time
}

func NewVaultSecretProvider(client VaultClient, keyPath string, version int, opts ...VaultOption) (*VaultSecretProvider, error) {
	ref, err := newVaultKeyRef(keyPath, version)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("security: vault client is required")
	}
	provider := &VaultSecretProvider{
		client:          client,
		active:          ref,
		decryptAllowed:  map[string]vaultKeyRef{ref.id(): ref},
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

func WithVaultDecryptCompatibilityKey(keyPath string, version int) VaultOption {
	return func(provider *VaultSecretProvider) {
		if provider == nil {
			return
		}
		ref, err := newVaultKeyRef(keyPath, version)
		if err != nil {
			return
		}
		provider.decryptAllowed[ref.id()] = ref
	}
}

func WithVaultRotationWindow(keyPath string, version int, window KeyRotationWindow) VaultOption {
	return func(provider *VaultSecretProvider) {
		if provider == nil {
			return
		}
		ref, err := newVaultKeyRef(keyPath, version)
		if err != nil {
			return
		}
		provider.rotationWindows[ref.id()] = window
	}
}

func WithVaultAllowAnyDecryptKey(allow bool) VaultOption {
	return func(provider *VaultSecretProvider) {
		if provider == nil {
			return
		}
		provider.allowAnyDecrypt = allow
	}
}

func WithVaultMetadata(metadata map[string]string) VaultOption {
	return func(provider *VaultSecretProvider) {
		if provider == nil {
			return
		}
		provider.metadata = copyStringMap(metadata)
	}
}

func WithVaultClock(now func() time.Time) VaultOption {
	return func(provider *VaultSecretProvider) {
		if provider == nil {
			return
		}
		provider.now = now
	}
}

func (p *VaultSecretProvider) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("security: plaintext is required")
	}
	if !p.rotationWindowAllows(p.active) {
		return nil, fmt.Errorf("security: vault key %q version %d is outside the configured rotation window", p.active.Path, p.active.Version)
	}

	response, err := p.client.Encrypt(ctx, VaultEncryptRequest{
		KeyPath:    p.active.Path,
		KeyVersion: p.active.Version,
		Plaintext:  append([]byte(nil), plaintext...),
		Metadata:   copyStringMap(p.metadata),
	})
	if err != nil {
		return nil, fmt.Errorf("security: vault encrypt: %w", err)
	}
	if len(response.Ciphertext) == 0 {
		return nil, fmt.Errorf("security: vault encrypt returned empty ciphertext")
	}
	return encodeEnvelope(envelope{
		KeyID:      p.active.Path,
		Version:    p.active.Version,
		Algorithm:  envelopeAlgorithmVault,
		Ciphertext: encodeCiphertextPayload(response.Ciphertext),
		Metadata:   copyStringMap(p.metadata),
	})
}

func (p *VaultSecretProvider) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	env, _, err := decodeEnvelope(ciphertext, envelopeDecodeOptions{DefaultAlgorithm: envelopeAlgorithmVault})
	if err != nil {
		return nil, err
	}
	if env.Algorithm != envelopeAlgorithmVault {
		return nil, fmt.Errorf("security: unsupported envelope algorithm %q", env.Algorithm)
	}
	ref, err := newVaultKeyRef(env.KeyID, env.Version)
	if err != nil {
		return nil, err
	}
	if !p.allowAnyDecrypt {
		if _, ok := p.decryptAllowed[ref.id()]; !ok {
			return nil, fmt.Errorf("security: vault decrypt key %q version %d is not configured", ref.Path, ref.Version)
		}
	}
	if !p.rotationWindowAllows(ref) {
		return nil, fmt.Errorf("security: vault key %q version %d is outside the configured rotation window", ref.Path, ref.Version)
	}

	payload, err := decodeCiphertextPayload(env.Ciphertext)
	if err != nil {
		return nil, err
	}
	response, err := p.client.Decrypt(ctx, VaultDecryptRequest{
		KeyPath:    ref.Path,
		KeyVersion: ref.Version,
		Ciphertext: payload,
		Metadata:   copyStringMap(env.Metadata),
	})
	if err != nil {
		return nil, fmt.Errorf("security: vault decrypt: %w", err)
	}
	if len(response.Plaintext) == 0 {
		return nil, fmt.Errorf("security: vault decrypt returned empty plaintext")
	}
	return response.Plaintext, nil
}

func (p *VaultSecretProvider) KeyID() string {
	if p == nil {
		return ""
	}
	return p.active.Path
}

func (p *VaultSecretProvider) Version() int {
	if p == nil {
		return 0
	}
	return p.active.Version
}

func (p *VaultSecretProvider) Metadata() (string, int) {
	return p.KeyID(), p.Version()
}

func (p *VaultSecretProvider) rotationWindowAllows(ref vaultKeyRef) bool {
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

func newVaultKeyRef(keyPath string, version int) (vaultKeyRef, error) {
	trimmed := strings.TrimSpace(keyPath)
	if trimmed == "" {
		return vaultKeyRef{}, fmt.Errorf("security: key path is required")
	}
	if version <= 0 {
		return vaultKeyRef{}, fmt.Errorf("security: key version must be greater than zero")
	}
	return vaultKeyRef{Path: trimmed, Version: version}, nil
}

var _ core.SecretProvider = (*VaultSecretProvider)(nil)
