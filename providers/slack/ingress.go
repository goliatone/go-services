package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func (p *Provider) Verify(ctx context.Context, input HTTPRequest) (Event, error) {
	if len(input.Body) == 0 || len(input.Body) > MaxEventBytes || p.config.SigningSecrets == nil {
		return Event{}, ErrInvalid
	}
	timestamp, err := strconv.ParseInt(input.Timestamp, 10, 64)
	if err != nil {
		return Event{}, ErrDenied
	}
	delta := p.config.Now().Sub(time.Unix(timestamp, 0))
	if delta > 5*time.Minute || delta < -5*time.Minute {
		return Event{}, ErrDenied
	}
	secret, err := p.config.SigningSecrets.ResolveSlackSigningSecret(ctx, input.ConnectionID)
	if err != nil || len(secret) == 0 {
		return Event{}, ErrDenied
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("v0:" + input.Timestamp + ":"))
	mac.Write(input.Body)
	provided, err := hex.DecodeString(strings.TrimPrefix(input.Signature, "v0="))
	if err != nil || !strings.HasPrefix(input.Signature, "v0=") || !hmac.Equal(provided, mac.Sum(nil)) {
		return Event{}, ErrDenied
	}
	return p.Normalize(input.Body)
}
func (p *Provider) Normalize(body []byte) (Event, error) {
	var envelope struct {
		Type      string
		TeamID    string `json:"team_id"`
		AppID     string `json:"api_app_id"`
		EventID   string `json:"event_id"`
		Challenge string
		Event     json.RawMessage
	}
	if len(body) > MaxEventBytes || json.Unmarshal(body, &envelope) != nil {
		return Event{}, ErrInvalid
	}
	if p.config.AppID != "" && envelope.AppID != p.config.AppID {
		return Event{}, ErrDenied
	}
	if envelope.Type == "url_verification" {
		if envelope.Challenge == "" || len(envelope.Challenge) > 4096 {
			return Event{}, ErrInvalid
		}
		return Event{Challenge: envelope.Challenge}, nil
	}
	if envelope.Type != "event_callback" {
		return Event{Ignored: true}, nil
	}
	var event struct {
		Type, Subtype, Channel, User, Text string
		TS                                 string `json:"ts"`
		Thread                             string `json:"thread_ts"`
		EventTS                            string `json:"event_ts"`
		DeletedTS                          string `json:"deleted_ts"`
		BotID                              string `json:"bot_id"`
		Message                            struct {
			TS, Text, User string
			Thread         string `json:"thread_ts"`
			BotID          string `json:"bot_id"`
			Edited         struct{ TS string }
		} `json:"message"`
		Previous struct {
			TS, Text, User string
			Thread         string `json:"thread_ts"`
			BotID          string `json:"bot_id"`
		} `json:"previous_message"`
	}
	if json.Unmarshal(envelope.Event, &event) != nil {
		return Event{}, ErrInvalid
	}
	if event.Type != "message" {
		return Event{Ignored: true}, nil
	}
	result := Event{DeliveryID: envelope.EventID, TeamID: envelope.TeamID, AppID: envelope.AppID, ChannelID: event.Channel, ThreadID: event.Thread, MessageID: event.TS, AuthorID: event.User, BotID: event.BotID, Body: event.Text, Revision: event.EventTS}
	switch event.Subtype {
	case "", "bot_message", "thread_broadcast", "file_share", "me_message":
	case "message_changed":
		result.MessageID, result.ThreadID, result.AuthorID, result.BotID, result.Body = event.Message.TS, event.Message.Thread, event.Message.User, event.Message.BotID, event.Message.Text
		if event.Message.Edited.TS != "" {
			result.Revision = event.Message.Edited.TS
		}
	case "message_deleted":
		result.MessageID, result.ThreadID, result.AuthorID, result.BotID, result.Body, result.Deleted = event.DeletedTS, event.Previous.Thread, event.Previous.User, event.Previous.BotID, "", true
	default:
		return Event{Ignored: true}, nil
	}
	if result.ThreadID == "" {
		result.ThreadID = result.MessageID
	}
	if result.MessageID != result.ThreadID {
		result.ParentID = result.ThreadID
	}
	if result.Revision == "" {
		sum := sha256.Sum256(envelope.Event)
		result.Revision = hex.EncodeToString(sum[:])
	}
	if result.DeliveryID == "" || result.TeamID == "" || result.ChannelID == "" || result.MessageID == "" || (!result.Deleted && result.AuthorID == "" && result.BotID == "") {
		return Event{}, ErrInvalid
	}
	result.Oversized = len(result.Body) > MaxTextBytes
	return result, nil
}
