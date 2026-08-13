package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type GrantPermissionEvaluator struct {
	ConnectionStore ConnectionStore
	GrantStore      GrantStore
	Registry        Registry
}

func NewGrantPermissionEvaluator(connectionStore ConnectionStore, grantStore GrantStore, registry Registry) *GrantPermissionEvaluator {
	return &GrantPermissionEvaluator{
		ConnectionStore: connectionStore,
		GrantStore:      grantStore,
		Registry:        registry,
	}
}

func (e *GrantPermissionEvaluator) EvaluateCapability(
	ctx context.Context,
	connectionID string,
	capability string,
) (PermissionDecision, error) {
	if e == nil {
		return PermissionDecision{Allowed: true, Capability: capability}, nil
	}
	if err := validateCapabilityEvaluation(connectionID, capability); err != nil {
		return PermissionDecision{}, err
	}
	if e.ConnectionStore == nil || e.Registry == nil {
		return PermissionDecision{Allowed: true, Capability: capability}, nil
	}

	descriptor, found, err := e.resolveCapabilityDescriptor(ctx, connectionID, capability)
	if err != nil {
		return PermissionDecision{}, err
	}
	if !found {
		return PermissionDecision{
			Allowed:    false,
			Capability: capability,
			Reason:     "capability is not supported by provider",
			Mode:       CapabilityDeniedBehaviorBlock,
		}, nil
	}

	granted, err := e.grantedCapabilities(ctx, connectionID)
	if err != nil {
		return PermissionDecision{}, err
	}
	return capabilityPermissionDecision(capability, descriptor, granted), nil
}

func validateCapabilityEvaluation(connectionID, capability string) error {
	if strings.TrimSpace(connectionID) == "" {
		return fmt.Errorf("core: connection id is required")
	}
	if strings.TrimSpace(capability) == "" {
		return fmt.Errorf("core: capability is required")
	}
	return nil
}

func (e *GrantPermissionEvaluator) resolveCapabilityDescriptor(
	ctx context.Context,
	connectionID string,
	capability string,
) (CapabilityDescriptor, bool, error) {
	connection, err := e.ConnectionStore.Get(ctx, connectionID)
	if err != nil {
		return CapabilityDescriptor{}, false, err
	}
	provider, ok := e.Registry.Get(connection.ProviderID)
	if !ok {
		return CapabilityDescriptor{}, false,
			fmt.Errorf("core: provider %q not found for capability evaluation", connection.ProviderID)
	}
	descriptor, found := findCapabilityDescriptor(provider.Capabilities(), capability)
	return descriptor, found, nil
}

func (e *GrantPermissionEvaluator) grantedCapabilities(ctx context.Context, connectionID string) (map[string]struct{}, error) {
	granted := map[string]struct{}{}
	if e.GrantStore == nil {
		return granted, nil
	}
	snapshot, found, err := e.GrantStore.GetLatestSnapshot(ctx, connectionID)
	if err != nil || !found {
		return granted, err
	}
	for _, grant := range normalizeGrants(snapshot.Granted) {
		granted[grant] = struct{}{}
	}
	return granted, nil
}

func capabilityPermissionDecision(
	capability string,
	descriptor CapabilityDescriptor,
	granted map[string]struct{},
) PermissionDecision {

	missingRequired := missingGrants(descriptor.RequiredGrants, granted)
	if len(missingRequired) > 0 {
		return PermissionDecision{
			Allowed:       false,
			Capability:    capability,
			Reason:        "required grants are missing",
			MissingGrants: missingRequired,
			Mode:          CapabilityDeniedBehaviorBlock,
		}
	}

	missingOptional := missingGrants(descriptor.OptionalGrants, granted)
	if len(missingOptional) > 0 && descriptor.DeniedBehavior == CapabilityDeniedBehaviorDegrade {
		return PermissionDecision{
			Allowed:       true,
			Capability:    capability,
			Reason:        "optional grants missing; degrade behavior selected",
			MissingGrants: missingOptional,
			Mode:          CapabilityDeniedBehaviorDegrade,
		}
	}

	mode := descriptor.DeniedBehavior
	if mode == "" {
		mode = CapabilityDeniedBehaviorBlock
	}
	return PermissionDecision{
		Allowed:    true,
		Capability: capability,
		Mode:       mode,
	}
}

func missingGrants(required []string, granted map[string]struct{}) []string {
	if len(required) == 0 {
		return []string{}
	}
	normalized := normalizeGrants(required)
	missing := make([]string, 0, len(normalized))
	for _, grant := range normalized {
		if _, ok := granted[grant]; !ok {
			missing = append(missing, grant)
		}
	}
	sort.Strings(missing)
	return missing
}

var _ PermissionEvaluator = (*GrantPermissionEvaluator)(nil)
