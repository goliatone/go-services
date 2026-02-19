package core

import (
	"context"
	"fmt"
	"testing"
)

type inMemoryIdentityBindingStore struct {
	byExternal map[string]IdentityBinding
}

func newInMemoryIdentityBindingStore() *inMemoryIdentityBindingStore {
	return &inMemoryIdentityBindingStore{
		byExternal: make(map[string]IdentityBinding),
	}
}

func (s *inMemoryIdentityBindingStore) Upsert(
	ctx context.Context,
	binding IdentityBinding,
) (IdentityBinding, error) {
	key := fmt.Sprintf("%s::%s", binding.SyncBindingID, binding.ExternalID)
	s.byExternal[key] = binding
	return binding, nil
}

func (s *inMemoryIdentityBindingStore) GetByExternalID(
	ctx context.Context,
	syncBindingID string,
	externalID string,
) (IdentityBinding, bool, error) {
	key := fmt.Sprintf("%s::%s", syncBindingID, externalID)
	binding, ok := s.byExternal[key]
	return binding, ok, nil
}

func (s *inMemoryIdentityBindingStore) ListByInternalID(
	ctx context.Context,
	syncBindingID string,
	internalType string,
	internalID string,
) ([]IdentityBinding, error) {
	out := make([]IdentityBinding, 0)
	for _, binding := range s.byExternal {
		if binding.SyncBindingID == syncBindingID &&
			binding.InternalType == internalType &&
			binding.InternalID == internalID {
			out = append(out, binding)
		}
	}
	return out, nil
}

func TestIdentityBindingReconcilerConfidentAndExisting(t *testing.T) {
	store := newInMemoryIdentityBindingStore()
	reconciler, err := NewIdentityBindingReconciler(store)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}

	first, err := reconciler.ReconcileIdentity(context.Background(), ReconcileIdentityRequest{
		ProviderID:    "hubspot",
		Scope:         ScopeRef{Type: "org", ID: "org_123"},
		ConnectionID:  "conn_1",
		SyncBindingID: "sync_binding_1",
		SourceObject:  "contacts",
		ExternalID:    "ext_1",
		Candidates: []IdentityCandidate{
			{InternalType: "contact", InternalID: "contact_1", Confidence: 0.92},
			{InternalType: "contact", InternalID: "contact_2", Confidence: 0.40},
		},
	})
	if err != nil {
		t.Fatalf("reconcile first: %v", err)
	}
	if !first.Created {
		t.Fatalf("expected first reconcile to create binding")
	}
	if first.Binding.MatchKind != IdentityBindingMatchConfident {
		t.Fatalf("expected confident match, got %s", first.Binding.MatchKind)
	}
	if first.Binding.InternalID != "contact_1" || first.Binding.Confidence != 0.92 {
		t.Fatalf("unexpected winning candidate binding: %#v", first.Binding)
	}

	second, err := reconciler.ReconcileIdentity(context.Background(), ReconcileIdentityRequest{
		ProviderID:    "hubspot",
		Scope:         ScopeRef{Type: "org", ID: "org_123"},
		ConnectionID:  "conn_1",
		SyncBindingID: "sync_binding_1",
		SourceObject:  "contacts",
		ExternalID:    "ext_1",
		Candidates: []IdentityCandidate{
			{InternalType: "contact", InternalID: "contact_9", Confidence: 1},
		},
	})
	if err != nil {
		t.Fatalf("reconcile second: %v", err)
	}
	if second.Created {
		t.Fatalf("expected second reconcile to return existing binding")
	}
	if second.Binding.InternalID != "contact_1" {
		t.Fatalf("expected existing binding to remain unchanged, got %#v", second.Binding)
	}
}

func TestIdentityBindingReconcilerAmbiguousAndUnresolved(t *testing.T) {
	store := newInMemoryIdentityBindingStore()
	reconciler, err := NewIdentityBindingReconciler(store)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}

	ambiguous, err := reconciler.ReconcileIdentity(context.Background(), ReconcileIdentityRequest{
		ProviderID:    "hubspot",
		Scope:         ScopeRef{Type: "org", ID: "org_123"},
		ConnectionID:  "conn_1",
		SyncBindingID: "sync_binding_2",
		SourceObject:  "contacts",
		ExternalID:    "ext_2",
		Candidates: []IdentityCandidate{
			{InternalType: "contact", InternalID: "contact_1", Confidence: 0.91},
			{InternalType: "contact", InternalID: "contact_2", Confidence: 0.89},
		},
	})
	if err != nil {
		t.Fatalf("reconcile ambiguous: %v", err)
	}
	if ambiguous.Binding.MatchKind != IdentityBindingMatchAmbiguous {
		t.Fatalf("expected ambiguous match, got %s", ambiguous.Binding.MatchKind)
	}
	if ambiguous.Binding.InternalID != "" {
		t.Fatalf("expected ambiguous binding to omit internal id, got %#v", ambiguous.Binding)
	}

	unresolved, err := reconciler.ReconcileIdentity(context.Background(), ReconcileIdentityRequest{
		ProviderID:    "hubspot",
		Scope:         ScopeRef{Type: "org", ID: "org_123"},
		ConnectionID:  "conn_1",
		SyncBindingID: "sync_binding_3",
		SourceObject:  "contacts",
		ExternalID:    "ext_3",
		Candidates:    nil,
	})
	if err != nil {
		t.Fatalf("reconcile unresolved: %v", err)
	}
	if unresolved.Binding.MatchKind != IdentityBindingMatchUnresolved {
		t.Fatalf("expected unresolved match, got %s", unresolved.Binding.MatchKind)
	}
	if unresolved.Binding.Confidence != 0 {
		t.Fatalf("expected unresolved confidence=0, got %f", unresolved.Binding.Confidence)
	}
}
