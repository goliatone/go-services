package core

import (
	"context"
	"errors"
	"strings"
	"time"
)

type TrackerWebhookObservation struct {
	ProviderID       string
	DeliveryID       string
	Action           string
	ScopeID          string
	ExternalID       string
	ProviderRevision string
	RequiresRefresh  bool
	Resource         *TrackerResource
}

func (o TrackerWebhookObservation) Validate() error {
	if strings.TrimSpace(o.ProviderID) == "" || strings.TrimSpace(o.DeliveryID) == "" || strings.TrimSpace(o.ScopeID) == "" || strings.TrimSpace(o.ExternalID) == "" {
		return errors.New("core: tracker webhook observation identity is required")
	}
	switch o.Action {
	case "create", "update":
		if o.RequiresRefresh {
			if o.Resource != nil {
				return errors.New("core: tracker webhook refresh hint cannot include a resource")
			}
			return nil
		}
		if o.Resource == nil || o.Resource.ExternalID != o.ExternalID || o.Resource.ProviderID != o.ProviderID {
			return errors.New("core: tracker webhook resource is required")
		}
		return o.Resource.Validate()
	case "remove":
		if o.Resource != nil {
			return errors.New("core: tracker webhook removal must not include a resource")
		}
		return nil
	default:
		return errors.New("core: tracker webhook action is unsupported")
	}
}

type TrackerWebhookObservationRequest struct {
	Verification TrackerWebhookVerificationRequest
	ObservedAt   time.Time
}

type TrackerWebhookObservationProvider interface {
	Provider
	NormalizeTrackerWebhook(context.Context, TrackerWebhookObservationRequest) (TrackerWebhookObservation, error)
}
