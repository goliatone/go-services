package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Service) Subscribe(ctx context.Context, req SubscribeRequest) (Subscription, error) {
	if s == nil || s.connectionStore == nil || s.subscriptionStore == nil {
		return Subscription{}, s.mapError(fmt.Errorf("core: subscribe requires connection and subscription stores"))
	}

	connectionID := strings.TrimSpace(req.ConnectionID)
	if connectionID == "" {
		return Subscription{}, s.mapError(fmt.Errorf("core: connection id is required"))
	}
	if strings.TrimSpace(req.ResourceType) == "" {
		return Subscription{}, s.mapError(fmt.Errorf("core: resource type is required"))
	}
	if strings.TrimSpace(req.ResourceID) == "" {
		return Subscription{}, s.mapError(fmt.Errorf("core: resource id is required"))
	}
	if strings.TrimSpace(req.CallbackURL) == "" {
		return Subscription{}, s.mapError(fmt.Errorf("core: callback url is required"))
	}
	req.ConnectionID = connectionID

	connection, err := s.connectionStore.Get(ctx, connectionID)
	if err != nil {
		return Subscription{}, s.mapError(err)
	}

	provider, err := s.resolveProvider(connection.ProviderID)
	if err != nil {
		return Subscription{}, err
	}
	subscribable, ok := provider.(SubscribableProvider)
	if !ok {
		return Subscription{}, s.mapError(fmt.Errorf("core: provider %q is not subscribable", connection.ProviderID))
	}

	result, err := subscribable.Subscribe(ctx, req)
	if err != nil {
		return Subscription{}, s.mapError(err)
	}

	record, err := s.subscriptionStore.Upsert(ctx, UpsertSubscriptionInput{
		ConnectionID:         connection.ID,
		ProviderID:           connection.ProviderID,
		ResourceType:         strings.TrimSpace(req.ResourceType),
		ResourceID:           strings.TrimSpace(req.ResourceID),
		ChannelID:            strings.TrimSpace(result.ChannelID),
		RemoteSubscriptionID: strings.TrimSpace(result.RemoteSubscriptionID),
		CallbackURL:          strings.TrimSpace(req.CallbackURL),
		Status:               SubscriptionStatusActive,
		ExpiresAt:            result.ExpiresAt,
		Metadata:             copyAnyMap(result.Metadata),
	})
	if err != nil {
		return Subscription{}, s.mapError(err)
	}

	return record, nil
}

func (s *Service) RenewSubscription(ctx context.Context, req RenewSubscriptionRequest) (Subscription, error) {
	if s == nil || s.subscriptionStore == nil {
		return Subscription{}, s.mapError(fmt.Errorf("core: subscription store is required"))
	}

	subscriptionID := strings.TrimSpace(req.SubscriptionID)
	if subscriptionID == "" {
		return Subscription{}, s.mapError(fmt.Errorf("core: subscription id is required"))
	}

	existing, err := s.subscriptionStore.Get(ctx, subscriptionID)
	if err != nil {
		return Subscription{}, s.mapError(err)
	}

	provider, err := s.resolveProvider(existing.ProviderID)
	if err != nil {
		return Subscription{}, err
	}
	subscribable, ok := provider.(SubscribableProvider)
	if !ok {
		return Subscription{}, s.mapError(fmt.Errorf("core: provider %q is not subscribable", existing.ProviderID))
	}

	result, err := subscribable.RenewSubscription(ctx, req)
	if err != nil {
		_ = s.subscriptionStore.UpdateState(ctx, existing.ID, SubscriptionStatusErrored, err.Error())
		return Subscription{}, s.mapError(err)
	}

	channelID := strings.TrimSpace(result.ChannelID)
	if channelID == "" {
		channelID = existing.ChannelID
	}
	remoteID := strings.TrimSpace(result.RemoteSubscriptionID)
	if remoteID == "" {
		remoteID = existing.RemoteSubscriptionID
	}
	expiresAt := result.ExpiresAt
	if expiresAt == nil && !existing.ExpiresAt.IsZero() {
		value := existing.ExpiresAt
		expiresAt = &value
	}

	metadata := mergeAnyMap(existing.Metadata, result.Metadata)
	record, err := s.subscriptionStore.Upsert(ctx, UpsertSubscriptionInput{
		ConnectionID:         existing.ConnectionID,
		ProviderID:           existing.ProviderID,
		ResourceType:         existing.ResourceType,
		ResourceID:           existing.ResourceID,
		ChannelID:            channelID,
		RemoteSubscriptionID: remoteID,
		CallbackURL:          existing.CallbackURL,
		VerificationTokenRef: existing.VerificationTokenRef,
		Status:               SubscriptionStatusActive,
		ExpiresAt:            expiresAt,
		Metadata:             metadata,
	})
	if err != nil {
		return Subscription{}, s.mapError(err)
	}

	return record, nil
}

func (s *Service) CancelSubscription(ctx context.Context, req CancelSubscriptionRequest) error {
	if s == nil || s.subscriptionStore == nil {
		return s.mapError(fmt.Errorf("core: subscription store is required"))
	}

	subscriptionID := strings.TrimSpace(req.SubscriptionID)
	if subscriptionID == "" {
		return s.mapError(fmt.Errorf("core: subscription id is required"))
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "cancelled"
	}

	existing, err := s.subscriptionStore.Get(ctx, subscriptionID)
	if err != nil {
		return s.mapError(err)
	}

	provider, err := s.resolveProvider(existing.ProviderID)
	if err != nil {
		return err
	}
	subscribable, ok := provider.(SubscribableProvider)
	if !ok {
		return s.mapError(fmt.Errorf("core: provider %q is not subscribable", existing.ProviderID))
	}
	if err := subscribable.CancelSubscription(ctx, CancelSubscriptionRequest{
		SubscriptionID: subscriptionID,
		Reason:         reason,
	}); err != nil {
		_ = s.subscriptionStore.UpdateState(ctx, existing.ID, SubscriptionStatusErrored, err.Error())
		return s.mapError(err)
	}

	if err := s.subscriptionStore.UpdateState(
		ctx,
		existing.ID,
		SubscriptionStatusCancelled,
		reason,
	); err != nil {
		return s.mapError(err)
	}
	return nil
}

func mergeAnyMap(left map[string]any, right map[string]any) map[string]any {
	if len(left) == 0 && len(right) == 0 {
		return map[string]any{}
	}
	merged := map[string]any{}
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		merged[key] = value
	}
	if _, ok := merged["renewed_at"]; !ok {
		merged["renewed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return merged
}
