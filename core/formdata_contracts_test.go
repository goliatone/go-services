package core

import "testing"

func TestMappingSpecValidate(t *testing.T) {
	spec := MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "provider",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts -> crm_contacts",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
		Version:      1,
		Status:       MappingSpecStatusDraft,
		Rules: []MappingRule{
			{
				SourcePath: "email",
				TargetPath: "email",
			},
		},
	}

	if err := spec.Validate(); err != nil {
		t.Fatalf("expected valid mapping spec, got error: %v", err)
	}

	spec.Status = MappingSpecStatus("bad")
	if err := spec.Validate(); err == nil {
		t.Fatalf("expected invalid mapping status error")
	}
}

func TestSyncBindingValidate(t *testing.T) {
	binding := SyncBinding{
		ProviderID:    "provider",
		Scope:         ScopeRef{Type: "org", ID: "org_123"},
		ConnectionID:  "conn_1",
		MappingSpecID: "spec_1",
		SourceObject:  "contacts",
		TargetModel:   "crm_contacts",
		Direction:     SyncDirectionImport,
		Status:        SyncBindingStatusActive,
	}

	if err := binding.Validate(); err != nil {
		t.Fatalf("expected valid sync binding, got error: %v", err)
	}

	binding.Direction = SyncDirection("bad")
	if err := binding.Validate(); err == nil {
		t.Fatalf("expected invalid sync direction error")
	}
}

func TestSyncConflictResolutionActionIsValid(t *testing.T) {
	if !SyncConflictResolutionResolve.IsValid() {
		t.Fatalf("expected resolve action to be valid")
	}
	if SyncConflictResolutionAction("invalid").IsValid() {
		t.Fatalf("expected invalid action to be rejected")
	}
}
