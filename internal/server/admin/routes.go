package admin

import (
	"net/http"

	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/report"
	"github.com/marcoantonios1/costguard/internal/usage"
)

type Deps struct {
	UsageStore usage.Store
	Reports    *report.EmailService
	Log        *logging.Log
}

func Register(mux *http.ServeMux, d Deps) {
	h := NewAdminHandler(d.UsageStore)

	mux.HandleFunc("/admin/usage/summary", h.UsageSummary)
	mux.HandleFunc("/admin/usage/teams", h.UsageTeams)

	if d.Reports != nil {
		reportsHandler := NewReportsHandler(d.Reports, d.Log)
		mux.HandleFunc("/admin/reports/monthly/send", reportsHandler.SendMonthlyReport)
	}
}
