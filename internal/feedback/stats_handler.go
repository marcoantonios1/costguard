package feedback

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// StatsHandler serves aggregated feedback quality metrics.
type StatsHandler struct {
	reader *Reader
}

// NewStatsHandler builds a StatsHandler backed by reader.
func NewStatsHandler(reader *Reader) *StatsHandler {
	return &StatsHandler{reader: reader}
}

func (h *StatsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	lastN := 0
	if s := q.Get("last_n"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 0 {
			http.Error(w, "last_n must be a non-negative integer", http.StatusBadRequest)
			return
		}
		lastN = v
	}

	f := Filter{
		Consumer: q.Get("consumer"),
		Role:     q.Get("role"),
		Model:    q.Get("model"),
		LastN:    lastN,
	}

	records, err := h.reader.Read(r.Context(), f)
	if err != nil {
		http.Error(w, "failed to read feedback log", http.StatusInternalServerError)
		return
	}

	stats := Aggregate(records, f)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}
