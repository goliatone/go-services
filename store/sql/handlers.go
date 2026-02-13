package sqlstore

import (
	"strings"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/google/uuid"
)

func connectionHandlers() repository.ModelHandlers[*connectionRecord] {
	return repository.ModelHandlers[*connectionRecord]{
		NewRecord: func() *connectionRecord {
			return &connectionRecord{}
		},
		GetID: func(record *connectionRecord) uuid.UUID {
			if record == nil {
				return uuid.Nil
			}
			return parseUUID(record.ID)
		},
		SetID: func(record *connectionRecord, id uuid.UUID) {
			if record == nil {
				return
			}
			record.ID = id.String()
		},
		GetIdentifier: func() string {
			return "id"
		},
		GetIdentifierValue: func(record *connectionRecord) string {
			if record == nil {
				return ""
			}
			return strings.TrimSpace(record.ID)
		},
	}
}

func credentialHandlers() repository.ModelHandlers[*credentialRecord] {
	return repository.ModelHandlers[*credentialRecord]{
		NewRecord: func() *credentialRecord {
			return &credentialRecord{}
		},
		GetID: func(record *credentialRecord) uuid.UUID {
			if record == nil {
				return uuid.Nil
			}
			return parseUUID(record.ID)
		},
		SetID: func(record *credentialRecord, id uuid.UUID) {
			if record == nil {
				return
			}
			record.ID = id.String()
		},
		GetIdentifier: func() string {
			return "id"
		},
		GetIdentifierValue: func(record *credentialRecord) string {
			if record == nil {
				return ""
			}
			return strings.TrimSpace(record.ID)
		},
	}
}

func eventHandlers() repository.ModelHandlers[*serviceEventRecord] {
	return repository.ModelHandlers[*serviceEventRecord]{
		NewRecord: func() *serviceEventRecord {
			return &serviceEventRecord{}
		},
		GetID: func(record *serviceEventRecord) uuid.UUID {
			if record == nil {
				return uuid.Nil
			}
			return parseUUID(record.ID)
		},
		SetID: func(record *serviceEventRecord, id uuid.UUID) {
			if record == nil {
				return
			}
			record.ID = id.String()
		},
		GetIdentifier: func() string {
			return "id"
		},
		GetIdentifierValue: func(record *serviceEventRecord) string {
			if record == nil {
				return ""
			}
			return strings.TrimSpace(record.ID)
		},
	}
}

func grantEventHandlers() repository.ModelHandlers[*grantEventRecord] {
	return repository.ModelHandlers[*grantEventRecord]{
		NewRecord: func() *grantEventRecord {
			return &grantEventRecord{}
		},
		GetID: func(record *grantEventRecord) uuid.UUID {
			if record == nil {
				return uuid.Nil
			}
			return parseUUID(record.ID)
		},
		SetID: func(record *grantEventRecord, id uuid.UUID) {
			if record == nil {
				return
			}
			record.ID = id.String()
		},
		GetIdentifier: func() string {
			return "id"
		},
		GetIdentifierValue: func(record *grantEventRecord) string {
			if record == nil {
				return ""
			}
			return strings.TrimSpace(record.ID)
		},
	}
}

func subscriptionHandlers() repository.ModelHandlers[*subscriptionRecord] {
	return repository.ModelHandlers[*subscriptionRecord]{
		NewRecord: func() *subscriptionRecord {
			return &subscriptionRecord{}
		},
		GetID: func(record *subscriptionRecord) uuid.UUID {
			if record == nil {
				return uuid.Nil
			}
			return parseUUID(record.ID)
		},
		SetID: func(record *subscriptionRecord, id uuid.UUID) {
			if record == nil {
				return
			}
			record.ID = id.String()
		},
		GetIdentifier: func() string {
			return "id"
		},
		GetIdentifierValue: func(record *subscriptionRecord) string {
			if record == nil {
				return ""
			}
			return strings.TrimSpace(record.ID)
		},
	}
}

func webhookDeliveryHandlers() repository.ModelHandlers[*webhookDeliveryRecord] {
	return repository.ModelHandlers[*webhookDeliveryRecord]{
		NewRecord: func() *webhookDeliveryRecord {
			return &webhookDeliveryRecord{}
		},
		GetID: func(record *webhookDeliveryRecord) uuid.UUID {
			if record == nil {
				return uuid.Nil
			}
			return parseUUID(record.ID)
		},
		SetID: func(record *webhookDeliveryRecord, id uuid.UUID) {
			if record == nil {
				return
			}
			record.ID = id.String()
		},
		GetIdentifier: func() string {
			return "id"
		},
		GetIdentifierValue: func(record *webhookDeliveryRecord) string {
			if record == nil {
				return ""
			}
			return strings.TrimSpace(record.ID)
		},
	}
}

func syncCursorHandlers() repository.ModelHandlers[*syncCursorRecord] {
	return repository.ModelHandlers[*syncCursorRecord]{
		NewRecord: func() *syncCursorRecord {
			return &syncCursorRecord{}
		},
		GetID: func(record *syncCursorRecord) uuid.UUID {
			if record == nil {
				return uuid.Nil
			}
			return parseUUID(record.ID)
		},
		SetID: func(record *syncCursorRecord, id uuid.UUID) {
			if record == nil {
				return
			}
			record.ID = id.String()
		},
		GetIdentifier: func() string {
			return "id"
		},
		GetIdentifierValue: func(record *syncCursorRecord) string {
			if record == nil {
				return ""
			}
			return strings.TrimSpace(record.ID)
		},
	}
}

func syncJobHandlers() repository.ModelHandlers[*syncJobRecord] {
	return repository.ModelHandlers[*syncJobRecord]{
		NewRecord: func() *syncJobRecord {
			return &syncJobRecord{}
		},
		GetID: func(record *syncJobRecord) uuid.UUID {
			if record == nil {
				return uuid.Nil
			}
			return parseUUID(record.ID)
		},
		SetID: func(record *syncJobRecord, id uuid.UUID) {
			if record == nil {
				return
			}
			record.ID = id.String()
		},
		GetIdentifier: func() string {
			return "id"
		},
		GetIdentifierValue: func(record *syncJobRecord) string {
			if record == nil {
				return ""
			}
			return strings.TrimSpace(record.ID)
		},
	}
}

func parseUUID(value string) uuid.UUID {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil
	}
	return parsed
}
