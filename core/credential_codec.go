package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	CredentialPayloadFormatLegacyToken = "legacy_token"
	CredentialPayloadFormatJSONV1      = "active_credential_json"
	CredentialPayloadVersionV1         = 1
)

type CredentialCodec interface {
	Format() string
	Version() int
	Encode(credential ActiveCredential) ([]byte, error)
	Decode(payload []byte) (ActiveCredential, error)
}

type JSONCredentialCodec struct{}

func (JSONCredentialCodec) Format() string {
	return CredentialPayloadFormatJSONV1
}

func (JSONCredentialCodec) Version() int {
	return CredentialPayloadVersionV1
}

type jsonCredentialPayload struct {
	ConnectionID    string         `json:"connection_id,omitempty"`
	TokenType       string         `json:"token_type,omitempty"`
	AccessToken     string         `json:"access_token,omitempty"`
	RefreshToken    string         `json:"refresh_token,omitempty"`
	RequestedScopes []string       `json:"requested_scopes,omitempty"`
	GrantedScopes   []string       `json:"granted_scopes,omitempty"`
	ExpiresAt       *time.Time     `json:"expires_at,omitempty"`
	Refreshable     bool           `json:"refreshable"`
	RotatesAt       *time.Time     `json:"rotates_at,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

func (JSONCredentialCodec) Encode(credential ActiveCredential) ([]byte, error) {
	payload := jsonCredentialPayload{
		ConnectionID:    strings.TrimSpace(credential.ConnectionID),
		TokenType:       strings.TrimSpace(credential.TokenType),
		AccessToken:     strings.TrimSpace(credential.AccessToken),
		RefreshToken:    strings.TrimSpace(credential.RefreshToken),
		RequestedScopes: append([]string(nil), credential.RequestedScopes...),
		GrantedScopes:   append([]string(nil), credential.GrantedScopes...),
		ExpiresAt:       cloneTimePointer(credential.ExpiresAt),
		Refreshable:     credential.Refreshable,
		RotatesAt:       cloneTimePointer(credential.RotatesAt),
		Metadata:        copyAnyMap(credential.Metadata),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("core: encode credential payload: %w", err)
	}
	return encoded, nil
}

func (JSONCredentialCodec) Decode(payload []byte) (ActiveCredential, error) {
	if len(payload) == 0 {
		return ActiveCredential{}, fmt.Errorf("core: credential payload is empty")
	}
	decoded := jsonCredentialPayload{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return ActiveCredential{}, fmt.Errorf("core: decode credential payload: %w", err)
	}
	return ActiveCredential{
		ConnectionID:    strings.TrimSpace(decoded.ConnectionID),
		TokenType:       strings.TrimSpace(decoded.TokenType),
		AccessToken:     strings.TrimSpace(decoded.AccessToken),
		RefreshToken:    strings.TrimSpace(decoded.RefreshToken),
		RequestedScopes: append([]string(nil), decoded.RequestedScopes...),
		GrantedScopes:   append([]string(nil), decoded.GrantedScopes...),
		ExpiresAt:       cloneTimePointer(decoded.ExpiresAt),
		Refreshable:     decoded.Refreshable,
		RotatesAt:       cloneTimePointer(decoded.RotatesAt),
		Metadata:        copyAnyMap(decoded.Metadata),
	}, nil
}

type LegacyTokenCredentialCodec struct{}

func (LegacyTokenCredentialCodec) Format() string {
	return CredentialPayloadFormatLegacyToken
}

func (LegacyTokenCredentialCodec) Version() int {
	return CredentialPayloadVersionV1
}

func (LegacyTokenCredentialCodec) Encode(credential ActiveCredential) ([]byte, error) {
	token := strings.TrimSpace(credential.AccessToken)
	if token == "" {
		token = strings.TrimSpace(credential.RefreshToken)
	}
	if token == "" {
		return nil, fmt.Errorf("core: legacy credential payload requires a token")
	}
	return []byte(token), nil
}

func (LegacyTokenCredentialCodec) Decode(payload []byte) (ActiveCredential, error) {
	token := strings.TrimSpace(string(payload))
	if token == "" {
		return ActiveCredential{}, fmt.Errorf("core: legacy credential payload is empty")
	}
	return ActiveCredential{
		AccessToken: token,
	}, nil
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := value.UTC()
	return &clone
}
