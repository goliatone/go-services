package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrSyncCursorConflict = errors.New("core: sync cursor advance conflict")

func (s *Service) LoadSyncCursor(
	ctx context.Context,
	connectionID string,
	resourceType string,
	resourceID string,
) (SyncCursor, error) {
	if s == nil || s.syncCursorStore == nil {
		return SyncCursor{}, s.mapError(fmt.Errorf("core: sync cursor store is required"))
	}
	return s.syncCursorStore.Get(
		ctx,
		strings.TrimSpace(connectionID),
		strings.TrimSpace(resourceType),
		strings.TrimSpace(resourceID),
	)
}

func (s *Service) AdvanceSyncCursor(ctx context.Context, in AdvanceSyncCursorInput) (SyncCursor, error) {
	if s == nil || s.syncCursorStore == nil {
		return SyncCursor{}, s.mapError(fmt.Errorf("core: sync cursor store is required"))
	}
	in.ConnectionID = strings.TrimSpace(in.ConnectionID)
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.ResourceType = strings.TrimSpace(in.ResourceType)
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	in.ExpectedCursor = strings.TrimSpace(in.ExpectedCursor)
	in.Cursor = strings.TrimSpace(in.Cursor)
	in.Status = strings.TrimSpace(in.Status)
	if in.ConnectionID == "" || in.ProviderID == "" {
		return SyncCursor{}, s.mapError(fmt.Errorf("core: connection id and provider id are required"))
	}
	if in.ResourceType == "" || in.ResourceID == "" {
		return SyncCursor{}, s.mapError(fmt.Errorf("core: resource type and resource id are required"))
	}
	if in.Cursor == "" {
		return SyncCursor{}, s.mapError(fmt.Errorf("core: cursor is required"))
	}

	cursor, err := s.syncCursorStore.Advance(ctx, in)
	if err != nil {
		if errors.Is(err, ErrSyncCursorConflict) {
			return SyncCursor{}, s.mapError(err)
		}
		return SyncCursor{}, s.mapError(err)
	}
	return cursor, nil
}
