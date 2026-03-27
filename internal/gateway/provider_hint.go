package gateway

import (
	"net/http"
	"strings"
)

const HeaderProviderHint = "X-Costguard-Provider"

func requestedProviderHint(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get(HeaderProviderHint))
}
