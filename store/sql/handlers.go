package sqlstore

import (
	"strings"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/google/uuid"
)

func connectionHandlers() repository.ModelHandlers[*connectionRecord] {
	return stringIDHandlers(
		func() *connectionRecord { return &connectionRecord{} },
		func(record *connectionRecord) *string { return connectionRecordID(record) },
	)
}

func credentialHandlers() repository.ModelHandlers[*credentialRecord] {
	return stringIDHandlers(
		func() *credentialRecord { return &credentialRecord{} },
		func(record *credentialRecord) *string { return credentialRecordID(record) },
	)
}

func eventHandlers() repository.ModelHandlers[*serviceEventRecord] {
	return stringIDHandlers(
		func() *serviceEventRecord { return &serviceEventRecord{} },
		func(record *serviceEventRecord) *string { return serviceEventRecordID(record) },
	)
}

func activityHandlers() repository.ModelHandlers[*activityEntryRecord] {
	return stringIDHandlers(
		func() *activityEntryRecord { return &activityEntryRecord{} },
		func(record *activityEntryRecord) *string { return activityEntryRecordID(record) },
	)
}

func grantEventHandlers() repository.ModelHandlers[*grantEventRecord] {
	return stringIDHandlers(
		func() *grantEventRecord { return &grantEventRecord{} },
		func(record *grantEventRecord) *string { return grantEventRecordID(record) },
	)
}

func grantSnapshotHandlers() repository.ModelHandlers[*grantSnapshotRecord] {
	return stringIDHandlers(
		func() *grantSnapshotRecord { return &grantSnapshotRecord{} },
		func(record *grantSnapshotRecord) *string { return grantSnapshotRecordID(record) },
	)
}

func subscriptionHandlers() repository.ModelHandlers[*subscriptionRecord] {
	return stringIDHandlers(
		func() *subscriptionRecord { return &subscriptionRecord{} },
		func(record *subscriptionRecord) *string { return subscriptionRecordID(record) },
	)
}

func webhookDeliveryHandlers() repository.ModelHandlers[*webhookDeliveryRecord] {
	return stringIDHandlers(
		func() *webhookDeliveryRecord { return &webhookDeliveryRecord{} },
		func(record *webhookDeliveryRecord) *string { return webhookDeliveryRecordID(record) },
	)
}

func syncCursorHandlers() repository.ModelHandlers[*syncCursorRecord] {
	return stringIDHandlers(
		func() *syncCursorRecord { return &syncCursorRecord{} },
		func(record *syncCursorRecord) *string { return syncCursorRecordID(record) },
	)
}

func installationHandlers() repository.ModelHandlers[*installationRecord] {
	return stringIDHandlers(
		func() *installationRecord { return &installationRecord{} },
		func(record *installationRecord) *string { return installationRecordID(record) },
	)
}

func rateLimitStateHandlers() repository.ModelHandlers[*rateLimitStateRecord] {
	return stringIDHandlers(
		func() *rateLimitStateRecord { return &rateLimitStateRecord{} },
		func(record *rateLimitStateRecord) *string { return rateLimitStateRecordID(record) },
	)
}

func syncJobHandlers() repository.ModelHandlers[*syncJobRecord] {
	return stringIDHandlers(
		func() *syncJobRecord { return &syncJobRecord{} },
		func(record *syncJobRecord) *string { return syncJobRecordID(record) },
	)
}

func outboxHandlers() repository.ModelHandlers[*lifecycleOutboxRecord] {
	return stringIDHandlers(
		func() *lifecycleOutboxRecord { return &lifecycleOutboxRecord{} },
		func(record *lifecycleOutboxRecord) *string { return lifecycleOutboxRecordID(record) },
	)
}

func notificationDispatchHandlers() repository.ModelHandlers[*notificationDispatchRecord] {
	return stringIDHandlers(
		func() *notificationDispatchRecord { return &notificationDispatchRecord{} },
		func(record *notificationDispatchRecord) *string { return notificationDispatchRecordID(record) },
	)
}

func stringIDHandlers[T any](newRecord func() T, idField func(T) *string) repository.ModelHandlers[T] {
	return repository.ModelHandlers[T]{
		NewRecord: newRecord,
		GetID: func(record T) uuid.UUID {
			field := idField(record)
			if field == nil {
				return uuid.Nil
			}
			return parseUUID(*field)
		},
		SetID: func(record T, id uuid.UUID) {
			if field := idField(record); field != nil {
				*field = id.String()
			}
		},
		GetIdentifier: func() string { return "id" },
		GetIdentifierValue: func(record T) string {
			field := idField(record)
			if field == nil {
				return ""
			}
			return strings.TrimSpace(*field)
		},
	}
}

func connectionRecordID(record *connectionRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func credentialRecordID(record *credentialRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func serviceEventRecordID(record *serviceEventRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func activityEntryRecordID(record *activityEntryRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func grantEventRecordID(record *grantEventRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func grantSnapshotRecordID(record *grantSnapshotRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func subscriptionRecordID(record *subscriptionRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func webhookDeliveryRecordID(record *webhookDeliveryRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func syncCursorRecordID(record *syncCursorRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func installationRecordID(record *installationRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func rateLimitStateRecordID(record *rateLimitStateRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func syncJobRecordID(record *syncJobRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func lifecycleOutboxRecordID(record *lifecycleOutboxRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}
func notificationDispatchRecordID(record *notificationDispatchRecord) *string {
	if record == nil {
		return nil
	}
	return &record.ID
}

func parseUUID(value string) uuid.UUID {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil
	}
	return parsed
}
