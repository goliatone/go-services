package linear

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

func (p *Provider) VerifyTrackerWebhook(_ context.Context, request core.TrackerWebhookVerificationRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(request.Event), "Issue") {
		return core.NewTrackerProviderError(core.TrackerErrorValidation, "unsupported Linear webhook event", false, 0, nil)
	}
	provided, err := hex.DecodeString(strings.TrimSpace(request.Signature))
	if err != nil || len(provided) != sha256.Size {
		return core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear webhook signature is invalid", false, 0, nil)
	}
	mac := hmac.New(sha256.New, request.Secret)
	_, _ = mac.Write(request.Body)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear webhook signature is invalid", false, 0, nil)
	}
	var payload struct {
		Type      string `json:"type"`
		Timestamp int64  `json:"webhookTimestamp"`
	}
	if json.Unmarshal(request.Body, &payload) != nil || payload.Type != "Issue" || payload.Timestamp <= 0 {
		return core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear webhook payload is invalid", false, 0, nil)
	}
	timestamp := time.UnixMilli(payload.Timestamp)
	if delta := time.Since(timestamp); delta < -time.Minute || delta > time.Minute {
		return core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear webhook timestamp is stale", false, 0, nil)
	}
	return nil
}

var _ core.TrackerWebhookVerificationProvider = (*Provider)(nil)
