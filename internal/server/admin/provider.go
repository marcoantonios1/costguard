package admin

import (
	"encoding/json"
	"net/http"

	"github.com/marcoantonios1/costguard/internal/providers"
)

type ProviderCatalogReader interface {
	List() []providers.RuntimeMetadata
}

func ProvidersHandler(catalog ProviderCatalogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providers": catalog.List(),
		})
	}
}
