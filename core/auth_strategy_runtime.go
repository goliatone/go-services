package core

import (
	"context"
	"strings"
)

type providerBackedAuthStrategy struct {
	provider Provider
}

func (s providerBackedAuthStrategy) Type() string {
	if s.provider == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(s.provider.AuthKind()))
}

func (s providerBackedAuthStrategy) Begin(ctx context.Context, req AuthBeginRequest) (AuthBeginResponse, error) {
	result, err := s.provider.BeginAuth(ctx, BeginAuthRequest{
		ProviderID:      s.provider.ID(),
		Scope:           req.Scope,
		RedirectURI:     req.RedirectURI,
		State:           req.State,
		RequestedGrants: append([]string(nil), req.RequestedRaw...),
		Metadata:        copyAnyMap(req.Metadata),
	})
	if err != nil {
		return AuthBeginResponse{}, err
	}
	return AuthBeginResponse{
		URL:             result.URL,
		State:           result.State,
		RequestedGrants: append([]string(nil), result.RequestedGrants...),
		Metadata:        copyAnyMap(result.Metadata),
	}, nil
}

func (s providerBackedAuthStrategy) Complete(ctx context.Context, req AuthCompleteRequest) (AuthCompleteResponse, error) {
	result, err := s.provider.CompleteAuth(ctx, CompleteAuthRequest{
		ProviderID:  s.provider.ID(),
		Scope:       req.Scope,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		Metadata:    copyAnyMap(req.Metadata),
	})
	if err != nil {
		return AuthCompleteResponse{}, err
	}
	return AuthCompleteResponse{
		ExternalAccountID: result.ExternalAccountID,
		Credential:        result.Credential,
		RequestedGrants:   append([]string(nil), result.RequestedGrants...),
		GrantedGrants:     append([]string(nil), result.GrantedGrants...),
		Metadata:          copyAnyMap(result.Metadata),
	}, nil
}

func (s providerBackedAuthStrategy) Refresh(ctx context.Context, cred ActiveCredential) (RefreshResult, error) {
	return s.provider.Refresh(ctx, cred)
}

func authKindRequiresCallbackState(kind string) bool {
	normalized := strings.TrimSpace(strings.ToLower(kind))
	return normalized == AuthKindOAuth2AuthCode || normalized == "oauth2"
}

func strategyRequiresCallbackState(strategy AuthStrategy) bool {
	if strategy == nil {
		return false
	}
	return authKindRequiresCallbackState(strategy.Type())
}

func (s *Service) resolveAuthStrategy(provider Provider) AuthStrategy {
	if provider == nil {
		return nil
	}
	if strategyProvider, ok := provider.(AuthStrategyProvider); ok {
		if strategy := strategyProvider.AuthStrategy(); strategy != nil {
			return strategy
		}
	}
	return providerBackedAuthStrategy{provider: provider}
}
