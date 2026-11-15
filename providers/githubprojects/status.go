package githubprojects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

// DiscoverTrackerProjectStatus returns the complete native Status options and a content-bound schema revision.
func (p *Provider) DiscoverTrackerProjectStatus(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerSchemaPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	if input.ResourceID == "" || input.Cursor != "" || input.Search != "" {
		return core.TrackerSchemaPage{}, statusError(core.TrackerErrorValidation)
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerSchemaPage{}, err
	}
	var response tracker.GraphQLResponse[struct {
		Node struct {
			ID              string
			ViewerCanUpdate bool `json:"viewerCanUpdate"`
			Field           struct {
				ID, Name, DataType string
				Options            []struct{ ID, Name string }
			}
		}
	}]
	query := `query($id:ID!){node(id:$id){... on ProjectV2{id viewerCanUpdate field(name:"Status"){... on ProjectV2SingleSelectField{id name dataType options{id name}}}}}}`
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"id": input.ResourceID}, &response); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	if len(response.Errors) != 0 {
		return core.TrackerSchemaPage{}, statusError(core.TrackerErrorExternal)
	}
	node := response.Data.Node
	if node.ID != input.ResourceID {
		return core.TrackerSchemaPage{}, statusError(core.TrackerErrorNotFound)
	}
	if node.Field.ID == "" || node.Field.Name != "Status" || node.Field.DataType != "SINGLE_SELECT" {
		return core.TrackerSchemaPage{}, statusError(core.TrackerErrorSchemaChanged)
	}
	raw, _ := json.Marshal(node.Field)
	digest := sha256.Sum256(append([]byte(node.ID+":"), raw...))
	revision := "github-project-status:" + hex.EncodeToString(digest[:])
	field := core.TrackerField{ID: node.Field.ID, Name: node.Field.Name, Type: "single_select", NativeRevision: revision}
	for _, option := range node.Field.Options {
		field.Options = append(field.Options, core.TrackerNode{ExternalID: option.ID, Name: option.Name, Type: "option", NativeRevision: revision})
	}
	result := core.TrackerSchemaPage{Items: []core.TrackerField{field}, Revision: revision, Metadata: map[string]any{"can_update": node.ViewerCanUpdate && slices.Contains(credential.GrantedScopes, "project"), "revision_guard": "preflight"}}
	return result, result.Validate()
}

type statusItem struct {
	ID, Type  string
	UpdatedAt time.Time `json:"updatedAt"`
	Project   struct{ ID string }
	Content   struct {
		ID, URL   string
		UpdatedAt time.Time `json:"updatedAt"`
	}
	FieldValueByName struct {
		OptionID string `json:"optionId"`
		Field    struct{ ID string }
	} `json:"fieldValueByName"`
}

func (v statusItem) revision() string {
	revision := v.UpdatedAt
	if v.Content.UpdatedAt.After(revision) {
		revision = v.Content.UpdatedAt
	}
	if revision.IsZero() {
		return ""
	}
	return revision.UTC().Format(time.RFC3339Nano)
}

const statusItemFields = `id type updatedAt project{id} content{... on Issue{id url updatedAt}} fieldValueByName(name:"Status"){... on ProjectV2ItemFieldSingleSelectValue{optionId field{... on ProjectV2FieldCommon{id}}}}`

// UpdateTrackerProjectStatus uses the shared update envelope: RepositoryID is the Project ID, IssueID the item ID, Field the Status field ID, and Value a native option ID.
func (p *Provider) UpdateTrackerProjectStatus(ctx context.Context, input core.TrackerIssueUpdateRequest, schemaRevision string) (core.TrackerMutationReceipt, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	var optionID string
	if json.Unmarshal(input.Value, &optionID) != nil || strings.TrimSpace(optionID) == "" || schemaRevision == "" || input.IssueNumber != 0 {
		return core.TrackerMutationReceipt{}, statusError(core.TrackerErrorValidation)
	}
	schema, err := p.DiscoverTrackerProjectStatus(ctx, core.TrackerDiscoveryRequest{ConnectionID: input.ConnectionID, ResourceID: input.RepositoryID})
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if schema.Revision != schemaRevision || schema.Items[0].ID != input.Field {
		return core.TrackerMutationReceipt{}, statusError(core.TrackerErrorSchemaChanged)
	}
	if schema.Metadata["can_update"] != true {
		return core.TrackerMutationReceipt{}, statusError(core.TrackerErrorPermission)
	}
	valid := false
	for _, option := range schema.Items[0].Options {
		valid = valid || option.ExternalID == optionID
	}
	if !valid {
		return core.TrackerMutationReceipt{}, statusError(core.TrackerErrorValidation)
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	var before tracker.GraphQLResponse[struct{ Node statusItem }]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, `query($id:ID!){node(id:$id){... on ProjectV2Item{`+statusItemFields+`}}}`, map[string]any{"id": input.IssueID}, &before); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if len(before.Errors) != 0 {
		return core.TrackerMutationReceipt{}, statusError(core.TrackerErrorExternal)
	}
	item := before.Data.Node
	if item.ID != input.IssueID || item.Project.ID != input.RepositoryID || item.Type != "ISSUE" || item.Content.ID == "" || !strings.HasPrefix(item.Content.URL, "https://") {
		return core.TrackerMutationReceipt{}, statusError(core.TrackerErrorValidation)
	}
	if item.revision() != input.ExpectedRevision {
		return core.TrackerMutationReceipt{}, statusError(core.TrackerErrorConflict)
	}
	receipt := core.TrackerMutationReceipt{ProviderID: ProviderID, RepositoryID: input.RepositoryID, ExternalID: input.IssueID, DisplayID: input.IssueID, CanonicalURL: item.Content.URL, Status: "unknown", CorrelationID: input.CorrelationID, CausationID: input.CausationID}
	var result tracker.GraphQLResponse[struct {
		UpdateProjectV2ItemFieldValue struct{ ProjectV2Item statusItem }
	}]
	query := `mutation($input:UpdateProjectV2ItemFieldValueInput!){updateProjectV2ItemFieldValue(input:$input){projectV2Item{` + statusItemFields + `}}}`
	variables := map[string]any{"input": map[string]any{"projectId": input.RepositoryID, "itemId": input.IssueID, "fieldId": input.Field, "value": map[string]any{"singleSelectOptionId": optionID}, "clientMutationId": input.IdempotencyKey}}
	// After dispatch, ambiguous failures require reconciliation rather than replay.
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, variables, &result); err != nil || len(result.Errors) != 0 {
		return receipt, nil
	}
	after := result.Data.UpdateProjectV2ItemFieldValue.ProjectV2Item
	if after.ID != item.ID || after.Project.ID != item.Project.ID || after.Type != "ISSUE" || after.Content.ID != item.Content.ID || after.revision() == "" || after.FieldValueByName.Field.ID != input.Field || after.FieldValueByName.OptionID != optionID {
		return receipt, nil
	}
	receipt.Status, receipt.ProviderRevision = "succeeded", after.revision()
	return receipt, nil
}
func statusError(code string) error {
	return core.NewTrackerProviderError(code, "GitHub Project Status operation refused: "+code, false, 0, nil)
}
