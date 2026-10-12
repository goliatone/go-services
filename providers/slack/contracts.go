package slack

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/goliatone/go-services/core"
)

const (
	ProviderID    = "slack"
	MaxTextBytes  = 8 << 10
	MaxEventBytes = 1 << 20
	Sent          = "sent"
	Unsent        = "unsent"
	RateLimited   = "rate_limited"
	Unknown       = "unknown"
)

var (
	ErrInvalid     = errors.New("slack: invalid_input")
	ErrDenied      = errors.New("slack: permission_denied")
	ErrUnavailable = errors.New("slack: capability_unavailable")
)

type CredentialResolver interface {
	ResolveSlackCredential(context.Context, string) (core.ActiveCredential, error)
}
type CredentialResolverFunc func(context.Context, string) (core.ActiveCredential, error)

func (f CredentialResolverFunc) ResolveSlackCredential(ctx context.Context, id string) (core.ActiveCredential, error) {
	return f(ctx, id)
}

type SigningSecretResolver interface {
	ResolveSlackSigningSecret(context.Context, string) ([]byte, error)
}
type SigningSecretResolverFunc func(context.Context, string) ([]byte, error)

func (f SigningSecretResolverFunc) ResolveSlackSigningSecret(ctx context.Context, id string) ([]byte, error) {
	return f(ctx, id)
}

type Installation struct {
	TeamID, TeamLabel, EnterpriseID, BotID, BotUserID string
	Grants                                            []string
}
type Channel struct{ ID, Label string }
type ChannelRequest struct {
	ConnectionID, Cursor string
	Limit                int
}
type ChannelPage struct {
	Channels   []Channel
	NextCursor string
}
type MessageIdentity struct{ TeamID, ChannelID, ThreadID, MessageID, URL string }
type MessageRequest struct{ OperationID, ConnectionID, TeamID, ChannelID, ThreadID, Body, CausationID string }

func (r MessageRequest) Validate() error {
	for _, value := range []string{r.OperationID, r.ConnectionID, r.TeamID, r.ChannelID, r.Body} {
		if strings.TrimSpace(value) == "" {
			return ErrInvalid
		}
	}
	if !utf8.ValidString(r.Body) || len(r.Body) > MaxTextBytes {
		return ErrInvalid
	}
	return nil
}

type MessageReceipt struct {
	OperationID, State, Reason string
	External                   *MessageIdentity
	RetryAt                    *time.Time
}

func (r MessageReceipt) ValidateFor(i MessageRequest) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if r.OperationID != i.OperationID {
		return ErrDenied
	}
	switch r.State {
	case Sent:
		if r.External == nil || r.External.TeamID != i.TeamID || r.External.ChannelID != i.ChannelID || (i.ThreadID != "" && r.External.ThreadID != i.ThreadID) || (i.ThreadID == "" && r.External.ThreadID != r.External.MessageID) || strings.TrimSpace(r.External.MessageID) == "" || r.RetryAt != nil {
			return ErrDenied
		}
	case Unsent, Unknown:
		if r.External != nil || r.RetryAt != nil || r.Reason == "" {
			return ErrInvalid
		}
	case RateLimited:
		if r.External != nil || r.RetryAt == nil || r.RetryAt.IsZero() || r.Reason == "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type HTTPRequest struct {
	ConnectionID         string
	Body                 []byte
	Signature, Timestamp string
}
type Event struct {
	DeliveryID, TeamID, AppID, ChannelID, ThreadID, MessageID, AuthorID, BotID, ParentID, Revision, Body, Challenge string
	Deleted, Ignored, Oversized                                                                                     bool
}
type Messaging interface {
	Inspect(context.Context, string) (Installation, error)
	Channels(context.Context, ChannelRequest) (ChannelPage, error)
	Send(context.Context, MessageRequest) (MessageReceipt, error)
	Reconcile(context.Context, MessageRequest) (MessageReceipt, error)
	Verify(context.Context, HTTPRequest) (Event, error)
}
