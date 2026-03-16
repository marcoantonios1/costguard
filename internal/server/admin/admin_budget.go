package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/budget"
)

type BudgetStatusReader interface {
	GetMonthlyStatus(ctx context.Context, now time.Time) (budget.Status, error)
}

type BudgetHandler struct {
	budget BudgetStatusReader
}

func NewBudgetHandler(budget BudgetStatusReader) *BudgetHandler {
	return &BudgetHandler{budget: budget}
}

func (h *BudgetHandler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status, err := h.budget.GetMonthlyStatus(r.Context(), time.Now())
	if err != nil {
		http.Error(w, "failed to load budget status", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, status)
}
