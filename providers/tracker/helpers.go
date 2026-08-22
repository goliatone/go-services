package tracker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/goliatone/go-services/core"
)

// Endpoint resolves a provider endpoint from non-secret credential metadata.
// A configured endpoint must be HTTPS so connection configuration cannot turn
// provider calls into an arbitrary local-network transport.
func Endpoint(credential core.ActiveCredential, metadataKey, fallback string) (*url.URL, error) {
	value := strings.TrimSpace(fallback)
	if credential.Metadata != nil {
		if configured, ok := credential.Metadata[metadataKey].(string); ok && strings.TrimSpace(configured) != "" {
			value = strings.TrimSpace(configured)
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, fmt.Sprintf("invalid %s endpoint", metadataKey), false, 0, err)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func Page(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil || page < 1 {
		return 0, core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, "tracker cursor is invalid", false, 0, err)
	}
	return page, nil
}

func Offset(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil || offset < 0 {
		return 0, core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, "tracker cursor is invalid", false, 0, err)
	}
	return offset, nil
}

func Limit(value, fallback, maximum int) int {
	if value <= 0 {
		value = fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func Revision(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:16])
}

func RawJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}
