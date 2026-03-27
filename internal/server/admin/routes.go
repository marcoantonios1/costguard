package admin

import (
	"net/http"

	"github.com/marcoantonios1/costguard/internal/budget"
	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/report"
	"github.com/marcoantonios1/costguard/internal/usage"
)

type Deps struct {
	UsageStore      usage.Store
	Reports         *report.EmailService
	Log             *logging.Log
	Budget          *budget.Service
	ProviderCatalog *providers.Catalog
}

func Register(mux *http.ServeMux, d Deps) {
	h := NewAdminHandler(d.UsageStore)

	mux.HandleFunc("/usage/summary", h.UsageSummary)
	mux.HandleFunc("/usage/teams", h.UsageTeams)
	mux.HandleFunc("/usage/projects", h.UsageProjects)

	if d.Reports != nil {
		reportsHandler := NewReportsHandler(d.Reports, d.Log)
		mux.HandleFunc("/reports/monthly/send", reportsHandler.SendMonthlyReport)
	}

	if d.Budget != nil {
		budgetHandler := NewBudgetHandler(d.Budget)
		mux.HandleFunc("/budget/status", budgetHandler.Status)
	}

	if d.ProviderCatalog != nil {
		mux.HandleFunc("/providers", ProvidersHandler(d.ProviderCatalog))
	}
}
