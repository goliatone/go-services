package core

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	GrantEventExpanded           = "expanded"
	GrantEventDowngraded         = "downgraded"
	GrantEventRevoked            = "revoked"
	GrantEventReconsentRequested = "reconsent_requested"
	GrantEventReconsentCompleted = "reconsent_completed"
)

type GrantDelta struct {
	EventType string
	Added     []string
	Removed   []string
}

func ComputeGrantDelta(previous, current []string) GrantDelta {
	prevSet := toGrantSet(previous)
	currSet := toGrantSet(current)

	added := make([]string, 0, len(currSet))
	removed := make([]string, 0, len(prevSet))
	for grant := range currSet {
		if _, ok := prevSet[grant]; !ok {
			added = append(added, grant)
		}
	}
	for grant := range prevSet {
		if _, ok := currSet[grant]; !ok {
			removed = append(removed, grant)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)

	eventType := ""
	switch {
	case len(removed) > 0 && len(currSet) == 0:
		eventType = GrantEventRevoked
	case len(removed) > 0:
		eventType = GrantEventDowngraded
	case len(added) > 0:
		eventType = GrantEventExpanded
	}
	return GrantDelta{
		EventType: eventType,
		Added:     added,
		Removed:   removed,
	}
}

func normalizeGrants(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	set := toGrantSet(values)
	out := make([]string, 0, len(set))
	for grant := range set {
		out = append(out, grant)
	}
	sort.Strings(out)
	return out
}

func toGrantSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(strings.ToLower(value))
		if trimmed == "" {
			continue
		}
		set[trimmed] = struct{}{}
	}
	return set
}

func (s *Service) reconcileGrantSnapshot(
	ctx context.Context,
	provider Provider,
	connectionID string,
	requested []string,
	granted []string,
	metadata map[string]any,
) (GrantSnapshot, GrantDelta, error) {
	if s == nil || s.grantStore == nil {
		return GrantSnapshot{}, GrantDelta{}, nil
	}

	requested = normalizeGrants(requested)
	granted = normalizeGrants(granted)
	if awareProvider, ok := provider.(GrantAwareProvider); ok {
		normalized, err := awareProvider.NormalizeGrantedPermissions(ctx, granted)
		if err != nil {
			return GrantSnapshot{}, GrantDelta{}, err
		}
		granted = normalizeGrants(normalized)
	}

	previous, hasPrevious, err := s.grantStore.GetLatestSnapshot(ctx, connectionID)
	if err != nil {
		return GrantSnapshot{}, GrantDelta{}, err
	}
	version := 1
	if hasPrevious {
		version = previous.Version + 1
	}

	now := time.Now().UTC()
	snapshotInput := SaveGrantSnapshotInput{
		ConnectionID: connectionID,
		Version:      version,
		Requested:    requested,
		Granted:      granted,
		CapturedAt:   now,
		Metadata:     copyAnyMap(metadata),
	}

	delta := ComputeGrantDelta(nil, granted)
	if hasPrevious {
		delta = ComputeGrantDelta(previous.Granted, granted)
	}
	var eventInput *AppendGrantEventInput
	if delta.EventType != "" {
		eventInput = &AppendGrantEventInput{
			ConnectionID: connectionID,
			EventType:    delta.EventType,
			Added:        delta.Added,
			Removed:      delta.Removed,
			OccurredAt:   now,
			Metadata:     copyAnyMap(metadata),
		}
	}

	if transactionalStore, ok := s.grantStore.(GrantStoreTransactional); ok {
		if saveErr := transactionalStore.SaveSnapshotAndEvent(ctx, snapshotInput, eventInput); saveErr != nil {
			return GrantSnapshot{}, GrantDelta{}, saveErr
		}
	} else {
		if saveErr := s.grantStore.SaveSnapshot(ctx, snapshotInput); saveErr != nil {
			return GrantSnapshot{}, GrantDelta{}, saveErr
		}
		if eventInput != nil {
			if appendErr := s.grantStore.AppendEvent(ctx, *eventInput); appendErr != nil {
				return GrantSnapshot{}, GrantDelta{}, appendErr
			}
		}
	}

	return GrantSnapshot{
		ConnectionID: connectionID,
		Version:      version,
		Requested:    requested,
		Granted:      granted,
		CapturedAt:   now,
		Metadata:     copyAnyMap(metadata),
	}, delta, nil
}

func missingRequiredProviderGrants(capabilities []CapabilityDescriptor, granted []string) []string {
	requiredSet := map[string]struct{}{}
	for _, descriptor := range capabilities {
		for _, grant := range normalizeGrants(descriptor.RequiredGrants) {
			requiredSet[grant] = struct{}{}
		}
	}
	if len(requiredSet) == 0 {
		return []string{}
	}

	grantedSet := toGrantSet(granted)
	missing := make([]string, 0, len(requiredSet))
	for grant := range requiredSet {
		if _, ok := grantedSet[grant]; !ok {
			missing = append(missing, grant)
		}
	}
	sort.Strings(missing)
	return missing
}
