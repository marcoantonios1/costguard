package admin

import (
	"net/http"

	"github.com/marcoantonios1/costguard/internal/usage"
)

type Deps struct {
	UsageStore usage.Store
}

func Register(mux *http.ServeMux, d Deps) {
	h := NewAdminHandler(d.UsageStore)

	mux.HandleFunc("/admin/usage/summary", h.UsageSummary)
	mux.HandleFunc("/admin/usage/teams", h.UsageTeams)
	
}
