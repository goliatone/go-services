package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/goliatone/go-services/core"
)

type AWSSigV4SigningProfile struct {
	AccessKeyID       string
	SecretAccessKey   string
	SessionToken      string
	Region            string
	Service           string
	SigningMode       string
	SigningExpiresSec int
	UnsignedPayload   bool
	AccessTokenHeader string
}

type OAuth2SigV4StrategyConfig struct {
	OAuth2  OAuth2ClientCredentialsStrategyConfig
	Profile AWSSigV4SigningProfile
}

type OAuth2SigV4Strategy struct {
	delegate *OAuth2ClientCredentialsStrategy
	profile  AWSSigV4SigningProfile
}

func NewOAuth2SigV4Strategy(cfg OAuth2SigV4StrategyConfig) *OAuth2SigV4Strategy {
	profile := cfg.Profile
	profile.SigningMode = normalizeSigningMode(profile.SigningMode)
	if profile.AccessTokenHeader == "" {
		profile.AccessTokenHeader = "x-amz-access-token"
	}
	return &OAuth2SigV4Strategy{
		delegate: NewOAuth2ClientCredentialsStrategy(cfg.OAuth2),
		profile:  profile,
	}
}

func (*OAuth2SigV4Strategy) Type() string { return core.AuthKindAWSSigV4 }

func (s *OAuth2SigV4Strategy) Begin(ctx context.Context, req core.AuthBeginRequest) (core.AuthBeginResponse, error) {
	if s == nil || s.delegate == nil {
		return core.AuthBeginResponse{}, fmt.Errorf("auth: oauth2 sigv4 strategy is not configured")
	}
	begin, err := s.delegate.Begin(ctx, req)
	if err != nil {
		return core.AuthBeginResponse{}, err
	}
	begin.Metadata = cloneMetadata(begin.Metadata)
	begin.Metadata["auth_kind"] = core.AuthKindAWSSigV4
	begin.Metadata["signing_profile"] = core.AuthKindAWSSigV4
	return begin, nil
}

func (s *OAuth2SigV4Strategy) Complete(ctx context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
	if s == nil || s.delegate == nil {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: oauth2 sigv4 strategy is not configured")
	}
	complete, err := s.delegate.Complete(ctx, req)
	if err != nil {
		return core.AuthCompleteResponse{}, err
	}
	complete.Credential.Metadata = applySigV4ProfileMetadata(
		cloneMetadata(complete.Credential.Metadata),
		s.profile,
		req.Metadata,
	)
	if err := validateSigV4ProfileMetadata(complete.Credential.Metadata); err != nil {
		return core.AuthCompleteResponse{}, err
	}
	complete.Credential.Metadata["auth_kind"] = core.AuthKindAWSSigV4
	complete.Metadata = cloneMetadata(complete.Metadata)
	complete.Metadata["auth_kind"] = core.AuthKindAWSSigV4
	complete.Metadata["signing_profile"] = core.AuthKindAWSSigV4
	return complete, nil
}

func (s *OAuth2SigV4Strategy) Refresh(ctx context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	if s == nil || s.delegate == nil {
		return core.RefreshResult{}, fmt.Errorf("auth: oauth2 sigv4 strategy is not configured")
	}
	refreshed, err := s.delegate.Refresh(ctx, cred)
	if err != nil {
		return core.RefreshResult{}, err
	}
	refreshed.Credential.Metadata = applySigV4ProfileMetadata(
		cloneMetadata(refreshed.Credential.Metadata),
		s.profile,
		cred.Metadata,
	)
	if err := validateSigV4ProfileMetadata(refreshed.Credential.Metadata); err != nil {
		return core.RefreshResult{}, err
	}
	refreshed.Credential.Metadata["auth_kind"] = core.AuthKindAWSSigV4
	refreshed.Metadata = cloneMetadata(refreshed.Metadata)
	refreshed.Metadata["auth_kind"] = core.AuthKindAWSSigV4
	refreshed.Metadata["signing_profile"] = core.AuthKindAWSSigV4
	return refreshed, nil
}

func applySigV4ProfileMetadata(
	metadata map[string]any,
	profile AWSSigV4SigningProfile,
	runtime map[string]any,
) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["auth_kind"] = core.AuthKindAWSSigV4
	metadata["aws_access_key_id"] = firstNonEmpty(
		readString(runtime, "aws_access_key_id", "access_key_id"),
		profile.AccessKeyID,
		readString(metadata, "aws_access_key_id", "access_key_id"),
	)
	metadata["aws_secret_access_key"] = firstNonEmpty(
		readString(runtime, "aws_secret_access_key", "secret_access_key"),
		profile.SecretAccessKey,
		readString(metadata, "aws_secret_access_key", "secret_access_key"),
	)
	metadata["aws_session_token"] = firstNonEmpty(
		readString(runtime, "aws_session_token", "session_token"),
		profile.SessionToken,
		readString(metadata, "aws_session_token", "session_token"),
	)
	metadata["aws_region"] = firstNonEmpty(
		readString(runtime, "aws_region", "region"),
		profile.Region,
		readString(metadata, "aws_region", "region"),
	)
	metadata["aws_service"] = firstNonEmpty(
		readString(runtime, "aws_service", "service"),
		profile.Service,
		readString(metadata, "aws_service", "service"),
	)
	metadata["aws_signing_mode"] = normalizeSigningMode(firstNonEmpty(
		readString(runtime, "aws_signing_mode", "signing_mode"),
		profile.SigningMode,
		readString(metadata, "aws_signing_mode", "signing_mode"),
	))
	accessTokenHeader := firstNonEmpty(
		strings.ToLower(readString(runtime, "aws_access_token_header")),
		strings.ToLower(strings.TrimSpace(profile.AccessTokenHeader)),
		strings.ToLower(readString(metadata, "aws_access_token_header")),
	)
	if accessTokenHeader == "" {
		accessTokenHeader = "x-amz-access-token"
	}
	metadata["aws_access_token_header"] = accessTokenHeader

	if raw := firstNonEmpty(
		readString(runtime, "aws_signing_expires", "aws_query_expires"),
		strconv.Itoa(profile.SigningExpiresSec),
		readString(metadata, "aws_signing_expires", "aws_query_expires"),
	); raw != "" && raw != "0" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds > 0 {
			metadata["aws_signing_expires"] = strconv.Itoa(seconds)
		}
	}
	unsignedPayload := profile.UnsignedPayload
	if value := strings.TrimSpace(readString(runtime, "aws_unsigned_payload")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			unsignedPayload = parsed
		}
	}
	metadata["aws_unsigned_payload"] = unsignedPayload
	return metadata
}

func validateSigV4ProfileMetadata(metadata map[string]any) error {
	if strings.TrimSpace(readString(metadata, "aws_access_key_id", "access_key_id")) == "" {
		return fmt.Errorf("auth: aws sigv4 requires aws_access_key_id")
	}
	if strings.TrimSpace(readString(metadata, "aws_secret_access_key", "secret_access_key")) == "" {
		return fmt.Errorf("auth: aws sigv4 requires aws_secret_access_key")
	}
	if strings.TrimSpace(readString(metadata, "aws_region", "region")) == "" {
		return fmt.Errorf("auth: aws sigv4 requires aws_region")
	}
	if strings.TrimSpace(readString(metadata, "aws_service", "service")) == "" {
		return fmt.Errorf("auth: aws sigv4 requires aws_service")
	}
	return nil
}

func normalizeSigningMode(mode string) string {
	normalized := strings.TrimSpace(strings.ToLower(mode))
	if normalized == "query" {
		return "query"
	}
	return "header"
}
