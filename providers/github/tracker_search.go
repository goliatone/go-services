package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

const (
	repositorySearchPageSize   = 100
	repositorySearchFetchLimit = 3
	repositorySearchMaxPage    = 2147483647
	repositorySearchTTL        = 15 * time.Minute
	repositoryAffiliation      = "owner,collaborator,organization_member"
)

type repositorySearchCursor struct {
	Version int   `json:"v"`
	Expires int64 `json:"exp"`
	Page    int64 `json:"page"`
	Offset  int   `json:"offset"`
}

type repositorySearchBinding struct {
	Connection, Search, Endpoint, TokenType, AccessToken string
	Grants                                               []string
	Limit                                                int
}

func (p *Provider) searchRepositories(ctx context.Context, input core.TrackerDiscoveryRequest, credential core.ActiveCredential, endpoint *url.URL) (core.TrackerNodePage, error) {
	if p.searchCursorKey == [32]byte{} {
		return core.TrackerNodePage{}, core.NewTrackerProviderError(core.TrackerErrorUnavailable, "repository search is unavailable", false, 0, nil)
	}
	binding := repositorySearchBinding{
		Connection: strings.TrimSpace(input.ConnectionID), Search: strings.ToLower(strings.TrimSpace(input.Search)),
		Endpoint: endpoint.String(), TokenType: credential.TokenType, AccessToken: credential.AccessToken,
		Grants: append([]string(nil), credential.GrantedScopes...), Limit: trackerruntime.Limit(input.Limit, 50, 100),
	}
	slices.Sort(binding.Grants)
	cursor, err := p.decodeSearchCursor(strings.TrimSpace(input.Cursor), binding, time.Now())
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	items := make([]core.TrackerNode, 0, binding.Limit)
	seen := make(map[string]bool)
	for fetch := 0; fetch < repositorySearchFetchLimit; fetch++ {
		if err := ctx.Err(); err != nil {
			return core.TrackerNodePage{}, err
		}
		target := *endpoint
		target.Path += "/user/repos"
		query := target.Query()
		query.Set("affiliation", repositoryAffiliation)
		query.Set("sort", "full_name")
		query.Set("direction", "asc")
		query.Set("per_page", strconv.Itoa(repositorySearchPageSize))
		query.Set("page", strconv.FormatInt(cursor.Page, 10))
		target.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return core.TrackerNodePage{}, err
		}
		var repos []repository
		if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &repos); err != nil {
			return core.TrackerNodePage{}, err
		}
		if len(repos) > repositorySearchPageSize {
			return core.TrackerNodePage{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "repository inventory exceeds page bound", false, 0, nil)
		}
		if cursor.Offset > 0 && cursor.Offset >= len(repos) {
			return core.TrackerNodePage{}, invalidSearchCursor()
		}
		for cursor.Offset < len(repos) {
			if err := ctx.Err(); err != nil {
				return core.TrackerNodePage{}, err
			}
			repo := repos[cursor.Offset]
			cursor.Offset++
			if !strings.Contains(strings.ToLower(repo.FullName), binding.Search) && !strings.Contains(strings.ToLower(repo.Owner.Login), binding.Search) {
				continue
			}
			identity := repo.FullName
			if repo.ID > 0 {
				identity = strconv.FormatInt(repo.ID, 10)
			}
			if seen[identity] {
				continue
			}
			seen[identity] = true
			items = append(items, githubRepositoryNode(repo))
			if len(items) == binding.Limit {
				break
			}
		}
		if err := ctx.Err(); err != nil {
			return core.TrackerNodePage{}, err
		}
		if cursor.Offset == len(repos) {
			if len(repos) < repositorySearchPageSize {
				return searchNodePage(items, ""), nil
			}
			if cursor.Page == repositorySearchMaxPage {
				return core.TrackerNodePage{}, invalidSearchCursor()
			}
			cursor.Page++
			cursor.Offset = 0
		}
		if len(items) == binding.Limit {
			break
		}
	}
	return searchNodePage(items, p.encodeSearchCursor(cursor, binding)), nil
}

func searchNodePage(items []core.TrackerNode, cursor string) core.TrackerNodePage {
	return core.TrackerNodePage{Items: items, NextCursor: cursor, HasMore: cursor != "", Revision: trackerruntime.Revision(items)}
}

func (p *Provider) searchCursorMAC(payload []byte, binding repositorySearchBinding) []byte {
	mac := hmac.New(sha256.New, p.searchCursorKey[:])
	_ = json.NewEncoder(mac).Encode(struct {
		Domain                       string
		Payload                      []byte
		Binding                      repositorySearchBinding
		Affiliation, Sort, Direction string
		PageSize                     int
	}{"github.repository-search.v1", payload, binding, repositoryAffiliation, "full_name", "asc", repositorySearchPageSize})
	return mac.Sum(nil)
}

func (p *Provider) encodeSearchCursor(cursor repositorySearchCursor, binding repositorySearchBinding) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(p.searchCursorMAC(payload, binding))
}

func (p *Provider) decodeSearchCursor(value string, binding repositorySearchBinding, now time.Time) (repositorySearchCursor, error) {
	if value == "" {
		return repositorySearchCursor{Version: 1, Expires: now.Add(repositorySearchTTL).Unix(), Page: 1}, nil
	}
	if len(value) > 4096 {
		return repositorySearchCursor{}, invalidSearchCursor()
	}
	body, signature, ok := strings.Cut(value, ".")
	if !ok {
		return repositorySearchCursor{}, invalidSearchCursor()
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return repositorySearchCursor{}, invalidSearchCursor()
	}
	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(mac, p.searchCursorMAC(payload, binding)) {
		return repositorySearchCursor{}, invalidSearchCursor()
	}
	var cursor repositorySearchCursor
	if json.Unmarshal(payload, &cursor) != nil || cursor.Version != 1 || cursor.Page < 1 || cursor.Page > repositorySearchMaxPage || cursor.Offset < 0 || cursor.Offset >= repositorySearchPageSize || cursor.Expires <= now.Unix() || cursor.Expires > now.Add(repositorySearchTTL).Unix() {
		return repositorySearchCursor{}, invalidSearchCursor()
	}
	return cursor, nil
}

func invalidSearchCursor() error {
	return core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, "repository search continuation is invalid; restart the search", false, 0, nil)
}
