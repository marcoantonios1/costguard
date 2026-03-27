package admin

import (
	"encoding/json"
	"net/http"
)

func ModesHandler(modeToProvider map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"modes": modeToProvider,
		})
	}
}
