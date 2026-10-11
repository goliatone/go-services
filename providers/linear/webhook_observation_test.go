package linear

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestNormalizeTrackerWebhookRequiresVerificationAndPreservesIdentity(t *testing.T) {
	provider := &Provider{}
	secret := []byte("signing-secret")
	body := []byte(fmt.Sprintf(`{"type":"Issue","action":"update","webhookTimestamp":%d,"url":"https://linear.app/team/issue/TEAM-42","data":{"id":"issue-1","identifier":"TEAM-42","teamId":"team-1","stateId":"state-2","title":"Ready","updatedAt":"2026-09-23T10:00:00Z"}}`, time.Now().UnixMilli()))
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	request := core.TrackerWebhookObservationRequest{Verification: core.TrackerWebhookVerificationRequest{Secret: secret, Body: body, Signature: hex.EncodeToString(mac.Sum(nil)), DeliveryID: "delivery-1", Event: "Issue"}, ObservedAt: time.Now().UTC()}
	observation, err := provider.NormalizeTrackerWebhook(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if observation.ScopeID != "team-1" || observation.ExternalID != "issue-1" || !observation.RequiresRefresh || observation.Resource != nil || observation.ProviderRevision != "2026-09-23T10:00:00Z" {
		t.Fatalf("observation = %#v", observation)
	}
	request.Verification.Signature = "bad"
	if _, err := provider.NormalizeTrackerWebhook(context.Background(), request); err == nil {
		t.Fatal("unverified delivery was normalized")
	}
}
