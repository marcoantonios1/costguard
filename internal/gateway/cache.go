package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
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

func buildCacheKey(r *http.Request, body []byte) string {
	sum := sha256.Sum256([]byte(r.Method + "\n" + r.URL.Path + "\n" + string(body)))
	return hex.EncodeToString(sum[:])
}

func shortKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12]
}