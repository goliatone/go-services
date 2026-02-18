package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Service) InvokeCapabilityOperation(
	ctx context.Context,
	req InvokeCapabilityOperationRequest,
) (result CapabilityOperationResult, err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"provider_id": req.ProviderID,
		"scope_type":  req.Scope.Type,
		"scope_id":    req.Scope.ID,
		"capability":  req.Capability,
	}
	defer func() {
		if result.Capability.Connection.ID != "" {
			fields["connection_id"] = result.Capability.Connection.ID
		}
		fields["executed"] = result.Executed
		s.observeOperation(ctx, startedAt, "invoke_capability_operation", err, fields)
	}()

	decision, err := s.InvokeCapability(ctx, InvokeCapabilityRequest{
		ProviderID:   req.ProviderID,
		Scope:        req.Scope,
		Capability:   req.Capability,
		Payload:      copyAnyMap(req.Payload),
		ConnectionID: req.ConnectionID,
	})
	if err != nil {
		return CapabilityOperationResult{}, err
	}
	result.Capability = decision
	if !decision.Allowed && decision.Mode == CapabilityDeniedBehaviorBlock {
		return result, nil
	}

	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return CapabilityOperationResult{}, err
	}
	resolver, ok := provider.(CapabilityOperationResolver)
	if !ok {
		return CapabilityOperationResult{}, s.mapError(fmt.Errorf(
			"core: provider %q does not support capability operation runtime",
			req.ProviderID,
		))
	}

	opRequest, resolveErr := resolver.ResolveCapabilityOperation(ctx, CapabilityOperationResolveRequest{
		ProviderID:      req.ProviderID,
		Scope:           req.Scope,
		Capability:      req.Capability,
		Payload:         copyAnyMap(req.Payload),
		Connection:      decision.Connection,
		Decision:        decision,
		Operation:       req.Operation,
		BucketKey:       req.BucketKey,
		TransportKind:   req.TransportKind,
		TransportConfig: copyAnyMap(req.TransportConfig),
		Metadata:        copyAnyMap(req.Metadata),
	})
	if resolveErr != nil {
		return CapabilityOperationResult{}, s.mapError(resolveErr)
	}

	opRequest.ProviderID = firstNonEmptyTrimmed(opRequest.ProviderID, req.ProviderID)
	opRequest.ConnectionID = firstNonEmptyTrimmed(opRequest.ConnectionID, decision.Connection.ID, req.ConnectionID)
	if strings.TrimSpace(opRequest.Scope.Type) == "" && strings.TrimSpace(opRequest.Scope.ID) == "" {
		opRequest.Scope = ScopeRef{
			Type: strings.TrimSpace(decision.Connection.ScopeType),
			ID:   strings.TrimSpace(decision.Connection.ScopeID),
		}
		if strings.TrimSpace(opRequest.Scope.Type) == "" || strings.TrimSpace(opRequest.Scope.ID) == "" {
			opRequest.Scope = req.Scope
		}
	}
	opRequest.Operation = firstNonEmptyTrimmed(opRequest.Operation, req.Operation, req.Capability)
	opRequest.BucketKey = firstNonEmptyTrimmed(opRequest.BucketKey, req.BucketKey)
	opRequest.TransportKind = firstNonEmptyTrimmed(opRequest.TransportKind, req.TransportKind)
	if len(opRequest.TransportConfig) == 0 {
		opRequest.TransportConfig = copyAnyMap(req.TransportConfig)
	}
	opRequest.Metadata = mergeProviderOperationMetadata(opRequest.Metadata, req.Metadata)
	if isZeroProviderRetryPolicy(opRequest.Retry) {
		opRequest.Retry = req.Retry
	}

	operation, execErr := s.ExecuteProviderOperation(ctx, opRequest)
	if execErr != nil {
		return CapabilityOperationResult{}, execErr
	}

	result.Executed = true
	result.Operation = operation
	return result, nil
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isZeroProviderRetryPolicy(policy ProviderOperationRetryPolicy) bool {
	if policy.MaxAttempts > 0 {
		return false
	}
	if policy.InitialBackoff > 0 || policy.MaxBackoff > 0 {
		return false
	}
	if len(policy.RetryableStatusCodes) > 0 {
		return false
	}
	if policy.Sleep != nil || policy.ShouldRetry != nil {
		return false
	}
	return true
}
