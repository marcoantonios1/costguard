package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

func (g *Gateway) isCacheableRequest(r *http.Request, body []byte) bool {
	if r.Method != http.MethodPost {
		return false
	}
	if r.URL.Path != "/v1/chat/completions" {
		return false
	}

	var v struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &v); err == nil && v.Stream {
		return false
	}

	return true
}

func buildCacheKey(
	r *http.Request,
	body []byte,
	requestedProvider string,
	requestedMode string,
	selectedProvider string,
	effectiveModel string,
) string {
	h := sha256.New()

	writeKeyPart(h, r.Method)
	writeKeyPart(h, r.URL.Path)
	writeKeyPart(h, r.URL.RawQuery)

	writeKeyPart(h, strings.TrimSpace(requestedProvider))
	writeKeyPart(h, strings.TrimSpace(strings.ToLower(requestedMode)))
	writeKeyPart(h, strings.TrimSpace(selectedProvider))
	writeKeyPart(h, strings.TrimSpace(effectiveModel))

	_, _ = h.Write(body)

	return hex.EncodeToString(h.Sum(nil))
}

func writeKeyPart(h interface{ Write([]byte) (int, error) }, value string) {
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}

func shortKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12]
}
