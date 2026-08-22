package services

import (
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/github"
	"github.com/goliatone/go-services/providers/githubprojects"
	"github.com/goliatone/go-services/providers/jira"
	"github.com/goliatone/go-services/providers/linear"
)

func TestTrackerProviderFactoriesExposeTypedContract(t *testing.T) {
	cases := []struct {
		id string
		fn func() (core.Provider, error)
	}{
		{id: github.ProviderID, fn: func() (core.Provider, error) {
			return GitHubProvider(github.Config{ClientID: "client", ClientSecret: "secret"})
		}},
		{id: githubprojects.ProviderID, fn: func() (core.Provider, error) {
			return GitHubProjectsProvider(githubprojects.Config{ClientID: "client", ClientSecret: "secret"})
		}},
		{id: linear.ProviderID, fn: func() (core.Provider, error) { return LinearProvider(linear.Config{}) }},
		{id: jira.ProviderID, fn: func() (core.Provider, error) { return JiraProvider(jira.Config{}) }},
	}
	for _, test := range cases {
		provider, err := test.fn()
		if err != nil {
			t.Fatalf("%s: %v", test.id, err)
		}
		if provider.ID() != test.id {
			t.Fatalf("id = %q, want %q", provider.ID(), test.id)
		}
		if _, ok := provider.(core.TrackerProvider); !ok {
			t.Fatalf("%s does not implement core.TrackerProvider", test.id)
		}
	}
}
