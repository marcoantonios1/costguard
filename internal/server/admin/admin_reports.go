package admin

import (
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/report"
)

type ReportsHandler struct {
	reports *report.EmailService
	log     *logging.Log
}

func NewReportsHandler(reports *report.EmailService, log *logging.Log) *ReportsHandler {
	return &ReportsHandler{
		reports: reports,
		log:     log,
	}
}

func (h *ReportsHandler) SendMonthlyReport(w http.ResponseWriter, r *http.Request) {
	// err := h.reports.SendMonthlyUsageReportIfNotSent(r.Context(), time.Now())
	err := h.reports.SendMonthlyUsageReport(r.Context(), time.Now())
	if err != nil {

		if h.log != nil {
			h.log.Error("monthly_report_failed", map[string]any{
				"error": err.Error(),
			})
		}

		http.Error(w, "failed to send monthly report", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"monthly report sent"}`))
}
