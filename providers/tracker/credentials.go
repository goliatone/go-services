package tracker

import (
	"context"
	"errors"
	"strings"

	"github.com/goliatone/go-services/core"
)

type StoredCredentialResolver struct {
	Credentials core.CredentialStore
	Secrets     core.SecretProvider
	Codec       core.CredentialCodec
}

func (r StoredCredentialResolver) ResolveTrackerCredential(ctx context.Context, connectionID string) (core.ActiveCredential, error) {
	if r.Credentials == nil || r.Secrets == nil {
		return core.ActiveCredential{}, errors.New("tracker: credential store and secret provider are required")
	}
	connectionID = strings.TrimSpace(connectionID)
	if connectionID == "" {
		return core.ActiveCredential{}, errors.New("tracker: connection id is required")
	}
	stored, err := r.Credentials.GetActiveByConnection(ctx, connectionID)
	if err != nil {
		return core.ActiveCredential{}, err
	}
	if stored.Status != core.CredentialStatusActive {
		return core.ActiveCredential{}, errors.New("tracker: active credential is revoked")
	}
	plaintext, err := r.Secrets.Decrypt(ctx, stored.EncryptedPayload)
	if err != nil {
		return core.ActiveCredential{}, err
	}
	codec := r.Codec
	if codec == nil {
		codec = core.JSONCredentialCodec{}
	}
	credential, err := codec.Decode(plaintext)
	if err != nil {
		return core.ActiveCredential{}, err
	}
	credential.ConnectionID = connectionID
	return credential, nil
}

var _ core.TrackerCredentialResolver = StoredCredentialResolver{}
