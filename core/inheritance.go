package core

import (
	"context"
	"strings"
)

type StrictIsolationPolicy struct {
	ConnectionStore ConnectionStore
}

func (p *StrictIsolationPolicy) ResolveConnection(ctx context.Context, providerID string, requested ScopeRef) (ConnectionResolution, error) {
	if err := requested.Validate(); err != nil {
		return ConnectionResolution{Outcome: ConnectionResolutionNotFound, Reason: err.Error()}, nil
	}
	if p == nil || p.ConnectionStore == nil {
		return ConnectionResolution{Outcome: ConnectionResolutionNotFound, Reason: "connection store unavailable"}, nil
	}
	connections, err := p.ConnectionStore.FindByScope(ctx, strings.TrimSpace(providerID), requested)
	if err != nil {
		return ConnectionResolution{}, err
	}
	for _, conn := range connections {
		if conn.Status == ConnectionStatusActive {
			return ConnectionResolution{Outcome: ConnectionResolutionDirect, Connection: conn}, nil
		}
	}
	return ConnectionResolution{Outcome: ConnectionResolutionNotFound, Reason: "no active direct connection"}, nil
}

func allowProviderInheritance(providerID string, cfg InheritanceConfig) bool {
	id := strings.TrimSpace(providerID)
	if id == "" {
		return false
	}
	for _, candidate := range cfg.EnabledProviders {
		if strings.EqualFold(strings.TrimSpace(candidate), id) {
			return true
		}
	}
	return false
}
