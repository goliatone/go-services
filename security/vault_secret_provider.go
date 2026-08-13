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
	return encryptManagedEnvelope(
		plaintext,
		"vault",
		p.active.Path,
		p.active.Version,
		envelopeAlgorithmVault,
		p.metadata,
		p.rotationWindowAllows(p.active),
		func(payload []byte) ([]byte, error) {
			response, err := p.client.Encrypt(ctx, VaultEncryptRequest{
				KeyPath: p.active.Path, KeyVersion: p.active.Version, Plaintext: payload, Metadata: copyStringMap(p.metadata),
			})
			return response.Ciphertext, err
		},
	)
}

//nolint:dupl // Shared policy is in decryptManagedEnvelope; typed Vault request/error handling cannot share the KMS client API. TestVaultSecretProvider_EncryptDecryptRoundTrip covers this adapter.
func (p *VaultSecretProvider) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	return decryptManagedEnvelope(
		ciphertext,
		"vault",
		envelopeAlgorithmVault,
		p.allowAnyDecrypt,
		func(env envelope) (managedEnvelopeKey, error) {
			ref, err := newVaultKeyRef(env.KeyID, env.Version)
			if err != nil {
				return managedEnvelopeKey{}, err
			}
			_, configured := p.decryptAllowed[ref.id()]
			return managedEnvelopeKey{
				keyID: ref.Path, version: ref.Version, configured: configured, rotationAllowed: p.rotationWindowAllows(ref),
			}, nil
		},
		func(key managedEnvelopeKey, payload []byte, metadata map[string]string) ([]byte, error) {
			response, err := p.client.Decrypt(ctx, VaultDecryptRequest{
				KeyPath: key.keyID, KeyVersion: key.version, Ciphertext: payload, Metadata: metadata,
			})
			return response.Plaintext, err
		},
	)
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
