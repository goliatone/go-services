package security

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-services/core"
)

type SecretProviderFailurePolicy string

const (
	SecretProviderFailurePolicyStrict   SecretProviderFailurePolicy = "strict_fail"
	SecretProviderFailurePolicyFallback SecretProviderFailurePolicy = "fallback_allowed"
)

type SecretProviderDiagnostic struct {
	OccurredAt time.Time
	Operation  string
	Policy     SecretProviderFailurePolicy
	Outcome    string
	Primary    string
	Fallback   string
	Error      string
}

type SecretProviderDiagnosticHook func(event SecretProviderDiagnostic)

type FailoverOption func(*FailoverSecretProvider)

type providerMetadataPair struct {
	KeyID   string
	Version int
}

type FailoverSecretProvider struct {
	primary        core.SecretProvider
	fallback       core.SecretProvider
	policy         SecretProviderFailurePolicy
	diagnosticHook SecretProviderDiagnosticHook
	now            func() time.Time

	mu             sync.RWMutex
	lastEncryption providerMetadataPair
}

func NewFailoverSecretProvider(primary core.SecretProvider, opts ...FailoverOption) (*FailoverSecretProvider, error) {
	if primary == nil {
		return nil, fmt.Errorf("security: primary secret provider is required")
	}
	provider := &FailoverSecretProvider{
		primary: primary,
		policy:  SecretProviderFailurePolicyStrict,
		now:     func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(provider)
	}
	provider.policy = normalizeFailurePolicy(provider.policy)
	if provider.policy == SecretProviderFailurePolicyFallback && provider.fallback == nil {
		return nil, fmt.Errorf("security: fallback policy requires a configured fallback secret provider")
	}
	if provider.now == nil {
		provider.now = func() time.Time { return time.Now().UTC() }
	}
	provider.recordMetadata(provider.primary)
	return provider, nil
}

func WithFallbackSecretProvider(provider core.SecretProvider) FailoverOption {
	return func(f *FailoverSecretProvider) {
		if f == nil {
			return
		}
		f.fallback = provider
	}
}

func WithSecretProviderFailurePolicy(policy SecretProviderFailurePolicy) FailoverOption {
	return func(f *FailoverSecretProvider) {
		if f == nil {
			return
		}
		f.policy = normalizeFailurePolicy(policy)
	}
}

func WithSecretProviderDiagnostics(hook SecretProviderDiagnosticHook) FailoverOption {
	return func(f *FailoverSecretProvider) {
		if f == nil {
			return
		}
		f.diagnosticHook = hook
	}
}

func WithFailoverClock(now func() time.Time) FailoverOption {
	return func(f *FailoverSecretProvider) {
		if f == nil {
			return
		}
		f.now = now
	}
}

func (p *FailoverSecretProvider) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("security: plaintext is required")
	}
	ciphertext, err := p.primary.Encrypt(ctx, plaintext)
	if err == nil {
		p.recordMetadata(p.primary)
		return ciphertext, nil
	}
	p.emit("encrypt", "primary_failed", err)
	if p.policy == SecretProviderFailurePolicyStrict || p.fallback == nil {
		return nil, fmt.Errorf("security: primary encrypt failed with %s policy: %w", p.policy, err)
	}
	fallbackCiphertext, fallbackErr := p.fallback.Encrypt(ctx, plaintext)
	if fallbackErr != nil {
		p.emit("encrypt", "fallback_failed", fallbackErr)
		return nil, fmt.Errorf("security: primary encrypt failed: %v; fallback encrypt failed: %w", err, fallbackErr)
	}
	p.recordMetadata(p.fallback)
	p.emit("encrypt", "fallback_succeeded", err)
	return fallbackCiphertext, nil
}

func (p *FailoverSecretProvider) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("security: secret provider is nil")
	}
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("security: ciphertext is required")
	}
	plaintext, err := p.primary.Decrypt(ctx, ciphertext)
	if err == nil {
		return plaintext, nil
	}
	p.emit("decrypt", "primary_failed", err)
	if p.policy == SecretProviderFailurePolicyStrict || p.fallback == nil {
		return nil, fmt.Errorf("security: primary decrypt failed with %s policy: %w", p.policy, err)
	}
	fallbackPlaintext, fallbackErr := p.fallback.Decrypt(ctx, ciphertext)
	if fallbackErr != nil {
		p.emit("decrypt", "fallback_failed", fallbackErr)
		return nil, fmt.Errorf("security: primary decrypt failed: %v; fallback decrypt failed: %w", err, fallbackErr)
	}
	p.emit("decrypt", "fallback_succeeded", err)
	return fallbackPlaintext, nil
}

func (p *FailoverSecretProvider) Metadata() (string, int) {
	if p == nil {
		return "", 0
	}
	p.mu.RLock()
	last := p.lastEncryption
	p.mu.RUnlock()
	if strings.TrimSpace(last.KeyID) != "" && last.Version > 0 {
		return last.KeyID, last.Version
	}
	if keyID, version, ok := readProviderMetadata(p.primary); ok {
		return keyID, version
	}
	if keyID, version, ok := readProviderMetadata(p.fallback); ok {
		return keyID, version
	}
	return "", 0
}

func (p *FailoverSecretProvider) emit(operation string, outcome string, err error) {
	if p == nil || p.diagnosticHook == nil {
		return
	}
	now := p.now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	p.diagnosticHook(SecretProviderDiagnostic{
		OccurredAt: now().UTC(),
		Operation:  operation,
		Policy:     p.policy,
		Outcome:    outcome,
		Primary:    describeSecretProvider(p.primary),
		Fallback:   describeSecretProvider(p.fallback),
		Error:      msg,
	})
}

func (p *FailoverSecretProvider) recordMetadata(provider core.SecretProvider) {
	if p == nil {
		return
	}
	keyID, version, ok := readProviderMetadata(provider)
	if !ok {
		return
	}
	p.mu.Lock()
	p.lastEncryption = providerMetadataPair{KeyID: keyID, Version: version}
	p.mu.Unlock()
}

func normalizeFailurePolicy(policy SecretProviderFailurePolicy) SecretProviderFailurePolicy {
	normalized := SecretProviderFailurePolicy(strings.ToLower(strings.TrimSpace(string(policy))))
	switch normalized {
	case SecretProviderFailurePolicyFallback:
		return SecretProviderFailurePolicyFallback
	default:
		return SecretProviderFailurePolicyStrict
	}
}

func readProviderMetadata(provider core.SecretProvider) (string, int, bool) {
	if provider == nil {
		return "", 0, false
	}
	metadataProvider, ok := provider.(interface{ Metadata() (string, int) })
	if !ok {
		return "", 0, false
	}
	keyID, version := metadataProvider.Metadata()
	keyID = strings.TrimSpace(keyID)
	if keyID == "" || version <= 0 {
		return "", 0, false
	}
	return keyID, version, true
}

func describeSecretProvider(provider core.SecretProvider) string {
	if provider == nil {
		return ""
	}
	label := reflect.TypeOf(provider).String()
	if keyID, version, ok := readProviderMetadata(provider); ok {
		return fmt.Sprintf("%s(%s:%d)", label, keyID, version)
	}
	return label
}

var _ core.SecretProvider = (*FailoverSecretProvider)(nil)
