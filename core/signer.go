package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ProviderSigner interface {
	Signer() Signer
}

type BearerTokenSigner struct{}

func (BearerTokenSigner) Sign(_ context.Context, req *http.Request, cred ActiveCredential) error {
	if req == nil {
		return fmt.Errorf("core: http request is required")
	}
	token := strings.TrimSpace(cred.AccessToken)
	if token == "" {
		return fmt.Errorf("core: access token is required for bearer signing")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

type APIKeySigner struct {
	Header     string
	Prefix     string
	QueryParam string
}

func (s APIKeySigner) Sign(_ context.Context, req *http.Request, cred ActiveCredential) error {
	if req == nil {
		return fmt.Errorf("core: http request is required")
	}
	key := strings.TrimSpace(cred.AccessToken)
	if key == "" {
		return fmt.Errorf("core: api key token is required for signing")
	}

	header := strings.TrimSpace(s.Header)
	if header == "" {
		header = "X-API-Key"
	}
	if queryParam := strings.TrimSpace(s.QueryParam); queryParam != "" {
		setQueryValue(req.URL, queryParam, key)
	}

	value := key
	if prefix := strings.TrimSpace(s.Prefix); prefix != "" {
		value = prefix + " " + key
	}
	req.Header.Set(header, value)
	return nil
}

type PATSigner struct{}

func (PATSigner) Sign(ctx context.Context, req *http.Request, cred ActiveCredential) error {
	return APIKeySigner{
		Header: "Authorization",
		Prefix: "token",
	}.Sign(ctx, req, cred)
}

type HMACSigner struct {
	SignatureHeader string
	TimestampHeader string
	Now             func() time.Time
}

func (s HMACSigner) Sign(_ context.Context, req *http.Request, cred ActiveCredential) error {
	if req == nil {
		return fmt.Errorf("core: http request is required")
	}
	secret := strings.TrimSpace(cred.AccessToken)
	if secret == "" {
		return fmt.Errorf("core: hmac secret is required for signing")
	}

	signatureHeader := strings.TrimSpace(s.SignatureHeader)
	if signatureHeader == "" {
		signatureHeader = "X-Signature"
	}
	timestampHeader := strings.TrimSpace(s.TimestampHeader)
	if timestampHeader == "" {
		timestampHeader = "X-Timestamp"
	}
	nowFn := s.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	timestamp := fmt.Sprintf("%d", nowFn().UTC().Unix())
	bodyHash, err := readRequestBodyHash(req)
	if err != nil {
		return err
	}

	canonical := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(req.Method)),
		req.URL.Path,
		timestamp,
		bodyHash,
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	signature := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set(timestampHeader, timestamp)
	req.Header.Set(signatureHeader, signature)
	return nil
}

type BasicAuthSigner struct{}

func (BasicAuthSigner) Sign(_ context.Context, req *http.Request, cred ActiveCredential) error {
	if req == nil {
		return fmt.Errorf("core: http request is required")
	}
	token := strings.TrimSpace(cred.AccessToken)
	if token == "" {
		return fmt.Errorf("core: basic auth token is required for signing")
	}
	req.Header.Set("Authorization", "Basic "+token)
	return nil
}

type MTLSSigner struct{}

func (MTLSSigner) Sign(_ context.Context, req *http.Request, _ ActiveCredential) error {
	if req == nil {
		return fmt.Errorf("core: http request is required")
	}
	return nil
}

func (s *Service) resolveSignerForProvider(provider Provider) Signer {
	if provider != nil {
		if signerProvider, ok := provider.(ProviderSigner); ok {
			if signer := signerProvider.Signer(); signer != nil {
				return signer
			}
		}
	}
	return s.signer
}

func (s *Service) resolveSignerForCredential(provider Provider, cred *ActiveCredential) Signer {
	if signer := s.resolveSignerForProvider(provider); signer != nil {
		if provider != nil {
			if _, ok := provider.(ProviderSigner); ok {
				return signer
			}
		}
	}
	if cred != nil {
		if s == nil || s.signer == nil {
			if authSigner := resolveAuthKindSigner(*cred); authSigner != nil {
				return authSigner
			}
		} else {
			if _, isDefaultBearer := s.signer.(BearerTokenSigner); isDefaultBearer {
				if authSigner := resolveAuthKindSigner(*cred); authSigner != nil {
					return authSigner
				}
			}
		}
	}
	return s.resolveSignerForProvider(provider)
}

func (s *Service) SignRequest(
	ctx context.Context,
	providerID string,
	connectionID string,
	req *http.Request,
	cred *ActiveCredential,
) error {
	if s == nil {
		return fmt.Errorf("core: service is nil")
	}
	if req == nil {
		return s.mapError(fmt.Errorf("core: http request is required"))
	}

	connectionID = strings.TrimSpace(connectionID)
	resolvedProviderID := strings.TrimSpace(providerID)
	if s.connectionStore != nil && connectionID != "" {
		connection, loadErr := s.connectionStore.Get(ctx, connectionID)
		if loadErr != nil {
			return s.mapError(loadErr)
		}
		connectionProviderID := strings.TrimSpace(connection.ProviderID)
		if connectionProviderID == "" {
			return s.mapError(fmt.Errorf("core: connection %q has no provider id", connectionID))
		}
		if resolvedProviderID == "" {
			resolvedProviderID = connectionProviderID
		} else if !strings.EqualFold(resolvedProviderID, connectionProviderID) {
			return s.mapError(
				fmt.Errorf(
					"core: provider mismatch for connection %q: got %q want %q",
					connectionID,
					resolvedProviderID,
					connectionProviderID,
				),
			)
		}
	}
	if resolvedProviderID == "" {
		return s.mapError(fmt.Errorf("core: provider id is required for signing"))
	}

	provider, err := s.resolveProvider(resolvedProviderID)
	if err != nil {
		return err
	}
	active := ActiveCredential{}
	if cred != nil {
		active = *cred
	} else if s.credentialStore != nil {
		if connectionID == "" {
			return s.mapError(fmt.Errorf("core: connection id is required to load signing credential"))
		}
		stored, loadErr := s.credentialStore.GetActiveByConnection(ctx, connectionID)
		if loadErr != nil {
			return s.mapError(loadErr)
		}
		resolved, resolveErr := s.credentialToActive(ctx, stored)
		if resolveErr != nil {
			return s.mapError(resolveErr)
		}
		active = resolved
	} else {
		return s.mapError(fmt.Errorf("core: credential is required for signing"))
	}

	signer := s.resolveSignerForCredential(provider, &active)
	if signer == nil {
		return s.mapError(fmt.Errorf("core: signer is not configured"))
	}

	if signErr := signer.Sign(ctx, req, active); signErr != nil {
		return s.mapError(signErr)
	}
	return nil
}

func resolveAuthKindSigner(cred ActiveCredential) Signer {
	authKind := strings.TrimSpace(strings.ToLower(resolveCredentialAuthKind(cred)))
	switch authKind {
	case AuthKindAPIKey:
		return APIKeySigner{
			Header:     firstNonEmpty(metadataString(cred.Metadata, "api_key_header"), "X-API-Key"),
			Prefix:     metadataString(cred.Metadata, "api_key_prefix"),
			QueryParam: metadataString(cred.Metadata, "api_key_query_param"),
		}
	case AuthKindPAT:
		return PATSigner{}
	case AuthKindHMAC:
		signer := HMACSigner{
			SignatureHeader: firstNonEmpty(metadataString(cred.Metadata, "signature_header"), "X-Signature"),
			TimestampHeader: firstNonEmpty(metadataString(cred.Metadata, "timestamp_header"), "X-Timestamp"),
		}
		if raw := metadataString(cred.Metadata, "hmac_timestamp_unix"); raw != "" {
			if unix, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && unix > 0 {
				t := time.Unix(unix, 0).UTC()
				signer.Now = func() time.Time { return t }
			}
		}
		return signer
	case AuthKindBasic:
		return BasicAuthSigner{}
	case AuthKindMTLS:
		return MTLSSigner{}
	case AuthKindAWSSigV4:
		return AWSSigV4Signer{}
	default:
		return nil
	}
}

func resolveCredentialAuthKind(cred ActiveCredential) string {
	if raw := metadataString(cred.Metadata, "auth_kind"); raw != "" {
		return raw
	}
	if strings.TrimSpace(cred.TokenType) != "" {
		return strings.TrimSpace(strings.ToLower(cred.TokenType))
	}
	return ""
}

func readRequestBodyHash(req *http.Request) (string, error) {
	if req == nil || req.Body == nil {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:]), nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return "", fmt.Errorf("core: read request body: %w", err)
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func setQueryValue(requestURL *url.URL, key string, value string) {
	if requestURL == nil {
		return
	}
	query := requestURL.Query()
	query.Set(key, value)
	requestURL.RawQuery = query.Encode()
}
