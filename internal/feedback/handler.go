package feedback

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/logging"
)

// Handler ingests feedback records over HTTP.
type Handler struct {
	store Store
	log   *logging.Log
}

// NewHandler builds a Handler backed by store. log may be nil.
func NewHandler(store Store, log *logging.Log) *Handler {
	return &Handler{store: store, log: log}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var rec FeedbackRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if rec.Consumer == "" {
		http.Error(w, "consumer is required", http.StatusUnprocessableEntity)
		return
	}
	if rec.Outcome != "success" && rec.Outcome != "failure" {
		http.Error(w, `outcome must be "success" or "failure"`, http.StatusUnprocessableEntity)
		return
	}

	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}
	rec.ReceivedAt = time.Now().UTC()

	w.WriteHeader(http.StatusAccepted)

	go func() {
		if err := h.store.Append(context.Background(), rec); err != nil && h.log != nil {
			h.log.Error("feedback_append_failed", map[string]any{
				"consumer": rec.Consumer,
				"error":    err.Error(),
			})
		}
	}()
}
