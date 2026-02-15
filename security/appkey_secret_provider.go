package security

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/goliatone/go-services/core"
)

const (
	envelopePrefix    = "services.secret.v1:"
	envelopeAlgorithm = "aes-256-gcm"
)

type Option func(*AppKeySecretProvider)

type AppKeySecretProvider struct {
	key                []byte
	keyID              string
	version            int
	allowLegacyDecrypt bool
}

type envelope struct {
	KeyID      string `json:"kid"`
	Version    int    `json:"ver"`
	Algorithm  string `json:"alg"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func WithKeyID(id string) Option {
	return func(provider *AppKeySecretProvider) {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			provider.keyID = trimmed
		}
	}
}

func WithVersion(version int) Option {
	return func(provider *AppKeySecretProvider) {
		if version > 0 {
			provider.version = version
		}
	}
}

func WithAllowLegacyDecrypt(allow bool) Option {
	return func(provider *AppKeySecretProvider) {
		provider.allowLegacyDecrypt = allow
	}
}

func NewAppKeySecretProvider(keyMaterial []byte, opts ...Option) (*AppKeySecretProvider, error) {
	key := bytes.TrimSpace(keyMaterial)
	if len(key) == 0 {
		return nil, fmt.Errorf("security: key material is required")
	}
	normalized := normalizeKey(key)
	provider := &AppKeySecretProvider{
		key:                normalized,
		keyID:              "app-key",
		version:            1,
		allowLegacyDecrypt: false,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(provider)
	}
	return provider, nil
}

func NewAppKeySecretProviderFromString(key string, opts ...Option) (*AppKeySecretProvider, error) {
	return NewAppKeySecretProvider([]byte(key), opts...)
}

func (p *AppKeySecretProvider) Encrypt(_ context.Context, plaintext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("security: plaintext is required")
	}
	if strings.TrimSpace(p.keyID) == "" {
		return nil, fmt.Errorf("security: key id is required")
	}
	if p.version <= 0 {
		return nil, fmt.Errorf("security: key version must be greater than zero")
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return nil, fmt.Errorf("security: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("security: create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("security: nonce generation failed: %w", err)
	}

	aad := envelopeAAD(p.keyID, p.version, envelopeAlgorithm)
	sealed := gcm.Seal(nil, nonce, plaintext, aad)
	data, err := json.Marshal(envelope{
		KeyID:      p.keyID,
		Version:    p.version,
		Algorithm:  envelopeAlgorithm,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(sealed),
	})
	if err != nil {
		return nil, fmt.Errorf("security: encode envelope: %w", err)
	}

	prefixed := append([]byte(envelopePrefix), data...)
	return prefixed, nil
}

func (p *AppKeySecretProvider) Decrypt(_ context.Context, ciphertext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("security: ciphertext is required")
	}

	payload := string(ciphertext)
	hasPrefix := strings.HasPrefix(payload, envelopePrefix)
	if hasPrefix {
		payload = strings.TrimPrefix(payload, envelopePrefix)
	} else if !p.allowLegacyDecrypt {
		return nil, fmt.Errorf("security: invalid ciphertext envelope prefix")
	}

	var parsed envelope
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, fmt.Errorf("security: decode envelope: %w", err)
	}

	parsed.KeyID = strings.TrimSpace(parsed.KeyID)
	parsed.Algorithm = strings.ToLower(strings.TrimSpace(parsed.Algorithm))
	if parsed.Algorithm == "" {
		parsed.Algorithm = envelopeAlgorithm
	}
	if parsed.Algorithm != envelopeAlgorithm {
		return nil, fmt.Errorf("security: unsupported envelope algorithm %q", parsed.Algorithm)
	}
	if parsed.KeyID == "" || parsed.Version <= 0 {
		if !p.allowLegacyDecrypt {
			return nil, fmt.Errorf("security: envelope metadata is incomplete")
		}
	}

	if parsed.KeyID != "" && parsed.KeyID != p.keyID {
		return nil, fmt.Errorf("security: key id mismatch: got %q want %q", parsed.KeyID, p.keyID)
	}
	if parsed.Version > 0 && parsed.Version != p.version {
		return nil, fmt.Errorf("security: key version mismatch: got %d want %d", parsed.Version, p.version)
	}

	nonce, err := base64.StdEncoding.DecodeString(parsed.Nonce)
	if err != nil {
		return nil, fmt.Errorf("security: decode nonce: %w", err)
	}
	if len(nonce) == 0 {
		return nil, fmt.Errorf("security: nonce is required")
	}
	encryptedPayload, err := base64.StdEncoding.DecodeString(parsed.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("security: decode ciphertext payload: %w", err)
	}

	block, err := aes.NewCipher(p.key)
	if err != nil {
		return nil, fmt.Errorf("security: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("security: create gcm: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("security: invalid nonce length")
	}

	var aad []byte
	if parsed.KeyID != "" && parsed.Version > 0 {
		aad = envelopeAAD(parsed.KeyID, parsed.Version, parsed.Algorithm)
	}
	plaintext, err := gcm.Open(nil, nonce, encryptedPayload, aad)
	if err != nil && p.allowLegacyDecrypt && len(aad) > 0 {
		plaintext, err = gcm.Open(nil, nonce, encryptedPayload, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("security: decrypt payload: %w", err)
	}
	return plaintext, nil
}

func (p *AppKeySecretProvider) KeyID() string {
	if p == nil {
		return ""
	}
	return p.keyID
}

func (p *AppKeySecretProvider) Version() int {
	if p == nil {
		return 0
	}
	return p.version
}

func (p *AppKeySecretProvider) Metadata() (string, int) {
	return p.KeyID(), p.Version()
}

func normalizeKey(value []byte) []byte {
	if len(value) == 16 || len(value) == 24 || len(value) == 32 {
		key := make([]byte, len(value))
		copy(key, value)
		return key
	}
	sum := sha256.Sum256(value)
	key := make([]byte, len(sum))
	copy(key, sum[:])
	return key
}

func envelopeAAD(keyID string, version int, algorithm string) []byte {
	return []byte(
		fmt.Sprintf(
			"%s|%s|%d|%s",
			envelopePrefix,
			strings.TrimSpace(keyID),
			version,
			strings.ToLower(strings.TrimSpace(algorithm)),
		),
	)
}

var _ core.SecretProvider = (*AppKeySecretProvider)(nil)
