package health

import (
	"sort"
	"sync"
	"time"
)

// Outcome represents the result of a single upstream call.
type Outcome struct {
	Success       bool
	Latency       time.Duration
	Timestamp     time.Time
	ErrorCategory string // "" on success; taxonomy category on failure
}

// Snapshot is a point-in-time view of a provider's health.
type Snapshot struct {
	Provider     string
	Total        int
	Successes    int
	Failures     int
	SuccessRate  float64 // 0.0–1.0; -1 if no data
	AvgLatencyMS float64 // average of successful calls only; -1 if no data
	LastSuccess  time.Time
	LastFailure  time.Time
	LastError    string // ErrorCategory of the most recent failure, or ""
	WindowSize   int
}

// ring is a fixed-size circular buffer of Outcomes.
type ring struct {
	buf  []Outcome
	head int
	size int
}

func newRing(windowSize int) *ring {
	return &ring{buf: make([]Outcome, windowSize)}
}

func (r *ring) add(o Outcome) {
	r.buf[r.head] = o
	r.head = (r.head + 1) % len(r.buf)
	if r.size < len(r.buf) {
		r.size++
	}
}

// entries returns outcomes ordered from oldest to newest.
func (r *ring) entries() []Outcome {
	if r.size == 0 {
		return nil
	}
	out := make([]Outcome, r.size)
	cap := len(r.buf)
	start := (r.head - r.size + cap) % cap
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(start+i)%cap]
	}
	return out
}

// Tracker records per-provider call outcomes in a fixed-size ring buffer.
// All methods are safe for concurrent use.
type Tracker struct {
	mu         sync.RWMutex
	windowSize int
	rings      map[string]*ring
}

// New creates a Tracker with the given ring-buffer size per provider.
// windowSize values < 1 are clamped to 1.
func New(windowSize int) *Tracker {
	if windowSize < 1 {
		windowSize = 1
	}
	return &Tracker{
		windowSize: windowSize,
		rings:      map[string]*ring{},
	}
}

// Record adds an outcome for a named provider.
func (t *Tracker) Record(provider string, o Outcome) {
	t.mu.Lock()
	r, ok := t.rings[provider]
	if !ok {
		r = newRing(t.windowSize)
		t.rings[provider] = r
	}
	r.add(o)
	t.mu.Unlock()
}

// Snapshot returns a point-in-time health snapshot for a named provider.
// Returns SuccessRate=-1 and AvgLatencyMS=-1 when no outcomes are recorded.
func (t *Tracker) Snapshot(provider string) Snapshot {
	t.mu.RLock()
	r, ok := t.rings[provider]
	if !ok {
		t.mu.RUnlock()
		return Snapshot{Provider: provider, SuccessRate: -1, AvgLatencyMS: -1, WindowSize: t.windowSize}
	}
	entries := r.entries()
	t.mu.RUnlock()
	return buildSnapshot(provider, t.windowSize, entries)
}

// Snapshots returns snapshots for all providers that have at least one
// recorded outcome, sorted by provider name ascending.
func (t *Tracker) Snapshots() []Snapshot {
	t.mu.RLock()
	type providerData struct {
		name    string
		entries []Outcome
	}
	all := make([]providerData, 0, len(t.rings))
	for name, r := range t.rings {
		all = append(all, providerData{name: name, entries: r.entries()})
	}
	t.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool { return all[i].name < all[j].name })

	snaps := make([]Snapshot, len(all))
	for i, pd := range all {
		snaps[i] = buildSnapshot(pd.name, t.windowSize, pd.entries)
	}
	return snaps
}

func buildSnapshot(provider string, windowSize int, entries []Outcome) Snapshot {
	s := Snapshot{
		Provider:     provider,
		WindowSize:   windowSize,
		SuccessRate:  -1,
		AvgLatencyMS: -1,
	}
	if len(entries) == 0 {
		return s
	}

	s.Total = len(entries)
	var latencySum float64
	var latencyCount int

	for _, o := range entries {
		if o.Success {
			s.Successes++
			latencySum += float64(o.Latency.Milliseconds())
			latencyCount++
			if o.Timestamp.After(s.LastSuccess) {
				s.LastSuccess = o.Timestamp
			}
		} else {
			s.Failures++
			if o.Timestamp.After(s.LastFailure) {
				s.LastFailure = o.Timestamp
				s.LastError = o.ErrorCategory
			}
		}
	}

	s.SuccessRate = float64(s.Successes) / float64(s.Total)
	if latencyCount > 0 {
		s.AvgLatencyMS = latencySum / float64(latencyCount)
	}
	return s
}
