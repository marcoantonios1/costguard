package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/usage"
)

type AdminHandler struct {
	usageStore usage.Store
}

func NewAdminHandler(store usage.Store) *AdminHandler {
	return &AdminHandler{usageStore: store}
}

func (h *AdminHandler) UsageSummary(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseRange(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	total, err := h.usageStore.GetTotalSpend(r.Context(), from, to)
	if err != nil {
		http.Error(w, "failed to load usage summary", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, usage.Summary{
		From:          from,
		To:            to,
		TotalSpendUSD: total,
	})
}

func (h *AdminHandler) UsageTeams(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseRange(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	items, err := h.usageStore.GetSpendByTeam(r.Context(), from, to)
	if err != nil {
		http.Error(w, "failed to load team usage", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from":  from,
		"to":    to,
		"teams": items,
	})
}

func parseRange(r *http.Request) (time.Time, time.Time, error) {
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	if fromStr == "" || toStr == "" {
		return time.Time{}, time.Time{}, http.ErrMissingFile
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return from, to, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}