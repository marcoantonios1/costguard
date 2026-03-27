package gateway

import (
	"net/http"
	"strings"
)

const HeaderModeHint = "X-Costguard-Mode"

func requestedModeHint(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(r.Header.Get(HeaderModeHint)))
}

func isSupportedMode(mode string) bool {
	switch mode {
	case "cheap", "balanced", "best", "private":
		return true
	default:
		return false
	}
}
