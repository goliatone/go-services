package githubprojects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

const (
	discoveryCursorVersion = 1
	discoveryPhaseViewer   = "viewer"
	discoveryPhaseOrgs     = "organizations"
	maxDiscoveryCursorSize = 4096
	maxDiscoveryStateValue = 512
)

type discoveryCursor struct {
	Version           int    `json:"v"`
	Binding           string `json:"binding"`
	Phase             string `json:"phase"`
	ProjectAfter      string `json:"project_after,omitempty"`
	OrganizationAfter string `json:"organization_after,omitempty"`
	OrganizationID    string `json:"organization_id,omitempty"`
	OrganizationLogin string `json:"organization_login,omitempty"`
	MoreOrganizations bool   `json:"more_organizations,omitempty"`
}

type projectOwner struct {
	TypeName string `json:"__typename"`
	ID       string `json:"id"`
	Login    string `json:"login"`
}

type projectNode struct {
	ID        string       `json:"id"`
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	URL       string       `json:"url"`
	UpdatedAt time.Time    `json:"updatedAt"`
	Owner     projectOwner `json:"owner"`
}

type projectConnection struct {
	Nodes    []projectNode `json:"nodes"`
	PageInfo pageInfo      `json:"pageInfo"`
}

type organizationNode struct {
	ID       string            `json:"id"`
	Login    string            `json:"login"`
	Projects projectConnection `json:"projectsV2"`
}

func (p *Provider) DiscoverTrackerScopes(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerNodePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerNodePage{}, err
	}
	if strings.TrimSpace(input.Search) != "" {
		return core.TrackerNodePage{}, core.NewTrackerProviderError(core.TrackerErrorSearchUnsupported, "search is not supported for this discovery operation", false, 0, nil)
	}
	cursor, err := decodeDiscoveryCursor(input.Cursor, input.ConnectionID)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	limit := tracker.Limit(input.Limit, 50, 100)
	if cursor.Phase == discoveryPhaseViewer {
		return p.discoverViewerProjects(ctx, credential, endpoint, limit, cursor)
	}
	return p.discoverOrganizationProjects(ctx, credential, endpoint, limit, cursor)
}

func (p *Provider) discoverViewerProjects(ctx context.Context, credential core.ActiveCredential, endpoint string, limit int, cursor discoveryCursor) (core.TrackerNodePage, error) {
	query := `query($first:Int!,$after:String){viewer{id login projectsV2(first:$first,after:$after,minPermissionLevel:READ,orderBy:{field:NUMBER,direction:ASC}){nodes{id number title url updatedAt owner{__typename id ... on User{login}... on Organization{login}}}pageInfo{hasNextPage endCursor}}}}`
	var response tracker.GraphQLResponse[struct {
		Viewer struct {
			ID       string            `json:"id"`
			Login    string            `json:"login"`
			Projects projectConnection `json:"projectsV2"`
		} `json:"viewer"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"first": limit, "after": nullable(cursor.ProjectAfter)}, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	if err := graphQLErrors(response.Errors); err != nil {
		return core.TrackerNodePage{}, err
	}
	projects := response.Data.Viewer.Projects
	if err := validatePageInfo(projects.PageInfo); err != nil {
		return core.TrackerNodePage{}, err
	}
	items := projectNodes(projects.Nodes, projectOwner{TypeName: "User", ID: response.Data.Viewer.ID, Login: response.Data.Viewer.Login})
	if projects.PageInfo.HasNextPage {
		cursor.ProjectAfter = projects.PageInfo.EndCursor
		return discoveryPage(items, cursor, true), nil
	}
	cursor.Phase = discoveryPhaseOrgs
	cursor.ProjectAfter = ""
	if len(items) == 0 {
		return p.discoverOrganizationProjects(ctx, credential, endpoint, limit, cursor)
	}
	return discoveryPage(items, cursor, true), nil
}

func (p *Provider) discoverOrganizationProjects(ctx context.Context, credential core.ActiveCredential, endpoint string, limit int, cursor discoveryCursor) (core.TrackerNodePage, error) {
	if cursor.OrganizationID == "" {
		return p.discoverNextOrganization(ctx, credential, endpoint, limit, cursor)
	}
	query := `query($owner:ID!,$first:Int!,$after:String){node(id:$owner){... on Organization{id login projectsV2(first:$first,after:$after,minPermissionLevel:READ,orderBy:{field:NUMBER,direction:ASC}){nodes{id number title url updatedAt owner{__typename id ... on User{login}... on Organization{login}}}pageInfo{hasNextPage endCursor}}}}}`
	var response tracker.GraphQLResponse[struct {
		Node *organizationNode `json:"node"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"owner": cursor.OrganizationID, "first": limit, "after": nullable(cursor.ProjectAfter)}, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	if err := graphQLErrors(response.Errors); err != nil {
		return core.TrackerNodePage{}, err
	}
	if response.Data.Node == nil || strings.TrimSpace(response.Data.Node.ID) != cursor.OrganizationID {
		return core.TrackerNodePage{}, invalidDiscoveryCursor(errors.New("organization is no longer available"))
	}
	organization := *response.Data.Node
	if !validDiscoveryStateValue(organization.ID) || !validDiscoveryStateValue(organization.Login) {
		return core.TrackerNodePage{}, providerDiscoveryError("organization identity is incomplete")
	}
	cursor.OrganizationLogin = organization.Login
	return organizationProjectPage(organization.Projects, cursor)
}

func (p *Provider) discoverNextOrganization(ctx context.Context, credential core.ActiveCredential, endpoint string, limit int, cursor discoveryCursor) (core.TrackerNodePage, error) {
	query := `query($ownerAfter:String,$projectFirst:Int!){viewer{organizations(first:1,after:$ownerAfter,orderBy:{field:LOGIN,direction:ASC}){nodes{id login projectsV2(first:$projectFirst,minPermissionLevel:READ,orderBy:{field:NUMBER,direction:ASC}){nodes{id number title url updatedAt owner{__typename id ... on User{login}... on Organization{login}}}pageInfo{hasNextPage endCursor}}}pageInfo{hasNextPage endCursor}}}}`
	var response tracker.GraphQLResponse[struct {
		Viewer struct {
			Organizations struct {
				Nodes    []organizationNode `json:"nodes"`
				PageInfo pageInfo           `json:"pageInfo"`
			} `json:"organizations"`
		} `json:"viewer"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"ownerAfter": nullable(cursor.OrganizationAfter), "projectFirst": limit}, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	if err := graphQLErrors(response.Errors); err != nil {
		return core.TrackerNodePage{}, err
	}
	organizations := response.Data.Viewer.Organizations
	if err := validatePageInfo(organizations.PageInfo); err != nil {
		return core.TrackerNodePage{}, err
	}
	if len(organizations.Nodes) == 0 {
		if organizations.PageInfo.HasNextPage {
			return core.TrackerNodePage{}, providerDiscoveryError("organization page has a continuation without an owner")
		}
		return discoveryPage(nil, discoveryCursor{}, false), nil
	}
	if len(organizations.Nodes) != 1 {
		return core.TrackerNodePage{}, providerDiscoveryError("organization page exceeded its requested bound")
	}
	organization := organizations.Nodes[0]
	if !validDiscoveryStateValue(organization.ID) || !validDiscoveryStateValue(organization.Login) {
		return core.TrackerNodePage{}, providerDiscoveryError("organization identity is incomplete")
	}
	cursor.OrganizationID = organization.ID
	cursor.OrganizationLogin = organization.Login
	cursor.OrganizationAfter = organizations.PageInfo.EndCursor
	cursor.MoreOrganizations = organizations.PageInfo.HasNextPage
	return organizationProjectPage(organization.Projects, cursor)
}

func organizationProjectPage(projects projectConnection, cursor discoveryCursor) (core.TrackerNodePage, error) {
	if err := validatePageInfo(projects.PageInfo); err != nil {
		return core.TrackerNodePage{}, err
	}
	items := projectNodes(projects.Nodes, projectOwner{TypeName: "Organization", ID: cursor.OrganizationID, Login: cursor.OrganizationLogin})
	if projects.PageInfo.HasNextPage {
		cursor.ProjectAfter = projects.PageInfo.EndCursor
		return discoveryPage(items, cursor, true), nil
	}
	if cursor.MoreOrganizations {
		cursor.OrganizationID = ""
		cursor.OrganizationLogin = ""
		cursor.ProjectAfter = ""
		cursor.MoreOrganizations = false
		return discoveryPage(items, cursor, true), nil
	}
	return discoveryPage(items, discoveryCursor{}, false), nil
}

func projectNodes(projects []projectNode, fallback projectOwner) []core.TrackerNode {
	items := make([]core.TrackerNode, 0, len(projects))
	for _, project := range projects {
		owner := project.Owner
		if strings.TrimSpace(owner.ID) == "" {
			owner.ID = fallback.ID
		}
		if strings.TrimSpace(owner.Login) == "" {
			owner.Login = fallback.Login
		}
		if strings.TrimSpace(owner.TypeName) == "" {
			owner.TypeName = fallback.TypeName
		}
		ownerType := strings.ToLower(strings.TrimSpace(owner.TypeName))
		items = append(items, core.TrackerNode{
			ExternalID: project.ID,
			Type:       "project_v2",
			Name:       project.Title,
			ParentID:   owner.ID,
			Capabilities: []string{
				"items.read",
				"schema.read",
			},
			NativeRevision: project.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Metadata: map[string]any{
				"number":        project.Number,
				"canonical_url": project.URL,
				"owner":         owner.Login,
				"owner_id":      owner.ID,
				"owner_type":    ownerType,
			},
		})
	}
	return items
}

func discoveryPage(items []core.TrackerNode, cursor discoveryCursor, hasMore bool) core.TrackerNodePage {
	next := ""
	if hasMore {
		next = encodeDiscoveryCursor(cursor)
	}
	return core.TrackerNodePage{Items: items, NextCursor: next, HasMore: hasMore, Revision: tracker.Revision(items)}
}

func decodeDiscoveryCursor(value, connectionID string) (discoveryCursor, error) {
	binding := discoveryBinding(connectionID)
	if strings.TrimSpace(value) == "" {
		return discoveryCursor{Version: discoveryCursorVersion, Binding: binding, Phase: discoveryPhaseViewer}, nil
	}
	cursor, err := parseDiscoveryCursor(value)
	if err != nil {
		return discoveryCursor{}, invalidDiscoveryCursor(err)
	}
	if err := validateDiscoveryCursor(cursor, binding); err != nil {
		return discoveryCursor{}, invalidDiscoveryCursor(err)
	}
	return cursor, nil
}

func parseDiscoveryCursor(value string) (discoveryCursor, error) {
	if len(value) > maxDiscoveryCursorSize {
		return discoveryCursor{}, errors.New("cursor exceeds size bound")
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return discoveryCursor{}, err
	}
	if len(payload) == 0 || len(payload) > maxDiscoveryCursorSize {
		return discoveryCursor{}, errors.New("cursor payload size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor discoveryCursor
	if err := decoder.Decode(&cursor); err != nil {
		return discoveryCursor{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return discoveryCursor{}, err
	}
	return cursor, nil
}

func validateDiscoveryCursor(cursor discoveryCursor, binding string) error {
	if cursor.Version != discoveryCursorVersion || cursor.Binding != binding {
		return errors.New("cursor binding is invalid")
	}
	if cursor.Phase != discoveryPhaseViewer && cursor.Phase != discoveryPhaseOrgs {
		return errors.New("cursor phase is invalid")
	}
	if err := validateDiscoveryCursorStateValues(cursor); err != nil {
		return err
	}
	if cursor.Phase == discoveryPhaseViewer {
		return validateViewerDiscoveryCursor(cursor)
	}
	return validateOrganizationDiscoveryCursor(cursor)
}

func validateDiscoveryCursorStateValues(cursor discoveryCursor) error {
	for _, item := range []string{cursor.ProjectAfter, cursor.OrganizationAfter, cursor.OrganizationID, cursor.OrganizationLogin} {
		if item != "" && !validDiscoveryStateValue(item) {
			return errors.New("cursor state is invalid")
		}
	}
	return nil
}

func validateViewerDiscoveryCursor(cursor discoveryCursor) error {
	if cursor.OrganizationAfter != "" || cursor.OrganizationID != "" || cursor.OrganizationLogin != "" || cursor.MoreOrganizations {
		return errors.New("viewer cursor contains organization state")
	}
	return nil
}

func validateOrganizationDiscoveryCursor(cursor discoveryCursor) error {
	if (cursor.OrganizationID == "") != (cursor.OrganizationLogin == "") {
		return errors.New("organization cursor identity is incomplete")
	}
	if cursor.OrganizationID == "" && (cursor.ProjectAfter != "" || cursor.MoreOrganizations) {
		return errors.New("organization cursor has no active owner")
	}
	return nil
}

func encodeDiscoveryCursor(cursor discoveryCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func discoveryBinding(connectionID string) string {
	digest := sha256.Sum256([]byte("github-projects.discovery.v1\x00" + strings.TrimSpace(connectionID)))
	return hex.EncodeToString(digest[:16])
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("cursor contains trailing data")
	}
	return err
}

func validatePageInfo(page pageInfo) error {
	if page.HasNextPage && strings.TrimSpace(page.EndCursor) == "" {
		return providerDiscoveryError("provider page has more results without a cursor")
	}
	if page.EndCursor != "" && !validDiscoveryStateValue(page.EndCursor) {
		return providerDiscoveryError("provider page cursor is invalid")
	}
	return nil
}

func validDiscoveryStateValue(value string) bool {
	return value != "" && len(value) <= maxDiscoveryStateValue && strings.TrimSpace(value) == value
}

func invalidDiscoveryCursor(cause error) error {
	return core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, "GitHub Projects discovery continuation is invalid; restart discovery", false, 0, cause)
}

func providerDiscoveryError(message string) error {
	return core.NewTrackerProviderError(core.TrackerErrorExternal, message, false, 0, nil)
}
