package sqlstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/core"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type ActivityStore struct {
	db   *bun.DB
	repo repository.Repository[*activityEntryRecord]
}

func NewActivityStore(db *bun.DB) (*ActivityStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*activityEntryRecord](db, activityHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid activity repository wiring: %w", err)
		}
	}
	return &ActivityStore{db: db, repo: repo}, nil
}

func (s *ActivityStore) Record(ctx context.Context, entry core.ServiceActivityEntry) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: activity store is not configured")
	}
	metadata := copyAnyMap(entry.Metadata)
	providerID := metadataString(metadata, "provider_id")
	scopeType := metadataString(metadata, "scope_type")
	scopeID := metadataString(metadata, "scope_id")
	if providerID == "" || scopeType == "" || scopeID == "" {
		return fmt.Errorf("sqlstore: activity metadata requires provider_id, scope_type, and scope_id")
	}

	objectType, objectID := parseObject(entry.Object)
	actorType := metadataString(metadata, "actor_type")
	if actorType == "" {
		actorType = inferActorType(entry.Actor)
	}
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		id = uuid.NewString()
	}
	createdAt := entry.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	record := &activityEntryRecord{
		ID:         id,
		ProviderID: providerID,
		ScopeType:  scopeType,
		ScopeID:    scopeID,
		Channel:    strings.TrimSpace(entry.Channel),
		Action:     strings.TrimSpace(entry.Action),
		ObjectType: objectType,
		ObjectID:   objectID,
		Actor:      strings.TrimSpace(entry.Actor),
		ActorType:  actorType,
		Status:     strings.TrimSpace(string(entry.Status)),
		Metadata:   metadata,
		CreatedAt:  createdAt,
	}
	if record.Channel == "" {
		record.Channel = core.DefaultLifecycleChannel
	}
	if record.Action == "" {
		record.Action = "lifecycle.event"
	}
	if record.ObjectType == "" {
		record.ObjectType = "event"
	}
	if record.ObjectID == "" {
		record.ObjectID = id
	}
	if record.Actor == "" {
		record.Actor = "system"
	}
	if record.Status == "" {
		record.Status = string(core.ServiceActivityStatusOK)
	}
	if value := metadataString(metadata, "connection_id"); value != "" {
		record.ConnectionID = &value
	}
	if value := metadataString(metadata, "installation_id"); value != "" {
		record.InstallationID = &value
	}
	if value := metadataString(metadata, "subscription_id"); value != "" {
		record.SubscriptionID = &value
	}
	if value := metadataString(metadata, "sync_job_id"); value != "" {
		record.SyncJobID = &value
	}

	_, err := s.repo.Create(ctx, record)
	return err
}

func (s *ActivityStore) List(ctx context.Context, filter core.ServicesActivityFilter) (core.ServicesActivityPage, error) {
	if s == nil || s.repo == nil {
		return core.ServicesActivityPage{}, fmt.Errorf("sqlstore: activity store is not configured")
	}
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage <= 0 {
		perPage = 25
	}
	offset := (page - 1) * perPage

	selectors := []repository.SelectCriteria{
		repository.OrderBy("created_at DESC"),
		repository.SelectPaginate(perPage, offset),
	}
	if providerID := strings.TrimSpace(filter.ProviderID); providerID != "" {
		selectors = append(selectors, repository.SelectBy("provider_id", "=", providerID))
	}
	if scopeType := strings.TrimSpace(filter.ScopeType); scopeType != "" {
		selectors = append(selectors, repository.SelectBy("scope_type", "=", scopeType))
	}
	if scopeID := strings.TrimSpace(filter.ScopeID); scopeID != "" {
		selectors = append(selectors, repository.SelectBy("scope_id", "=", scopeID))
	}
	if action := strings.TrimSpace(filter.Action); action != "" {
		selectors = append(selectors, repository.SelectBy("action", "=", action))
	}
	if status := strings.TrimSpace(string(filter.Status)); status != "" {
		selectors = append(selectors, repository.SelectBy("status", "=", status))
	}
	if filter.From != nil {
		selectors = append(selectors, repository.SelectByTimetz("created_at", ">=", filter.From.UTC()))
	}
	if filter.To != nil {
		selectors = append(selectors, repository.SelectByTimetz("created_at", "<=", filter.To.UTC()))
	}

	records, total, err := s.repo.List(ctx, selectors...)
	if err != nil {
		return core.ServicesActivityPage{}, err
	}
	items := make([]core.ServiceActivityEntry, 0, len(records))
	for _, record := range records {
		items = append(items, activityRecordToDomain(record))
	}
	hasNext := offset+len(items) < total
	nextOffset := ""
	if hasNext {
		nextOffset = strconv.Itoa(offset + len(items))
	}
	return core.ServicesActivityPage{
		Items:      items,
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		HasNext:    hasNext,
		NextCursor: nextOffset,
	}, nil
}

func (s *ActivityStore) Prune(ctx context.Context, policy core.ActivityRetentionPolicy) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("sqlstore: activity store is not configured")
	}
	deleted := 0
	now := time.Now().UTC()

	if policy.TTL > 0 {
		cutoff := now.Add(-policy.TTL)
		res, err := s.db.NewDelete().
			Model((*activityEntryRecord)(nil)).
			Where("created_at < ?", cutoff).
			Exec(ctx)
		if err != nil {
			return deleted, err
		}
		affected, _ := res.RowsAffected()
		deleted += int(affected)
	}

	if policy.RowCap > 0 {
		total, err := s.db.NewSelect().Model((*activityEntryRecord)(nil)).Count(ctx)
		if err != nil {
			return deleted, err
		}
		excess := total - policy.RowCap
		if excess > 0 {
			res, err := s.db.NewRaw(
				"DELETE FROM service_activity_entries WHERE id IN (SELECT id FROM service_activity_entries ORDER BY created_at ASC LIMIT ?)",
				excess,
			).Exec(ctx)
			if err != nil {
				return deleted, err
			}
			affected, _ := res.RowsAffected()
			deleted += int(affected)
		}
	}

	return deleted, nil
}

func activityRecordToDomain(record *activityEntryRecord) core.ServiceActivityEntry {
	if record == nil {
		return core.ServiceActivityEntry{}
	}
	object := strings.TrimSpace(record.ObjectType) + ":" + strings.TrimSpace(record.ObjectID)
	metadata := copyAnyMap(record.Metadata)
	metadata["provider_id"] = strings.TrimSpace(record.ProviderID)
	metadata["scope_type"] = strings.TrimSpace(record.ScopeType)
	metadata["scope_id"] = strings.TrimSpace(record.ScopeID)
	if record.ConnectionID != nil {
		metadata["connection_id"] = strings.TrimSpace(*record.ConnectionID)
	}
	if record.InstallationID != nil {
		metadata["installation_id"] = strings.TrimSpace(*record.InstallationID)
	}
	if record.SubscriptionID != nil {
		metadata["subscription_id"] = strings.TrimSpace(*record.SubscriptionID)
	}
	if record.SyncJobID != nil {
		metadata["sync_job_id"] = strings.TrimSpace(*record.SyncJobID)
	}

	return core.ServiceActivityEntry{
		ID:        record.ID,
		Actor:     record.Actor,
		Action:    record.Action,
		Object:    object,
		Channel:   record.Channel,
		Status:    core.ServiceActivityStatus(record.Status),
		Metadata:  metadata,
		CreatedAt: record.CreatedAt,
	}
}

func parseObject(value string) (objectType string, objectID string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	parts := strings.SplitN(value, ":", 2)
	if len(parts) == 1 {
		return "event", parts[0]
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func inferActorType(actor string) string {
	normalized := strings.ToLower(strings.TrimSpace(actor))
	switch normalized {
	case "user", "system", "job", "webhook":
		return normalized
	default:
		return "system"
	}
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return ""
	}
	return text
}

var (
	_ core.ServicesActivitySink    = (*ActivityStore)(nil)
	_ core.ActivityRetentionPruner = (*ActivityStore)(nil)
)
