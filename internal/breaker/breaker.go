package breaker

import (
	"sync"
	"time"
)

// State represents the current circuit-breaker state.
type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half_open"
)

// Policy configures a Breaker instance.
type Policy struct {
	// FailureThreshold is the number of consecutive relevant failures before
	// the breaker opens. Zero uses the default (5).
	FailureThreshold int
	// CooldownDuration is how long the breaker stays open before allowing a
	// probe. Zero uses the default (30s).
	CooldownDuration time.Duration
	// TripOn is the set of error categories that count as relevant failures.
	// Defaults to ["upstream_failure", "provider_unavailable"].
	TripOn []string
	// Disabled makes Allow always return true and all recording a no-op.
	Disabled bool
}

// DefaultPolicy returns a Policy suitable for production use.
func DefaultPolicy() Policy {
	return Policy{
		FailureThreshold: 5,
		CooldownDuration: 30 * time.Second,
		TripOn:           []string{"upstream_failure", "provider_unavailable"},
	}
}

// Stats is a point-in-time snapshot of a Breaker.
type Stats struct {
	State            State
	ConsecFailures   int
	TripTime         time.Time
	CooldownDuration time.Duration
}

// Breaker is a three-state (closed/open/half-open) circuit breaker. All
// methods are safe for concurrent use.
type Breaker struct {
	mu            sync.Mutex
	policy        Policy
	state         State
	failures      int
	tripTime      time.Time
	probeInFlight bool
	now           func() time.Time
}

// New returns a Breaker using the given policy and real wall time.
func New(p Policy) *Breaker {
	return newWithClock(p, time.Now)
}

func newWithClock(p Policy, now func() time.Time) *Breaker {
	return &Breaker{
		policy: p,
		state:  StateClosed,
		now:    now,
	}
}

// Allow reports whether a request may proceed. In half-open state exactly one
// concurrent probe is permitted; subsequent callers receive (false, half_open)
// until the probe is resolved.
//
// Open → half-open transition is evaluated on every Allow call.
func (b *Breaker) Allow() (allowed bool, state State) {
	if b.policy.Disabled {
		return true, StateClosed
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Open → half-open if cooldown has elapsed.
	if b.state == StateOpen && b.now().Sub(b.tripTime) >= b.policy.CooldownDuration {
		b.state = StateHalfOpen
	}

	switch b.state {
	case StateClosed:
		return true, StateClosed
	case StateOpen:
		return false, StateOpen
	case StateHalfOpen:
		if b.probeInFlight {
			return false, StateHalfOpen
		}
		b.probeInFlight = true
		return true, StateHalfOpen
	default:
		return false, b.state
	}
}

// RecordSuccess resets the failure counter and, if in half-open, closes the
// breaker.
func (b *Breaker) RecordSuccess() {
	if b.policy.Disabled {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.probeInFlight = false
	b.state = StateClosed
}

// RecordFailure records a failure for the given error category. Categories not
// in Policy.TripOn are silently ignored.
//
//   - Closed: increments consecutive counter; trips when threshold reached.
//   - Half-open: any relevant failure reopens the breaker and resets cooldown.
//   - Open: no-op (already open).
func (b *Breaker) RecordFailure(errorCategory string) {
	if b.policy.Disabled {
		return
	}
	if !b.isRelevant(errorCategory) {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.probeInFlight = false

	switch b.state {
	case StateClosed:
		b.failures++
		if b.failures >= b.policy.FailureThreshold {
			b.state = StateOpen
			b.tripTime = b.now()
		}
	case StateHalfOpen:
		b.failures++
		b.state = StateOpen
		b.tripTime = b.now()
	}
}

// State returns the current state without side effects.
func (b *Breaker) State() State {
	if b.policy.Disabled {
		return StateClosed
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Stats returns a point-in-time snapshot.
func (b *Breaker) Stats() Stats {
	if b.policy.Disabled {
		return Stats{State: StateClosed}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return Stats{
		State:            b.state,
		ConsecFailures:   b.failures,
		TripTime:         b.tripTime,
		CooldownDuration: b.policy.CooldownDuration,
	}
}

func (b *Breaker) isRelevant(category string) bool {
	for _, c := range b.policy.TripOn {
		if c == category {
			return true
		}
	}
	return false
}
