package facebook

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
	meta "github.com/goliatone/go-services/providers/meta/common"
)

func TestNew_UsesSharedMetaAuthDefaults(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if provider.ID() != ProviderID {
		t.Fatalf("expected provider id %q, got %q", ProviderID, provider.ID())
	}
	if provider.AuthKind() != core.AuthKindOAuth2AuthCode {
		t.Fatalf("expected auth kind %q, got %q", core.AuthKindOAuth2AuthCode, provider.AuthKind())
	}

	begin, err := provider.BeginAuth(context.Background(), core.BeginAuthRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
		State: "state_1",
	})
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}
	parsed, err := url.Parse(begin.URL)
	if err != nil {
		t.Fatalf("parse begin auth url: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != meta.MetaOAuthAuthURL {
		t.Fatalf("expected begin auth endpoint %q, got %q", meta.MetaOAuthAuthURL, parsed.Scheme+"://"+parsed.Host+parsed.Path)
	}

	scopeSet := parseScopeSet(parsed.Query().Get("scope"))
	for _, scope := range []string{ScopePagesShowList, ScopePagesReadEngagement, ScopeBusinessManagement} {
		if !scopeSet[scope] {
			t.Fatalf("expected scope %q in begin auth query", scope)
		}
	}
}

func TestProvider_BaselineCapabilitiesUseCanonicalRequiredGrants(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	descriptors := provider.Capabilities()
	expected := map[string]string{
		"pages.read":      GrantPagesShowList,
		"engagement.read": GrantPagesReadEngagement,
		"business.read":   GrantBusinessManagement,
	}
	if len(descriptors) != len(expected) {
		t.Fatalf("expected %d capabilities, got %d", len(expected), len(descriptors))
	}
	for _, descriptor := range descriptors {
		required, ok := expected[descriptor.Name]
		if !ok {
			t.Fatalf("unexpected capability descriptor %q", descriptor.Name)
		}
		if len(descriptor.RequiredGrants) != 1 || descriptor.RequiredGrants[0] != required {
			t.Fatalf("expected capability %q to require grant %q, got %v", descriptor.Name, required, descriptor.RequiredGrants)
		}
	}
}

func TestProvider_NormalizeGrantedPermissions(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	aware, ok := provider.(core.GrantAwareProvider)
	if !ok {
		t.Fatalf("expected provider to implement GrantAwareProvider")
	}
	grants, err := aware.NormalizeGrantedPermissions(context.Background(), []string{
		"pages_show_list",
		"meta:pages_read_engagement",
		" FACEBOOK:business_management ",
		"unsupported",
	})
	if err != nil {
		t.Fatalf("normalize granted permissions: %v", err)
	}
	expected := []string{GrantBusinessManagement, GrantPagesReadEngagement, GrantPagesShowList}
	if len(grants) != len(expected) {
		t.Fatalf("expected %d grants, got %d (%v)", len(expected), len(grants), grants)
	}
	for idx := range expected {
		if grants[idx] != expected[idx] {
			t.Fatalf("expected grant %q at index %d, got %q", expected[idx], idx, grants[idx])
		}
	}
}

func parseScopeSet(raw string) map[string]bool {
	set := map[string]bool{}
	for _, scope := range strings.Fields(raw) {
		set[strings.TrimSpace(scope)] = true
	}
	return set
}
