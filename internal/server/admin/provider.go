package admin

import (
	"encoding/json"
	"net/http"

	"github.com/marcoantonios1/costguard/internal/providers"
)

type ProviderCatalogReader interface {
	List() []providers.RuntimeMetadata
}

type ProviderCatalogGetter interface {
	Get(name string) (providers.RuntimeMetadata, bool)
}

func ProvidersHandler(catalog ProviderCatalogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providers": catalog.List(),
		})
	}
}

// ProviderModelsHandler handles GET /admin/providers/{name}/models and returns
// the declared SupportedModels for the named provider. An empty slice means the
// provider is unconstrained (accepts all models).
func ProviderModelsHandler(catalog ProviderCatalogGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		md, ok := catalog.Get(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		models := md.SupportedModels
		if models == nil {
			models = []string{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": models,
		})
	}
}
