package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type IdentityBindingReconcilerOption func(*IdentityBindingReconciler)

func WithIdentityConfidenceThreshold(threshold float64) IdentityBindingReconcilerOption {
	return func(r *IdentityBindingReconciler) {
		if r == nil || threshold < 0 || threshold > 1 {
			return
		}
		r.confidentThreshold = threshold
	}
}

func WithIdentityExactThreshold(threshold float64) IdentityBindingReconcilerOption {
	return func(r *IdentityBindingReconciler) {
		if r == nil || threshold < 0 || threshold > 1 {
			return
		}
		r.exactThreshold = threshold
	}
}

func WithIdentityAmbiguousDelta(delta float64) IdentityBindingReconcilerOption {
	return func(r *IdentityBindingReconciler) {
		if r == nil || delta < 0 || delta > 1 {
			return
		}
		r.ambiguousDelta = delta
	}
}

func WithIdentityReconcilerClock(now func() time.Time) IdentityBindingReconcilerOption {
	return func(r *IdentityBindingReconciler) {
		if r == nil || now == nil {
			return
		}
		r.now = now
	}
}

type IdentityBindingReconciler struct {
	store              IdentityBindingStore
	now                func() time.Time
	exactThreshold     float64
	confidentThreshold float64
	ambiguousDelta     float64
}

func NewIdentityBindingReconciler(
	store IdentityBindingStore,
	opts ...IdentityBindingReconcilerOption,
) (*IdentityBindingReconciler, error) {
	if store == nil {
		return nil, fmt.Errorf("core: identity binding store is required")
	}
	r := &IdentityBindingReconciler{
		store:              store,
		now:                func() time.Time { return time.Now().UTC() },
		exactThreshold:     0.99,
		confidentThreshold: 0.80,
		ambiguousDelta:     0.05,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r, nil
}

func (r *IdentityBindingReconciler) ReconcileIdentity(
	ctx context.Context,
	req ReconcileIdentityRequest,
) (ReconcileIdentityResult, error) {
	if r == nil || r.store == nil {
		return ReconcileIdentityResult{}, fmt.Errorf("core: identity binding reconciler store is required")
	}

	req = normalizeReconcileIdentityRequest(req)
	if req.ProviderID == "" {
		return ReconcileIdentityResult{}, fmt.Errorf("core: provider id is required")
	}
	if err := req.Scope.Validate(); err != nil {
		return ReconcileIdentityResult{}, err
	}
	if req.ConnectionID == "" || req.SyncBindingID == "" {
		return ReconcileIdentityResult{}, fmt.Errorf("core: connection id and sync binding id are required")
	}
	if req.SourceObject == "" || req.ExternalID == "" {
		return ReconcileIdentityResult{}, fmt.Errorf("core: source object and external id are required")
	}

	existing, found, err := r.store.GetByExternalID(ctx, req.SyncBindingID, req.ExternalID)
	if err != nil {
		return ReconcileIdentityResult{}, err
	}
	if found {
		return ReconcileIdentityResult{
			Binding: existing,
			Created: false,
		}, nil
	}

	candidates := normalizeCandidates(req.Candidates)
	matchKind, selected, secondConfidence := r.resolveMatchKind(candidates)

	metadata := copyMetadata(req.Metadata)
	metadata["reconcile.candidate_count"] = len(candidates)
	metadata["reconcile.second_confidence"] = secondConfidence
	if selected != nil {
		metadata["reconcile.top_confidence"] = selected.Confidence
		metadata["reconcile.top_internal_type"] = selected.InternalType
		metadata["reconcile.top_internal_id"] = selected.InternalID
	}

	binding := IdentityBinding{
		ProviderID:    req.ProviderID,
		Scope:         req.Scope,
		ConnectionID:  req.ConnectionID,
		SyncBindingID: req.SyncBindingID,
		SourceObject:  req.SourceObject,
		ExternalID:    req.ExternalID,
		MatchKind:     matchKind,
		Metadata:      metadata,
	}

	switch matchKind {
	case IdentityBindingMatchExact, IdentityBindingMatchConfident:
		if selected != nil {
			binding.InternalType = selected.InternalType
			binding.InternalID = selected.InternalID
			binding.Confidence = selected.Confidence
		}
	case IdentityBindingMatchAmbiguous:
		if selected != nil {
			binding.Confidence = selected.Confidence
		}
	default:
		binding.Confidence = 0
	}

	saved, saveErr := r.store.Upsert(ctx, binding)
	if saveErr != nil {
		return ReconcileIdentityResult{}, saveErr
	}
	return ReconcileIdentityResult{
		Binding: saved,
		Created: true,
	}, nil
}

func (r *IdentityBindingReconciler) resolveMatchKind(
	candidates []IdentityCandidate,
) (IdentityBindingMatchKind, *IdentityCandidate, float64) {
	if len(candidates) == 0 {
		return IdentityBindingMatchUnresolved, nil, 0
	}

	top := candidates[0]
	secondConfidence := 0.0
	if len(candidates) > 1 {
		secondConfidence = candidates[1].Confidence
		if top.Confidence-secondConfidence <= r.ambiguousDelta {
			return IdentityBindingMatchAmbiguous, &top, secondConfidence
		}
	}

	if top.Confidence >= r.exactThreshold {
		return IdentityBindingMatchExact, &top, secondConfidence
	}
	if top.Confidence >= r.confidentThreshold {
		return IdentityBindingMatchConfident, &top, secondConfidence
	}
	return IdentityBindingMatchUnresolved, &top, secondConfidence
}

func normalizeCandidates(raw []IdentityCandidate) []IdentityCandidate {
	candidates := make([]IdentityCandidate, 0, len(raw))
	for _, candidate := range raw {
		candidate.InternalType = strings.TrimSpace(candidate.InternalType)
		candidate.InternalID = strings.TrimSpace(candidate.InternalID)
		if candidate.InternalType == "" || candidate.InternalID == "" {
			continue
		}
		if candidate.Confidence < 0 || candidate.Confidence > 1 {
			continue
		}
		candidates = append(candidates, candidate)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.Confidence != right.Confidence {
			return left.Confidence > right.Confidence
		}
		if left.InternalType != right.InternalType {
			return left.InternalType < right.InternalType
		}
		return left.InternalID < right.InternalID
	})
	return candidates
}

func normalizeReconcileIdentityRequest(req ReconcileIdentityRequest) ReconcileIdentityRequest {
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.Scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(req.Scope.Type)),
		ID:   strings.TrimSpace(req.Scope.ID),
	}
	req.ConnectionID = strings.TrimSpace(req.ConnectionID)
	req.SyncBindingID = strings.TrimSpace(req.SyncBindingID)
	req.SourceObject = strings.TrimSpace(req.SourceObject)
	req.ExternalID = strings.TrimSpace(req.ExternalID)
	return req
}

func copyMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return make(map[string]any)
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}
