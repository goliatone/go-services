package embedded

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSessionTokenValidator_ValidToken(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	validator := NewSessionTokenValidator(SessionTokenValidatorConfig{
		AppSecret: "app_secret",
		ClientID:  "client_id",
		Now:       func() time.Time { return now },
	})
	token := signTestJWT(t, "app_secret", map[string]any{
		"iss":  "https://merchant.myshopify.com/admin",
		"dest": "https://merchant.myshopify.com",
		"aud":  "client_id",
		"sub":  "user_1",
		"exp":  now.Add(time.Minute).Unix(),
		"nbf":  now.Add(-time.Minute).Unix(),
		"iat":  now.Add(-30 * time.Second).Unix(),
		"jti":  "jti_1",
	})

	claims, err := validator.ValidateSessionToken(context.Background(), ValidateSessionTokenRequest{
		SessionToken:       token,
		ExpectedShopDomain: "merchant",
	})
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if claims.ShopDomain != "merchant.myshopify.com" {
		t.Fatalf("expected normalized shop domain, got %q", claims.ShopDomain)
	}
	if claims.Audience != "client_id" {
		t.Fatalf("expected audience client_id, got %q", claims.Audience)
	}
}

func TestSessionTokenValidator_BadSignature(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	validator := NewSessionTokenValidator(SessionTokenValidatorConfig{
		AppSecret: "expected_secret",
		ClientID:  "client_id",
		Now:       func() time.Time { return now },
	})
	token := signTestJWT(t, "different_secret", validClaims(now, "client_id"))

	if _, err := validator.ValidateSessionToken(context.Background(), ValidateSessionTokenRequest{
		SessionToken: token,
	}); err == nil {
		t.Fatalf("expected bad signature validation error")
	}
}

func TestSessionTokenValidator_BadAudience(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	validator := NewSessionTokenValidator(SessionTokenValidatorConfig{
		AppSecret: "app_secret",
		ClientID:  "client_id",
		Now:       func() time.Time { return now },
	})
	claims := validClaims(now, "different_client")
	token := signTestJWT(t, "app_secret", claims)

	if _, err := validator.ValidateSessionToken(context.Background(), ValidateSessionTokenRequest{
		SessionToken: token,
	}); err == nil {
		t.Fatalf("expected bad audience validation error")
	}
}

func TestSessionTokenValidator_ExpiredToken(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	validator := NewSessionTokenValidator(SessionTokenValidatorConfig{
		AppSecret: "app_secret",
		ClientID:  "client_id",
		Now:       func() time.Time { return now },
	})
	claims := validClaims(now, "client_id")
	claims["exp"] = now.Add(-2 * time.Minute).Unix()
	token := signTestJWT(t, "app_secret", claims)

	if _, err := validator.ValidateSessionToken(context.Background(), ValidateSessionTokenRequest{
		SessionToken: token,
	}); err == nil {
		t.Fatalf("expected expired token validation error")
	}
}

func TestSessionTokenValidator_NotBeforeInFuture(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	validator := NewSessionTokenValidator(SessionTokenValidatorConfig{
		AppSecret: "app_secret",
		ClientID:  "client_id",
		Now:       func() time.Time { return now },
	})
	claims := validClaims(now, "client_id")
	claims["nbf"] = now.Add(3 * time.Minute).Unix()
	token := signTestJWT(t, "app_secret", claims)

	if _, err := validator.ValidateSessionToken(context.Background(), ValidateSessionTokenRequest{
		SessionToken: token,
	}); err == nil {
		t.Fatalf("expected token with future nbf to fail")
	}
}

func TestSessionTokenValidator_InvalidDestOrIssFormat(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	validator := NewSessionTokenValidator(SessionTokenValidatorConfig{
		AppSecret: "app_secret",
		ClientID:  "client_id",
		Now:       func() time.Time { return now },
	})
	testCases := []struct {
		name   string
		claims map[string]any
	}{
		{
			name: "invalid_dest",
			claims: map[string]any{
				"iss":  "https://merchant.myshopify.com/admin",
				"dest": "merchant.myshopify.com",
				"aud":  "client_id",
				"exp":  now.Add(time.Minute).Unix(),
				"nbf":  now.Add(-time.Minute).Unix(),
				"iat":  now.Add(-30 * time.Second).Unix(),
				"jti":  "jti_dest",
			},
		},
		{
			name: "invalid_iss",
			claims: map[string]any{
				"iss":  "merchant.myshopify.com/admin",
				"dest": "https://merchant.myshopify.com",
				"aud":  "client_id",
				"exp":  now.Add(time.Minute).Unix(),
				"nbf":  now.Add(-time.Minute).Unix(),
				"iat":  now.Add(-30 * time.Second).Unix(),
				"jti":  "jti_iss",
			},
		},
		{
			name: "dest_host_missing_shopify_suffix",
			claims: map[string]any{
				"iss":  "https://merchant.myshopify.com/admin",
				"dest": "https://merchant",
				"aud":  "client_id",
				"exp":  now.Add(time.Minute).Unix(),
				"nbf":  now.Add(-time.Minute).Unix(),
				"iat":  now.Add(-30 * time.Second).Unix(),
				"jti":  "jti_dest_suffix",
			},
		},
		{
			name: "iss_invalid_path",
			claims: map[string]any{
				"iss":  "https://merchant.myshopify.com/",
				"dest": "https://merchant.myshopify.com",
				"aud":  "client_id",
				"exp":  now.Add(time.Minute).Unix(),
				"nbf":  now.Add(-time.Minute).Unix(),
				"iat":  now.Add(-30 * time.Second).Unix(),
				"jti":  "jti_iss_path",
			},
		},
		{
			name: "dest_invalid_path",
			claims: map[string]any{
				"iss":  "https://merchant.myshopify.com/admin",
				"dest": "https://merchant.myshopify.com/admin",
				"aud":  "client_id",
				"exp":  now.Add(time.Minute).Unix(),
				"nbf":  now.Add(-time.Minute).Unix(),
				"iat":  now.Add(-30 * time.Second).Unix(),
				"jti":  "jti_dest_path",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			token := signTestJWT(t, "app_secret", tc.claims)
			if _, err := validator.ValidateSessionToken(context.Background(), ValidateSessionTokenRequest{
				SessionToken: token,
			}); err == nil {
				t.Fatalf("expected invalid format token to fail for %s", tc.name)
			}
		})
	}
}

func TestSessionTokenValidator_MissingJTI(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	validator := NewSessionTokenValidator(SessionTokenValidatorConfig{
		AppSecret: "app_secret",
		ClientID:  "client_id",
		Now:       func() time.Time { return now },
	})
	claims := validClaims(now, "client_id")
	delete(claims, "jti")
	token := signTestJWT(t, "app_secret", claims)

	if _, err := validator.ValidateSessionToken(context.Background(), ValidateSessionTokenRequest{
		SessionToken: token,
	}); err == nil {
		t.Fatalf("expected missing jti to fail validation")
	}
}

func TestSessionTokenValidator_UnsupportedAlgorithm(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	validator := NewSessionTokenValidator(SessionTokenValidatorConfig{
		AppSecret: "app_secret",
		ClientID:  "client_id",
		Now:       func() time.Time { return now },
	})
	token := signTestJWTWithHeader(
		t,
		"app_secret",
		map[string]any{"alg": "HS512", "typ": "JWT"},
		validClaims(now, "client_id"),
	)

	if _, err := validator.ValidateSessionToken(context.Background(), ValidateSessionTokenRequest{
		SessionToken: token,
	}); err == nil {
		t.Fatalf("expected unsupported algorithm to fail validation")
	}
}

func validClaims(now time.Time, audience string) map[string]any {
	return map[string]any{
		"iss":  "https://merchant.myshopify.com/admin",
		"dest": "https://merchant.myshopify.com",
		"aud":  audience,
		"exp":  now.Add(time.Minute).Unix(),
		"nbf":  now.Add(-time.Minute).Unix(),
		"iat":  now.Add(-30 * time.Second).Unix(),
		"jti":  "jti_123",
	}
}

func signTestJWT(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	return signTestJWTWithHeader(t, secret, map[string]any{"alg": "HS256", "typ": "JWT"}, claims)
}

func signTestJWTWithHeader(t *testing.T, secret string, header map[string]any, claims map[string]any) string {
	t.Helper()
	headerRaw, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	claimsRaw, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	headerPart := base64.RawURLEncoding.EncodeToString(headerRaw)
	claimsPart := base64.RawURLEncoding.EncodeToString(claimsRaw)
	signingInput := headerPart + "." + claimsPart

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return strings.Join([]string{headerPart, claimsPart, signature}, ".")
}
