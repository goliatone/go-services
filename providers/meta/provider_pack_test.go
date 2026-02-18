package meta_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/devkit"
	"github.com/goliatone/go-services/providers/meta/common"
	"github.com/goliatone/go-services/providers/meta/facebook"
	"github.com/goliatone/go-services/providers/meta/instagram"
	"github.com/goliatone/go-services/webhooks"
)

func TestMetaProviderPack_SharesAuthProfileEndpoints(t *testing.T) {
	instagramProvider, err := instagram.New(instagram.Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new instagram provider: %v", err)
	}
	facebookProvider, err := facebook.New(facebook.Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new facebook provider: %v", err)
	}

	instagramURL := beginAuthURL(t, instagramProvider)
	facebookURL := beginAuthURL(t, facebookProvider)
	if instagramURL != common.MetaOAuthAuthURL {
		t.Fatalf("expected instagram auth url %q, got %q", common.MetaOAuthAuthURL, instagramURL)
	}
	if facebookURL != common.MetaOAuthAuthURL {
		t.Fatalf("expected facebook auth url %q, got %q", common.MetaOAuthAuthURL, facebookURL)
	}
}

func TestMetaProviderPack_UsesProviderSpecificGrantMapping(t *testing.T) {
	instagramProvider, err := instagram.New(instagram.Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new instagram provider: %v", err)
	}
	facebookProvider, err := facebook.New(facebook.Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new facebook provider: %v", err)
	}

	instagramAware := instagramProvider.(core.GrantAwareProvider)
	facebookAware := facebookProvider.(core.GrantAwareProvider)

	instagramGrants, err := instagramAware.NormalizeGrantedPermissions(context.Background(), []string{
		"instagram_basic",
		"meta:pages_show_list",
		"pages_read_engagement",
	})
	if err != nil {
		t.Fatalf("normalize instagram grants: %v", err)
	}
	facebookGrants, err := facebookAware.NormalizeGrantedPermissions(context.Background(), []string{
		"instagram_basic",
		"meta:pages_show_list",
		"pages_read_engagement",
	})
	if err != nil {
		t.Fatalf("normalize facebook grants: %v", err)
	}

	expectedInstagram := []string{instagram.GrantInstagramBasic, instagram.GrantPagesShowList}
	assertSameStrings(t, expectedInstagram, instagramGrants)

	expectedFacebook := []string{facebook.GrantPagesReadEngagement, facebook.GrantPagesShowList}
	assertSameStrings(t, expectedFacebook, facebookGrants)
}

func TestMetaProviderPack_WebhookDedupeIsIsolatedPerProvider(t *testing.T) {
	secret := "meta_secret"
	ledger := devkit.NewWebhookDeliveryLedgerFixture()
	handler := staticWebhookHandler{}

	instagramTemplate := instagram.NewWebhookTemplate(instagram.DefaultWebhookConfig(secret))
	facebookTemplate := facebook.NewWebhookTemplate(facebook.DefaultWebhookConfig(secret))

	instagramProcessor := webhooks.NewProcessor(instagramTemplate.Verifier, ledger, handler)
	instagramProcessor.ExtractID = instagramTemplate.Extractor
	facebookProcessor := webhooks.NewProcessor(facebookTemplate.Verifier, ledger, handler)
	facebookProcessor.ExtractID = facebookTemplate.Extractor

	deliveryID := "meta_delivery_1"
	instagramReq := signedMetaRequest(
		instagram.ProviderID,
		[]byte(`{"object":"instagram","entry":[{"changes":[{"field":"comments"}]}]}`),
		secret,
		deliveryID,
	)
	facebookReq := signedMetaRequest(
		facebook.ProviderID,
		[]byte(`{"object":"page","entry":[{"changes":[{"field":"feed"}]}]}`),
		secret,
		deliveryID,
	)

	instagramResult, err := instagramProcessor.Process(context.Background(), instagramReq)
	if err != nil {
		t.Fatalf("process instagram delivery: %v", err)
	}
	if !instagramResult.Accepted {
		t.Fatalf("expected instagram delivery to be accepted")
	}

	facebookResult, err := facebookProcessor.Process(context.Background(), facebookReq)
	if err != nil {
		t.Fatalf("process facebook delivery: %v", err)
	}
	if !facebookResult.Accepted {
		t.Fatalf("expected facebook delivery to be accepted")
	}

	deduped, err := instagramProcessor.Process(context.Background(), instagramReq)
	if err != nil {
		t.Fatalf("process duplicate instagram delivery: %v", err)
	}
	if deduped.Metadata == nil || deduped.Metadata["deduped"] != true {
		t.Fatalf("expected duplicate instagram delivery to be deduped, got metadata %v", deduped.Metadata)
	}
}

type staticWebhookHandler struct{}

func (staticWebhookHandler) Handle(context.Context, core.InboundRequest) (core.InboundResult, error) {
	return core.InboundResult{Accepted: true, StatusCode: 202}, nil
}

func beginAuthURL(t *testing.T, provider core.Provider) string {
	t.Helper()
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
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}

func signedMetaRequest(providerID string, body []byte, secret string, deliveryID string) core.InboundRequest {
	return core.InboundRequest{
		ProviderID: providerID,
		Body:       body,
		Headers: map[string]string{
			"X-Hub-Signature-256": "sha256=" + signWebhookBody(secret, body),
			"X-Meta-Delivery-Id":  strings.TrimSpace(deliveryID),
		},
	}
}

func signWebhookBody(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func assertSameStrings(t *testing.T, expected []string, actual []string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("expected %d values, got %d (%v)", len(expected), len(actual), actual)
	}
	for idx := range expected {
		if actual[idx] != expected[idx] {
			t.Fatalf("expected value %q at index %d, got %q", expected[idx], idx, actual[idx])
		}
	}
}
