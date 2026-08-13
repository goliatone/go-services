package security

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	envelopePrefix         = "services.secret.v1:"
	envelopeAlgorithm      = "aes-256-gcm"
	envelopeAlgorithmKMS   = "kms"
	envelopeAlgorithmVault = "vault"
)

type envelope struct {
	KeyID      string            `json:"kid"`
	Version    int               `json:"ver"`
	Algorithm  string            `json:"alg"`
	Nonce      string            `json:"nonce,omitempty"`
	Ciphertext string            `json:"ciphertext"`
	Metadata   map[string]string `json:"meta,omitempty"`
}

type envelopeDecodeOptions struct {
	AllowMissingPrefix bool
	DefaultAlgorithm   string
}

type EnvelopeMetadata struct {
	HasPrefix bool
	KeyID     string
	Version   int
	Algorithm string
}

func ParseEnvelopeMetadata(ciphertext []byte, allowMissingPrefix bool) (EnvelopeMetadata, error) {
	env, hasPrefix, err := decodeEnvelope(ciphertext, envelopeDecodeOptions{AllowMissingPrefix: allowMissingPrefix})
	if err != nil {
		return EnvelopeMetadata{}, err
	}
	return EnvelopeMetadata{
		HasPrefix: hasPrefix,
		KeyID:     env.KeyID,
		Version:   env.Version,
		Algorithm: env.Algorithm,
	}, nil
}

func encodeEnvelope(env envelope) ([]byte, error) {
	normalized := normalizeEnvelope(env)
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("security: encode envelope: %w", err)
	}
	return append([]byte(envelopePrefix), data...), nil
}

func encryptManagedEnvelope(
	plaintext []byte,
	backend string,
	keyID string,
	version int,
	algorithm string,
	metadata map[string]string,
	rotationAllowed bool,
	encrypt func([]byte) ([]byte, error),
) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("security: plaintext is required")
	}
	if !rotationAllowed {
		return nil, fmt.Errorf(
			"security: %s key %q version %d is outside the configured rotation window",
			backend,
			keyID,
			version,
		)
	}
	ciphertext, err := encrypt(append([]byte(nil), plaintext...))
	if err != nil {
		return nil, fmt.Errorf("security: %s encrypt: %w", backend, err)
	}
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("security: %s encrypt returned empty ciphertext", backend)
	}
	return encodeEnvelope(envelope{
		KeyID:      keyID,
		Version:    version,
		Algorithm:  algorithm,
		Ciphertext: encodeCiphertextPayload(ciphertext),
		Metadata:   copyStringMap(metadata),
	})
}

type managedEnvelopeKey struct {
	keyID           string
	version         int
	configured      bool
	rotationAllowed bool
}

func decryptManagedEnvelope(
	ciphertext []byte,
	backend string,
	algorithm string,
	allowAny bool,
	resolveKey func(envelope) (managedEnvelopeKey, error),
	decrypt func(managedEnvelopeKey, []byte, map[string]string) ([]byte, error),
) ([]byte, error) {
	env, _, err := decodeEnvelope(ciphertext, envelopeDecodeOptions{DefaultAlgorithm: algorithm})
	if err != nil {
		return nil, err
	}
	if env.Algorithm != algorithm {
		return nil, fmt.Errorf("security: unsupported envelope algorithm %q", env.Algorithm)
	}
	key, err := resolveKey(env)
	if err != nil {
		return nil, err
	}
	if !allowAny && !key.configured {
		return nil, fmt.Errorf(
			"security: %s decrypt key %q version %d is not configured",
			backend,
			key.keyID,
			key.version,
		)
	}
	if !key.rotationAllowed {
		return nil, fmt.Errorf(
			"security: %s key %q version %d is outside the configured rotation window",
			backend,
			key.keyID,
			key.version,
		)
	}
	payload, err := decodeCiphertextPayload(env.Ciphertext)
	if err != nil {
		return nil, err
	}
	plaintext, err := decrypt(key, payload, copyStringMap(env.Metadata))
	if err != nil {
		return nil, fmt.Errorf("security: %s decrypt: %w", backend, err)
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("security: %s decrypt returned empty plaintext", backend)
	}
	return plaintext, nil
}

func decodeEnvelope(ciphertext []byte, options envelopeDecodeOptions) (envelope, bool, error) {
	if len(ciphertext) == 0 {
		return envelope{}, false, fmt.Errorf("security: ciphertext is required")
	}
	payload := string(ciphertext)
	hasPrefix := strings.HasPrefix(payload, envelopePrefix)
	if hasPrefix {
		payload = strings.TrimPrefix(payload, envelopePrefix)
	} else if !options.AllowMissingPrefix {
		return envelope{}, false, fmt.Errorf("security: invalid ciphertext envelope prefix")
	}

	parsed := envelope{}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return envelope{}, false, fmt.Errorf("security: decode envelope: %w", err)
	}
	parsed = normalizeEnvelope(parsed)
	if parsed.Algorithm == "" {
		parsed.Algorithm = strings.ToLower(strings.TrimSpace(options.DefaultAlgorithm))
	}
	if parsed.Ciphertext == "" {
		return envelope{}, false, fmt.Errorf("security: envelope ciphertext is required")
	}
	return parsed, hasPrefix, nil
}

func normalizeEnvelope(in envelope) envelope {
	in.KeyID = strings.TrimSpace(in.KeyID)
	in.Algorithm = strings.ToLower(strings.TrimSpace(in.Algorithm))
	if len(in.Metadata) > 0 {
		normalized := make(map[string]string, len(in.Metadata))
		for key, value := range in.Metadata {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			normalized[trimmedKey] = strings.TrimSpace(value)
		}
		in.Metadata = normalized
	}
	return in
}

func encodeCiphertextPayload(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(value)
}

func decodeCiphertextPayload(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, fmt.Errorf("security: envelope ciphertext is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("security: decode ciphertext payload: %w", err)
	}
	return decoded, nil
}
