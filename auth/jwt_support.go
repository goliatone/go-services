package auth

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
)

const (
	jwtAlgHS256 = "HS256"
	jwtAlgRS256 = "RS256"
)

func buildJWT(keyID string, algorithm string, signingKey string, claims map[string]any) (string, error) {
	normalizedAlgorithm, err := normalizeJWTAlgorithm(algorithm)
	if err != nil {
		return "", err
	}
	signingKey = strings.TrimSpace(signingKey)
	if signingKey == "" {
		return "", fmt.Errorf("auth: jwt signing key is required")
	}
	header := map[string]any{
		"alg": normalizedAlgorithm,
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

	signature, err := signJWT(normalizedAlgorithm, signingKey, signed)
	if err != nil {
		return "", err
	}
	return signed + "." + signature, nil
}

func normalizeJWTAlgorithm(algorithm string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(algorithm))
	if normalized == "" {
		normalized = jwtAlgRS256
	}
	switch normalized {
	case jwtAlgHS256, jwtAlgRS256:
		return normalized, nil
	default:
		return "", fmt.Errorf("auth: unsupported jwt signing algorithm %q", normalized)
	}
}

func signJWT(algorithm string, signingKey string, payload string) (string, error) {
	switch algorithm {
	case jwtAlgHS256:
		mac := hmac.New(sha256.New, []byte(signingKey))
		_, _ = mac.Write([]byte(payload))
		return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
	case jwtAlgRS256:
		privateKey, err := parseRSAPrivateKey(signingKey)
		if err != nil {
			return "", err
		}
		hashed := sha256.Sum256([]byte(payload))
		signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
		if err != nil {
			return "", fmt.Errorf("auth: sign jwt with rs256: %w", err)
		}
		return base64.RawURLEncoding.EncodeToString(signature), nil
	default:
		return "", fmt.Errorf("auth: unsupported jwt signing algorithm %q", algorithm)
	}
}

func parseRSAPrivateKey(signingKey string) (*rsa.PrivateKey, error) {
	normalized := strings.TrimSpace(signingKey)
	normalized = strings.ReplaceAll(normalized, "\\n", "\n")
	if normalized == "" {
		return nil, fmt.Errorf("auth: rsa private key is required")
	}
	block, _ := pem.Decode([]byte(normalized))
	if block == nil {
		return nil, fmt.Errorf("auth: invalid rsa private key pem")
	}
	if x509.IsEncryptedPEMBlock(block) {
		return nil, fmt.Errorf("auth: encrypted rsa private key pem is not supported")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	pkcs8, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: parse rsa private key: %w", err)
	}
	privateKey, ok := pkcs8.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("auth: parsed private key is not rsa")
	}
	return privateKey, nil
}
