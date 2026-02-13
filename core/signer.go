package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type ProviderSigner interface {
	Signer() Signer
}

type BearerTokenSigner struct{}

func (BearerTokenSigner) Sign(_ context.Context, req *http.Request, cred ActiveCredential) error {
	if req == nil {
		return fmt.Errorf("core: http request is required")
	}
	token := strings.TrimSpace(cred.AccessToken)
	if token == "" {
		return fmt.Errorf("core: access token is required for bearer signing")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func (s *Service) resolveSignerForProvider(provider Provider) Signer {
	if provider != nil {
		if signerProvider, ok := provider.(ProviderSigner); ok {
			if signer := signerProvider.Signer(); signer != nil {
				return signer
			}
		}
	}
	return s.signer
}

func (s *Service) SignRequest(
	ctx context.Context,
	providerID string,
	connectionID string,
	req *http.Request,
	cred *ActiveCredential,
) error {
	if s == nil {
		return fmt.Errorf("core: service is nil")
	}
	provider, err := s.resolveProvider(providerID)
	if err != nil {
		return err
	}
	signer := s.resolveSignerForProvider(provider)
	if signer == nil {
		return s.mapError(fmt.Errorf("core: signer is not configured"))
	}

	active := ActiveCredential{}
	if cred != nil {
		active = *cred
	} else if s.credentialStore != nil {
		stored, loadErr := s.credentialStore.GetActiveByConnection(ctx, connectionID)
		if loadErr != nil {
			return s.mapError(loadErr)
		}
		active = credentialToActive(stored)
	} else {
		return s.mapError(fmt.Errorf("core: credential is required for signing"))
	}

	if signErr := signer.Sign(ctx, req, active); signErr != nil {
		return s.mapError(signErr)
	}
	return nil
}
