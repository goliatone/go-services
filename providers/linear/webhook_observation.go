package linear

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

func (p *Provider) NormalizeTrackerWebhook(ctx context.Context, input core.TrackerWebhookObservationRequest) (core.TrackerWebhookObservation, error) {
	if err := p.VerifyTrackerWebhook(ctx, input.Verification); err != nil {
		return core.TrackerWebhookObservation{}, err
	}
	var payload struct {
		Action string          `json:"action"`
		Type   string          `json:"type"`
		URL    string          `json:"url"`
		Data   json.RawMessage `json:"data"`
	}
	if json.Unmarshal(input.Verification.Body, &payload) != nil || payload.Type != "Issue" || len(payload.Data) == 0 {
		return core.TrackerWebhookObservation{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue webhook payload is invalid", false, 0, nil)
	}
	var native struct {
		ID        string          `json:"id"`
		TeamID    string          `json:"teamId"`
		Team      json.RawMessage `json:"team"`
		UpdatedAt time.Time       `json:"updatedAt"`
	}
	if json.Unmarshal(payload.Data, &native) != nil || strings.TrimSpace(native.ID) == "" {
		return core.TrackerWebhookObservation{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue webhook identity is missing", false, 0, nil)
	}
	teamID := native.TeamID
	if teamID == "" && len(native.Team) > 0 {
		var team struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(native.Team, &team)
		teamID = team.ID
	}
	observation := core.TrackerWebhookObservation{ProviderID: ProviderID, DeliveryID: input.Verification.DeliveryID, Action: payload.Action, ScopeID: teamID, ExternalID: native.ID}
	if !native.UpdatedAt.IsZero() {
		observation.ProviderRevision = native.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if payload.Action == "create" || payload.Action == "update" {
		observation.RequiresRefresh = true
	}
	if err := observation.Validate(); err != nil {
		return core.TrackerWebhookObservation{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue webhook observation is incomplete", false, 0, err)
	}
	return observation, nil
}

var _ core.TrackerWebhookObservationProvider = (*Provider)(nil)
