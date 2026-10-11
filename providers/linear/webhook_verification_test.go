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

func TestVerifyTrackerWebhook(t *testing.T) {
	provider := &Provider{}
	secret := []byte("signing-secret")
	body := []byte(fmt.Sprintf(`{"type":"Issue","webhookTimestamp":%d}`, time.Now().UnixMilli()))
	sign := func(body []byte) string {
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(body)
		return hex.EncodeToString(mac.Sum(nil))
	}
	request := core.TrackerWebhookVerificationRequest{Secret: secret, Body: body, Signature: sign(body), DeliveryID: "delivery-1", Event: "Issue"}
	if err := provider.VerifyTrackerWebhook(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*core.TrackerWebhookVerificationRequest){
		"bad signature": func(r *core.TrackerWebhookVerificationRequest) { r.Signature = sign([]byte("different")) },
		"wrong event":   func(r *core.TrackerWebhookVerificationRequest) { r.Event = "Project" },
		"stale timestamp": func(r *core.TrackerWebhookVerificationRequest) {
			r.Body = []byte(fmt.Sprintf(`{"type":"Issue","webhookTimestamp":%d}`, time.Now().Add(-2*time.Minute).UnixMilli()))
			r.Signature = sign(r.Body)
		},
		"body event mismatch": func(r *core.TrackerWebhookVerificationRequest) {
			r.Body = []byte(fmt.Sprintf(`{"type":"Project","webhookTimestamp":%d}`, time.Now().UnixMilli()))
			r.Signature = sign(r.Body)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			mutate(&changed)
			if err := provider.VerifyTrackerWebhook(context.Background(), changed); err == nil {
				t.Fatal("unverified webhook accepted")
			}
		})
	}
}
