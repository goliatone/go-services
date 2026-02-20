package embedded

import (
	"fmt"
	"strings"

	"github.com/goliatone/go-services/core"
)

func resolveRequestedTokenType(
	value core.EmbeddedRequestedTokenType,
) (core.EmbeddedRequestedTokenType, string, error) {
	normalized := strings.ToLower(strings.TrimSpace(string(value)))
	switch normalized {
	case "":
		return core.EmbeddedRequestedTokenTypeOffline, requestedTypeOfflineURN, nil
	case string(core.EmbeddedRequestedTokenTypeOffline):
		return core.EmbeddedRequestedTokenTypeOffline, requestedTypeOfflineURN, nil
	case string(core.EmbeddedRequestedTokenTypeOnline):
		return core.EmbeddedRequestedTokenTypeOnline, requestedTypeOnlineURN, nil
	default:
		return "", "", fmt.Errorf("%w: %q", ErrInvalidRequestedTokenType, value)
	}
}
