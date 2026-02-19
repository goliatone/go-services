package core

import (
	"context"
	"testing"
	"time"
)

func TestMappingPreviewerPreviewMappingSpecDeterministic(t *testing.T) {
	previewer := NewMappingPreviewer(
		NewMappingCompiler(),
		WithMappingPreviewerClock(func() time.Time {
			return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
		}),
	)
	req := PreviewMappingSpecRequest{
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
					Transform:  "lowercase",
					Constraints: map[string]any{
						"target_type": "string",
					},
				},
				{
					ID:         "rule_age",
					SourcePath: "age",
					TargetPath: "age",
					Transform:  "to_int",
					Constraints: map[string]any{
						"target_type": "integer",
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
					},
				},
			},
		},
		Samples: []map[string]any{
			{"email": "USER@EXAMPLE.COM", "age": "41"},
		},
	}

	first, err := previewer.PreviewMappingSpec(context.Background(), req)
	if err != nil {
		t.Fatalf("preview mapping spec (first): %v", err)
	}
	second, err := previewer.PreviewMappingSpec(context.Background(), req)
	if err != nil {
		t.Fatalf("preview mapping spec (second): %v", err)
	}

	if first.DeterministicHash == "" {
		t.Fatalf("expected deterministic hash")
	}
	if first.DeterministicHash != second.DeterministicHash {
		t.Fatalf("expected deterministic hash, got %q and %q", first.DeterministicHash, second.DeterministicHash)
	}
	if len(first.Records) != 1 {
		t.Fatalf("expected one preview record, got %d", len(first.Records))
	}
	output := first.Records[0].Output
	if output["email"] != "user@example.com" {
		t.Fatalf("expected transformed email, got %#v", output["email"])
	}
	if output["age"] != int64(41) {
		t.Fatalf("expected transformed age int64(41), got %#v", output["age"])
	}
}

func TestMappingPreviewerPreviewMappingSpecDiffAndReport(t *testing.T) {
	previewer := NewMappingPreviewer(NewMappingCompiler())
	result, err := previewer.PreviewMappingSpec(context.Background(), PreviewMappingSpecRequest{
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
					TargetPath: "profile.email",
					Transform:  "lowercase",
					Constraints: map[string]any{
						"target_type": "string",
					},
				},
				{
					ID:         "rule_age",
					SourcePath: "age",
					TargetPath: "profile.age",
					Transform:  "to_int",
					Constraints: map[string]any{
						"target_type": "integer",
					},
				},
				{
					ID:         "rule_phone_missing",
					SourcePath: "phone",
					TargetPath: "profile.phone",
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
						{Path: "age", Type: "integer", Required: true},
					},
				},
			},
		},
		Samples: []map[string]any{
			{"email": "USER@EXAMPLE.COM", "age": "41"},
		},
	})
	if err != nil {
		t.Fatalf("preview mapping spec: %v", err)
	}

	if result.Report.SampleCount != 1 {
		t.Fatalf("expected sample_count=1, got %d", result.Report.SampleCount)
	}
	if result.Report.AppliedRuleCount != 2 {
		t.Fatalf("expected applied_rule_count=2, got %d", result.Report.AppliedRuleCount)
	}
	if result.Report.IssueCount != 2 {
		t.Fatalf("expected issue_count=2, got %d", result.Report.IssueCount)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected one record, got %d", len(result.Records))
	}
	if len(result.Records[0].Diff) != 2 {
		t.Fatalf("expected 2 diff entries, got %d", len(result.Records[0].Diff))
	}
	if len(result.Records[0].Issues) != 1 || result.Records[0].Issues[0].Code != "preview_source_missing" {
		t.Fatalf("expected one preview_source_missing issue, got %#v", result.Records[0].Issues)
	}
	if len(result.Issues) != 1 || result.Issues[0].Code != "source_field_not_found" {
		t.Fatalf("expected one source_field_not_found validation issue, got %#v", result.Issues)
	}
}
