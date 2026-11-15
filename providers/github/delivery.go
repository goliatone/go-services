package github

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

var deliveryRepositoryPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var deliveryCommit = regexp.MustCompile(`^([a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)

func (p *Provider) ReadTrackerDeliveryEvidence(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerResource, error) {
	invalid := func() (core.TrackerResource, error) {
		return core.TrackerResource{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Invalid pull request evidence identity", false, 0, nil)
	}
	parts := strings.Split(input.ResourceID, "/")
	if input.Validate() != nil || input.ResourceType != "pull_request" || input.Cursor != "" || input.Search != "" || len(parts) != 4 || parts[2] != "pull" || !deliveryRepositoryPart.MatchString(parts[0]) || !deliveryRepositoryPart.MatchString(parts[1]) || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return invalid()
	}
	number, err := strconv.ParseInt(parts[3], 10, 32)
	if err != nil || number < 1 || strconv.FormatInt(number, 10) != parts[3] {
		return invalid()
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerResource{}, err
	}
	if strings.HasSuffix(endpoint.Path, "/api/v3") {
		endpoint.Path = strings.TrimSuffix(endpoint.Path, "/v3") + "/graphql"
	} else {
		endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/graphql"
	}
	var response struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ID                string
					URL               string
					Number            int64
					HeadRefOID        string `json:"headRefOid"`
					Merged            bool
					MergeCommit       *struct{ OID string }
					StatusCheckRollup *struct {
						State  string
						Commit struct{ OID string }
					}
				}
			}
		}
		Errors []struct{ Type string }
	}
	query := `query($owner:String!,$repo:String!,$number:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$number){id url number headRefOid merged mergeCommit{oid} statusCheckRollup{state commit{oid}}}}}`
	metadata, err := p.runtime.DoGraphQLWithMetadata(ctx, credential, endpoint.String(), query, map[string]any{"owner": parts[0], "repo": parts[1], "number": number}, &response)
	if err != nil {
		return core.TrackerResource{}, err
	}
	if len(response.Errors) > 0 {
		code := core.TrackerErrorExternal
		for _, e := range response.Errors {
			if e.Type == "RATE_LIMITED" {
				delay := metadata.RetryAfter
				if metadata.RateLimitRemaining == 0 {
					if resetDelay := time.Until(metadata.RateLimitReset); resetDelay > delay {
						delay = resetDelay
					}
				}
				if delay <= 0 {
					delay = time.Minute
				}
				return core.TrackerResource{}, core.NewTrackerProviderError(core.TrackerErrorRateLimited, "Pull request evidence is rate limited", true, delay, nil)
			}

			if e.Type == "FORBIDDEN" {
				code = core.TrackerErrorPermission
			}
			if e.Type == "NOT_FOUND" {
				code = core.TrackerErrorNotFound
			}
		}
		return core.TrackerResource{}, core.NewTrackerProviderError(code, "Pull request evidence could not be read", false, 0, nil)
	}
	pr := response.Data.Repository.PullRequest
	expectedURL := "https://github.com/" + input.ResourceID
	if pr.ID == "" {
		return core.TrackerResource{}, core.NewTrackerProviderError(core.TrackerErrorNotFound, "Pull request evidence was not found", false, 0, nil)
	}
	if pr.Number != number || !strings.EqualFold(pr.URL, expectedURL) || !deliveryCommit.MatchString(pr.HeadRefOID) {
		return invalid()
	}
	fields := struct {
		Head   string `json:"head_commit"`
		Merge  string `json:"merge_commit,omitempty"`
		Checks string `json:"checks"`
	}{Head: pr.HeadRefOID, Checks: "unavailable"}
	if pr.Merged {
		if pr.MergeCommit == nil || !deliveryCommit.MatchString(pr.MergeCommit.OID) {
			return invalid()
		}
		fields.Merge = pr.MergeCommit.OID
	}
	if rollup := pr.StatusCheckRollup; rollup != nil {
		if rollup.Commit.OID != pr.HeadRefOID {
			return core.TrackerResource{}, core.NewTrackerProviderError(core.TrackerErrorConflict, "Check summary does not match the pull request head", true, 0, nil)
		}
		switch rollup.State {
		case "SUCCESS", "FAILURE", "PENDING", "ERROR", "EXPECTED":
			fields.Checks = strings.ToLower(rollup.State)
		default:
			return invalid()
		}
	}
	raw, _ := json.Marshal(fields)
	result := core.TrackerResource{ProviderID: ProviderID, ResourceType: "pull_request", ExternalID: pr.ID, CanonicalURL: pr.URL, ProviderRevision: trackerruntime.Revision(struct {
		ID     string
		Fields any
	}{pr.ID, fields}), SchemaRevision: "github-delivery-v1", ObservedAt: time.Now().UTC(), NormalizedFields: raw, NativeExtension: json.RawMessage(`{}`)}
	return result, result.Validate()
}
