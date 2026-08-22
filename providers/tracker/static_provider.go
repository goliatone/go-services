package tracker

import (
	"context"
	"errors"
	"strings"

	"github.com/goliatone/go-services/core"
)

type StaticProvider struct {
	ProviderID string
	Kind       core.AuthKind
	ScopeTypes []string
	Grants     []core.CapabilityDescriptor
}

func (p StaticProvider) ID() string                    { return strings.TrimSpace(strings.ToLower(p.ProviderID)) }
func (p StaticProvider) AuthKind() core.AuthKind       { return p.Kind }
func (p StaticProvider) SupportedScopeTypes() []string { return append([]string(nil), p.ScopeTypes...) }
func (p StaticProvider) Capabilities() []core.CapabilityDescriptor {
	return append([]core.CapabilityDescriptor(nil), p.Grants...)
}
func (p StaticProvider) BeginAuth(context.Context, core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	return core.BeginAuthResponse{}, errors.New("tracker: static credential providers do not support interactive auth")
}
func (p StaticProvider) CompleteAuth(context.Context, core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	return core.CompleteAuthResponse{}, errors.New("tracker: static credential providers do not support interactive auth")
}
func (p StaticProvider) Refresh(_ context.Context, credential core.ActiveCredential) (core.RefreshResult, error) {
	return core.RefreshResult{Credential: credential, GrantedGrants: append([]string(nil), credential.GrantedScopes...)}, nil
}

var _ core.Provider = StaticProvider{}
