package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultCredentialExpiringSoonWindow = 5 * time.Minute
	DefaultCredentialRefreshLeadWindow  = 5 * time.Minute
)

// CredentialTokenState captures access/refresh lifecycle state derived from an active credential.
type CredentialTokenState struct {
	ExpiresAt       *time.Time
	HasAccessToken  bool
	HasRefreshToken bool
	CanAutoRefresh  bool
	IsExpired       bool
	IsExpiringSoon  bool
}

// EnsureCredentialFreshRequest resolves and conditionally refreshes a connection credential.
type EnsureCredentialFreshRequest struct {
	ProviderID         string
	ConnectionID       string
	Credential         *ActiveCredential
	RefreshLeadWindow  time.Duration
	ExpiringSoonWindow time.Duration
}

// EnsureCredentialFreshResult returns resolved credential state and refresh outcomes.
type EnsureCredentialFreshResult struct {
	Credential       ActiveCredential
	State            CredentialTokenState
	RefreshAttempted bool
	Refreshed        bool
}

// ResolveCredentialTokenState evaluates expiry and refreshability flags for a credential.
func ResolveCredentialTokenState(now time.Time, credential ActiveCredential, expiringSoonWindow time.Duration) CredentialTokenState {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if expiringSoonWindow <= 0 {
		expiringSoonWindow = DefaultCredentialExpiringSoonWindow
	}

	state := CredentialTokenState{
		HasAccessToken:  strings.TrimSpace(credential.AccessToken) != "",
		HasRefreshToken: strings.TrimSpace(credential.RefreshToken) != "",
		CanAutoRefresh:  credential.Refreshable && strings.TrimSpace(credential.RefreshToken) != "",
	}
	if credential.ExpiresAt == nil {
		return state
	}
	expiresAt := credential.ExpiresAt.UTC()
	state.ExpiresAt = &expiresAt
	if !expiresAt.After(now) {
		state.IsExpired = true
		return state
	}
	state.IsExpiringSoon = !expiresAt.After(now.Add(expiringSoonWindow))
	return state
}

// ShouldRefreshCredential returns true when refresh should be attempted before provider operations.
func ShouldRefreshCredential(now time.Time, state CredentialTokenState, refreshLeadWindow time.Duration) bool {
	if !state.CanAutoRefresh {
		return false
	}
	if !state.HasAccessToken {
		return true
	}
	if state.ExpiresAt == nil {
		return false
	}
	if refreshLeadWindow <= 0 {
		refreshLeadWindow = DefaultCredentialRefreshLeadWindow
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	return !state.ExpiresAt.UTC().After(now.Add(refreshLeadWindow))
}

// EnsureCredentialFresh resolves active credentials and refreshes when needed.
func (s *Service) EnsureCredentialFresh(ctx context.Context, req EnsureCredentialFreshRequest) (EnsureCredentialFreshResult, error) {
	if s == nil {
		return EnsureCredentialFreshResult{}, fmt.Errorf("core: service is nil")
	}

	connectionID := strings.TrimSpace(req.ConnectionID)
	if req.Credential != nil && connectionID == "" {
		connectionID = strings.TrimSpace(req.Credential.ConnectionID)
	}
	if connectionID == "" {
		return EnsureCredentialFreshResult{}, s.mapError(fmt.Errorf("core: connection id is required"))
	}

	expiringSoonWindow := req.ExpiringSoonWindow
	if expiringSoonWindow <= 0 {
		expiringSoonWindow = DefaultCredentialExpiringSoonWindow
	}
	refreshLeadWindow := req.RefreshLeadWindow
	if refreshLeadWindow <= 0 {
		refreshLeadWindow = DefaultCredentialRefreshLeadWindow
	}

	active := ActiveCredential{}
	if req.Credential != nil {
		active = *req.Credential
	} else {
		if s.credentialStore == nil {
			return EnsureCredentialFreshResult{}, s.mapError(fmt.Errorf("core: credential store is not configured"))
		}
		stored, err := s.credentialStore.GetActiveByConnection(ctx, connectionID)
		if err != nil {
			return EnsureCredentialFreshResult{}, s.mapError(err)
		}
		decoded, err := s.credentialToActive(ctx, stored)
		if err != nil {
			return EnsureCredentialFreshResult{}, s.mapError(err)
		}
		active = decoded
	}
	if strings.TrimSpace(active.ConnectionID) == "" {
		active.ConnectionID = connectionID
	}

	now := time.Now().UTC()
	state := ResolveCredentialTokenState(now, active, expiringSoonWindow)
	result := EnsureCredentialFreshResult{
		Credential: active,
		State:      state,
	}
	if !ShouldRefreshCredential(now, state, refreshLeadWindow) {
		return result, nil
	}

	result.RefreshAttempted = true
	refreshResult, err := s.Refresh(ctx, RefreshRequest{
		ProviderID:   strings.TrimSpace(req.ProviderID),
		ConnectionID: connectionID,
		Credential:   &active,
	})
	if err != nil {
		return result, err
	}

	refreshed := refreshResult.Credential
	if strings.TrimSpace(refreshed.ConnectionID) == "" {
		refreshed.ConnectionID = connectionID
	}
	result.Credential = refreshed
	result.State = ResolveCredentialTokenState(now, refreshed, expiringSoonWindow)
	result.Refreshed = true
	return result, nil
}
