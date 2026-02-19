package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAWSSigV4Mode              = "header"
	defaultAWSSigV4QueryExpiry       = 5 * time.Minute
	defaultAWSSigV4AccessTokenHeader = "x-amz-access-token"
)

type AWSSigV4Signer struct {
	AccessKeyID       string
	SecretAccessKey   string
	SessionToken      string
	Region            string
	Service           string
	Mode              string
	QueryExpiry       time.Duration
	UnsignedPayload   bool
	AccessTokenHeader string
	Now               func() time.Time
}

type awsSigV4Profile struct {
	AccessKeyID       string
	SecretAccessKey   string
	SessionToken      string
	Region            string
	Service           string
	Mode              string
	QueryExpiry       time.Duration
	UnsignedPayload   bool
	AccessTokenHeader string
	Now               func() time.Time
}

func (s AWSSigV4Signer) Sign(_ context.Context, req *http.Request, cred ActiveCredential) error {
	if req == nil {
		return fmt.Errorf("core: http request is required")
	}
	profile, err := s.resolveProfile(cred)
	if err != nil {
		return err
	}
	now := profile.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	if profile.Mode == "query" {
		return signAWSSigV4Query(req, cred, profile, amzDate, dateStamp)
	}
	return signAWSSigV4Header(req, cred, profile, amzDate, dateStamp)
}

func (s AWSSigV4Signer) resolveProfile(cred ActiveCredential) (awsSigV4Profile, error) {
	metadata := cred.Metadata
	mode := firstNonEmpty(
		strings.TrimSpace(strings.ToLower(s.Mode)),
		strings.TrimSpace(strings.ToLower(metadataString(metadata, "aws_signing_mode", "signing_mode"))),
		defaultAWSSigV4Mode,
	)
	if mode != "header" && mode != "query" {
		return awsSigV4Profile{}, fmt.Errorf("core: unsupported aws sigv4 signing mode %q", mode)
	}

	accessKeyID := firstNonEmpty(
		strings.TrimSpace(s.AccessKeyID),
		metadataString(metadata, "aws_access_key_id", "access_key_id"),
	)
	secretAccessKey := firstNonEmpty(
		strings.TrimSpace(s.SecretAccessKey),
		metadataString(metadata, "aws_secret_access_key", "secret_access_key"),
	)
	region := firstNonEmpty(
		strings.TrimSpace(s.Region),
		metadataString(metadata, "aws_region", "region"),
	)
	service := firstNonEmpty(
		strings.TrimSpace(s.Service),
		metadataString(metadata, "aws_service", "service"),
	)
	if accessKeyID == "" || secretAccessKey == "" || region == "" || service == "" {
		return awsSigV4Profile{}, fmt.Errorf("core: aws sigv4 requires access key, secret, region, and service")
	}

	sessionToken := firstNonEmpty(
		strings.TrimSpace(s.SessionToken),
		metadataString(metadata, "aws_session_token", "session_token"),
	)
	accessTokenHeader := firstNonEmpty(
		strings.TrimSpace(strings.ToLower(s.AccessTokenHeader)),
		strings.TrimSpace(strings.ToLower(metadataString(metadata, "aws_access_token_header"))),
	)
	if accessTokenHeader == "" && strings.TrimSpace(cred.AccessToken) != "" {
		accessTokenHeader = defaultAWSSigV4AccessTokenHeader
	}

	queryExpiry := s.QueryExpiry
	if queryExpiry <= 0 {
		if raw := metadataString(metadata, "aws_signing_expires", "aws_query_expires"); raw != "" {
			seconds, err := strconv.Atoi(strings.TrimSpace(raw))
			if err == nil && seconds > 0 {
				queryExpiry = time.Duration(seconds) * time.Second
			}
		}
	}
	if queryExpiry <= 0 {
		queryExpiry = defaultAWSSigV4QueryExpiry
	}
	unsignedPayload := s.UnsignedPayload || metadataBool(metadata, "aws_unsigned_payload")
	now := s.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	return awsSigV4Profile{
		AccessKeyID:       accessKeyID,
		SecretAccessKey:   secretAccessKey,
		SessionToken:      sessionToken,
		Region:            region,
		Service:           service,
		Mode:              mode,
		QueryExpiry:       queryExpiry,
		UnsignedPayload:   unsignedPayload,
		AccessTokenHeader: accessTokenHeader,
		Now:               now,
	}, nil
}

func signAWSSigV4Header(
	req *http.Request,
	cred ActiveCredential,
	profile awsSigV4Profile,
	amzDate string,
	dateStamp string,
) error {
	req.Header.Del("Authorization")
	req.Header.Set("X-Amz-Date", amzDate)

	payloadHash := "UNSIGNED-PAYLOAD"
	if !profile.UnsignedPayload {
		hash, err := readRequestBodyHash(req)
		if err != nil {
			return err
		}
		payloadHash = hash
	}
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	if profile.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", profile.SessionToken)
	}
	if profile.AccessTokenHeader != "" && strings.TrimSpace(cred.AccessToken) != "" {
		req.Header.Set(profile.AccessTokenHeader, strings.TrimSpace(cred.AccessToken))
	}

	canonicalHeaders, signedHeaders := canonicalHeaderBlock(req.Header, req.URL.Host)
	canonicalQuery := canonicalQueryString(req.URL.Query(), false)
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(req.Method)),
		canonicalURI(req.URL),
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	credentialScope := dateStamp + "/" + profile.Region + "/" + profile.Service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := awsSigV4Signature(profile.SecretAccessKey, dateStamp, profile.Region, profile.Service, stringToSign)
	authorization := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		profile.AccessKeyID,
		credentialScope,
		signedHeaders,
		signature,
	)
	req.Header.Set("Authorization", authorization)
	return nil
}

func signAWSSigV4Query(
	req *http.Request,
	cred ActiveCredential,
	profile awsSigV4Profile,
	amzDate string,
	dateStamp string,
) error {
	query := req.URL.Query()
	credentialScope := dateStamp + "/" + profile.Region + "/" + profile.Service + "/aws4_request"
	signedHeaders := "host"
	expires := int(profile.QueryExpiry.Seconds())
	if expires <= 0 {
		expires = int(defaultAWSSigV4QueryExpiry.Seconds())
	}
	if expires > 604800 {
		expires = 604800
	}

	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", profile.AccessKeyID+"/"+credentialScope)
	query.Set("X-Amz-Date", amzDate)
	query.Set("X-Amz-Expires", strconv.Itoa(expires))
	query.Set("X-Amz-SignedHeaders", signedHeaders)
	if profile.SessionToken != "" {
		query.Set("X-Amz-Security-Token", profile.SessionToken)
	}
	if profile.AccessTokenHeader != "" && strings.TrimSpace(cred.AccessToken) != "" {
		req.Header.Set(profile.AccessTokenHeader, strings.TrimSpace(cred.AccessToken))
	}
	query.Del("X-Amz-Signature")

	payloadHash := "UNSIGNED-PAYLOAD"
	if !profile.UnsignedPayload {
		hash, err := readRequestBodyHash(req)
		if err != nil {
			return err
		}
		payloadHash = hash
	}
	canonicalQuery := canonicalQueryString(query, true)
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(req.Method)),
		canonicalURI(req.URL),
		canonicalQuery,
		"host:" + strings.ToLower(strings.TrimSpace(req.URL.Host)) + "\n",
		signedHeaders,
		payloadHash,
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := awsSigV4Signature(profile.SecretAccessKey, dateStamp, profile.Region, profile.Service, stringToSign)
	query.Set("X-Amz-Signature", signature)
	req.URL.RawQuery = canonicalQueryString(query, false)
	req.Header.Del("Authorization")
	return nil
}

func canonicalURI(requestURL *url.URL) string {
	if requestURL == nil {
		return "/"
	}
	path := requestURL.EscapedPath()
	if path == "" {
		path = "/"
	}
	return path
}

func canonicalHeaderBlock(headers http.Header, host string) (string, string) {
	normalized := map[string]string{
		"host": strings.ToLower(strings.TrimSpace(host)),
	}
	for key, values := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "" || lower == "authorization" {
			continue
		}
		cleaned := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			cleaned = append(cleaned, compressSpaces(trimmed))
		}
		if len(cleaned) == 0 {
			continue
		}
		normalized[lower] = strings.Join(cleaned, ",")
	}

	keys := make([]string, 0, len(normalized))
	for key := range normalized {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString(":")
		b.WriteString(normalized[key])
		b.WriteByte('\n')
	}
	return b.String(), strings.Join(keys, ";")
}

func canonicalQueryString(query url.Values, escape bool) string {
	if len(query) == 0 {
		return ""
	}
	type entry struct {
		key   string
		value string
	}
	values := make([]entry, 0, len(query))
	for key, list := range query {
		encodedKey := awsQueryEscape(key)
		if len(list) == 0 {
			values = append(values, entry{key: encodedKey, value: ""})
			continue
		}
		for _, value := range list {
			values = append(values, entry{
				key:   encodedKey,
				value: awsQueryEscape(value),
			})
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].key == values[j].key {
			return values[i].value < values[j].value
		}
		return values[i].key < values[j].key
	})

	pairs := make([]string, 0, len(values))
	for _, value := range values {
		pairs = append(pairs, value.key+"="+value.value)
	}
	if escape {
		return strings.Join(pairs, "&")
	}
	return strings.Join(pairs, "&")
}

func awsQueryEscape(value string) string {
	escaped := url.QueryEscape(value)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func awsSigV4Signature(
	secretAccessKey string,
	dateStamp string,
	region string,
	service string,
	stringToSign string,
) string {
	kDate := hmacSHA256([]byte("AWS4"+secretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hmacSHA256(kSigning, stringToSign)
	return hex.EncodeToString(signature)
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func metadataString(metadata map[string]any, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		value, ok := metadata[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		case AuthKind:
			if trimmed := strings.TrimSpace(string(typed)); trimmed != "" {
				return trimmed
			}
		case []byte:
			if trimmed := strings.TrimSpace(string(typed)); trimmed != "" {
				return trimmed
			}
		case fmt.Stringer:
			if trimmed := strings.TrimSpace(typed.String()); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func metadataBool(metadata map[string]any, key string) bool {
	if len(metadata) == 0 || strings.TrimSpace(key) == "" {
		return false
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	}
	return false
}

func compressSpaces(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
