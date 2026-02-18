package core

import (
	"context"
	"strings"
)

type CallbackURLResolveFlow string

const (
	CallbackURLResolveFlowConnect   CallbackURLResolveFlow = "connect"
	CallbackURLResolveFlowReconsent CallbackURLResolveFlow = "reconsent"
)

type CallbackURLResolveRequest struct {
	ProviderID      string
	Scope           ScopeRef
	ConnectionID    string
	Flow            CallbackURLResolveFlow
	RequestedGrants []string
	Metadata        map[string]any
}

type CallbackURLResolver interface {
	ResolveCallbackURL(ctx context.Context, req CallbackURLResolveRequest) (string, error)
}

type CallbackURLResolverFunc func(ctx context.Context, req CallbackURLResolveRequest) (string, error)

func (fn CallbackURLResolverFunc) ResolveCallbackURL(ctx context.Context, req CallbackURLResolveRequest) (string, error) {
	if fn == nil {
		return "", nil
	}
	url, err := fn(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(url), nil
}
