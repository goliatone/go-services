package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestService_AdvanceAndLoadSyncCursor(t *testing.T) {
	store := newMemorySyncCursorStore()
	svc, err := NewService(Config{}, WithSyncCursorStore(store))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	syncedAt := time.Now().UTC()
	cursor, err := svc.AdvanceSyncCursor(context.Background(), AdvanceSyncCursorInput{
		ConnectionID: "conn_1",
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_1",
		Cursor:       "cursor_1",
		LastSyncedAt: &syncedAt,
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("advance cursor: %v", err)
	}
	if cursor.Cursor != "cursor_1" {
		t.Fatalf("expected cursor value to persist")
	}

	loaded, err := svc.LoadSyncCursor(context.Background(), "conn_1", "drive.file", "file_1")
	if err != nil {
		t.Fatalf("load cursor: %v", err)
	}
	if loaded.Cursor != "cursor_1" {
		t.Fatalf("expected loaded cursor to match persisted value")
	}
}

func TestService_AdvanceSyncCursorConflict(t *testing.T) {
	store := newMemorySyncCursorStore()
	svc, err := NewService(Config{}, WithSyncCursorStore(store))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if _, err := svc.AdvanceSyncCursor(context.Background(), AdvanceSyncCursorInput{
		ConnectionID: "conn_1",
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_1",
		Cursor:       "cursor_1",
		Status:       "active",
	}); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	_, err = svc.AdvanceSyncCursor(context.Background(), AdvanceSyncCursorInput{
		ConnectionID:   "conn_1",
		ProviderID:     "github",
		ResourceType:   "drive.file",
		ResourceID:     "file_1",
		ExpectedCursor: "stale",
		Cursor:         "cursor_2",
		Status:         "active",
	})
	if !errors.Is(err, ErrSyncCursorConflict) {
		t.Fatalf("expected sync cursor conflict error, got %v", err)
	}
}
