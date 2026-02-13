package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func buildHS256JWT(keyID string, secret string, claims map[string]any) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", fmt.Errorf("auth: jwt signing secret is required")
	}
	header := map[string]any{
		"alg": "HS256",
		"typ": "JWT",
	}
	if strings.TrimSpace(keyID) != "" {
		header["kid"] = strings.TrimSpace(keyID)
	}

	headerRaw, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("auth: marshal jwt header: %w", err)
	}
	claimsRaw, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal jwt claims: %w", err)
	}

	headerToken := base64.RawURLEncoding.EncodeToString(headerRaw)
	claimsToken := base64.RawURLEncoding.EncodeToString(claimsRaw)
	signed := headerToken + "." + claimsToken

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signed))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signed + "." + signature, nil
}
