package core

import (
	"context"
	"slices"
	"testing"
)

func TestMappingCompilerValidateMappingSpecDeterministicHash(t *testing.T) {
	compiler := NewMappingCompiler()
	req := ValidateMappingSpecRequest{
		Spec: MappingSpec{
			SpecID:       "spec_contacts",
			ProviderID:   "hubspot",
			Scope:        ScopeRef{Type: "org", ID: "org_123"},
			Name:         "contacts",
			SourceObject: "contacts",
			TargetModel:  "crm_contacts",
			Version:      1,
			Status:       MappingSpecStatusDraft,
			Rules: []MappingRule{
				{
					ID:         "rule_email",
					SourcePath: "email",
					TargetPath: "email",
					Transform:  "identity",
					Constraints: map[string]any{
						"target_type": "string",
					},
				},
			},
		},
		Schema: ExternalSchema{
			ProviderID: "hubspot",
			Scope:      ScopeRef{Type: "org", ID: "org_123"},
			Name:       "contacts_schema",
			Objects: []ExternalObjectSchema{
				{
					Name: "contacts",
					Fields: []ExternalField{
						{Path: "email", Type: "string", Required: true},
					},
				},
			},
		},
	}

	first, err := compiler.ValidateMappingSpec(context.Background(), req)
	if err != nil {
		t.Fatalf("validate mapping spec (first): %v", err)
	}
	second, err := compiler.ValidateMappingSpec(context.Background(), req)
	if err != nil {
		t.Fatalf("validate mapping spec (second): %v", err)
	}

	if !first.Valid {
		t.Fatalf("expected valid result, got issues: %#v", first.Issues)
	}
	if len(first.Issues) != 0 {
		t.Fatalf("expected no issues, got %d", len(first.Issues))
	}
	if first.Compiled.DeterministicHash == "" {
		t.Fatalf("expected deterministic hash")
	}
	if first.Compiled.DeterministicHash != second.Compiled.DeterministicHash {
		t.Fatalf("expected deterministic hash, got %q and %q", first.Compiled.DeterministicHash, second.Compiled.DeterministicHash)
	}
}

func TestMappingCompilerValidateMappingSpecIssueOrderingAndCoverage(t *testing.T) {
	compiler := NewMappingCompiler()
	result, err := compiler.ValidateMappingSpec(context.Background(), ValidateMappingSpecRequest{
		Spec: MappingSpec{
			SpecID:       "spec_contacts",
			ProviderID:   "hubspot",
			Scope:        ScopeRef{Type: "org", ID: "org_123"},
			Name:         "contacts",
			SourceObject: "contacts",
			TargetModel:  "crm_contacts",
			Version:      1,
			Status:       MappingSpecStatusDraft,
			Rules: []MappingRule{
				{
					ID:         "rule_email",
					SourcePath: "email",
					TargetPath: "email",
					Transform:  "identity",
					Constraints: map[string]any{
						"target_type": "string",
					},
				},
				{
					ID:         "rule_age_bad_type",
					SourcePath: "age",
					TargetPath: "age_label",
					Transform:  "identity",
					Constraints: map[string]any{
						"target_type": "string",
					},
				},
				{
					ID:         "rule_alias_missing_source",
					SourcePath: "alias",
					TargetPath: "alias",
					Transform:  "lowercase",
					Constraints: map[string]any{
						"target_type": "string",
					},
				},
				{
					ID:         "rule_unknown_transform",
					SourcePath: "email",
					TargetPath: "email_alt",
					Transform:  "warp",
					Constraints: map[string]any{
						"target_type": "string",
					},
				},
			},
		},
		Schema: ExternalSchema{
			ProviderID: "hubspot",
			Scope:      ScopeRef{Type: "org", ID: "org_123"},
			Name:       "contacts_schema",
			Objects: []ExternalObjectSchema{
				{
					Name: "contacts",
					Fields: []ExternalField{
						{Path: "email", Type: "string", Required: true},
						{Path: "age", Type: "integer", Required: true},
						{Path: "phone", Type: "string", Required: true},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("validate mapping spec: %v", err)
	}

	if result.Valid {
		t.Fatalf("expected invalid result")
	}
	codes := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		codes = append(codes, issue.Code)
	}
	expectedCodes := []string{
		"required_field_unmapped",
		"source_field_not_found",
		"transform_unknown",
		"type_incompatible",
	}
	if !slices.Equal(codes, expectedCodes) {
		t.Fatalf("expected issue codes %v, got %v", expectedCodes, codes)
	}
}

func TestMappingCompilerValidateMappingSpecSchemaDriftDetection(t *testing.T) {
	compiler := NewMappingCompiler()
	result, err := compiler.ValidateMappingSpec(context.Background(), ValidateMappingSpecRequest{
		Spec: MappingSpec{
			SpecID:       "spec_contacts",
			ProviderID:   "hubspot",
			Scope:        ScopeRef{Type: "org", ID: "org_123"},
			Name:         "contacts",
			SchemaRef:    "contacts_schema@v1",
			SourceObject: "contacts",
			TargetModel:  "crm_contacts",
			Version:      1,
			Status:       MappingSpecStatusDraft,
			Rules: []MappingRule{
				{
					ID:         "rule_email",
					SourcePath: "email",
					TargetPath: "email",
					Transform:  "identity",
					Constraints: map[string]any{
						"target_type": "string",
					},
				},
			},
		},
		Schema: ExternalSchema{
			ProviderID: "hubspot",
			Scope:      ScopeRef{Type: "org", ID: "org_123"},
			Name:       "contacts_schema",
			Version:    "v2",
			Objects: []ExternalObjectSchema{
				{
					Name: "contacts",
					Fields: []ExternalField{
						{Path: "email", Type: "string", Required: true},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("validate mapping spec: %v", err)
	}

	if len(result.Issues) != 1 {
		t.Fatalf("expected one schema drift issue, got %#v", result.Issues)
	}
	if result.Issues[0].Code != "schema_drift_detected" {
		t.Fatalf("expected schema_drift_detected issue code, got %q", result.Issues[0].Code)
	}
	if result.Issues[0].Severity != MappingValidationIssueWarning {
		t.Fatalf("expected warning severity, got %s", result.Issues[0].Severity)
	}
}
