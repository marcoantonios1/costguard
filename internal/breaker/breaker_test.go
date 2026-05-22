package breaker

import (
	"sync"
	"testing"
	"time"
)

func tripPolicy() Policy {
	return Policy{
		FailureThreshold: 3,
		CooldownDuration: 30 * time.Second,
		TripOn:           []string{"upstream_failure", "provider_unavailable"},
	}
}

func TestClosed_AllowsRequests(t *testing.T) {
	b := New(tripPolicy())
	allowed, state := b.Allow()
	if !allowed {
		t.Error("expected allowed in closed state")
	}
	if state != StateClosed {
		t.Errorf("state: got %q, want closed", state)
	}
}

func TestClosed_TripsOnThreshold(t *testing.T) {
	b := New(tripPolicy())
	for i := 0; i < 2; i++ {
		b.RecordFailure("upstream_failure")
		if b.State() != StateClosed {
			t.Errorf("should still be closed after %d failures", i+1)
		}
	}
	b.RecordFailure("upstream_failure") // 3rd — should trip
	if b.State() != StateOpen {
		t.Errorf("state: got %q, want open after threshold", b.State())
	}
}

func TestClosed_NonTripOnCategoryIgnored(t *testing.T) {
	b := New(tripPolicy())
	for _, cat := range []string{"auth", "invalid_request", "rate_limit", "unknown"} {
		b.RecordFailure(cat)
	}
	if b.State() != StateClosed {
		t.Errorf("non-tripon categories should not change state; got %q", b.State())
	}
	if b.Stats().ConsecFailures != 0 {
		t.Errorf("ConsecFailures: got %d, want 0", b.Stats().ConsecFailures)
	}
}

func TestClosed_SuccessResetsCounter(t *testing.T) {
	b := New(tripPolicy())
	b.RecordFailure("upstream_failure")
	b.RecordFailure("upstream_failure")
	b.RecordSuccess()
	if b.Stats().ConsecFailures != 0 {
		t.Errorf("ConsecFailures: got %d, want 0 after success", b.Stats().ConsecFailures)
	}
	if b.State() != StateClosed {
		t.Errorf("state: got %q, want closed", b.State())
	}
}

func TestOpen_RejectsRequests(t *testing.T) {
	b := New(tripPolicy())
	for i := 0; i < 3; i++ {
		b.RecordFailure("upstream_failure")
	}
	allowed, state := b.Allow()
	if allowed {
		t.Error("expected not allowed in open state")
	}
	if state != StateOpen {
		t.Errorf("state: got %q, want open", state)
	}
}

func TestOpen_TransitionsToHalfOpenAfterCooldown(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	b := newWithClock(tripPolicy(), clock)

	for i := 0; i < 3; i++ {
		b.RecordFailure("upstream_failure")
	}
	if b.State() != StateOpen {
		t.Fatal("breaker should be open")
	}

	// Before cooldown — still open.
	now = now.Add(29 * time.Second)
	allowed, state := b.Allow()
	if allowed || state != StateOpen {
		t.Errorf("should still be open before cooldown; allowed=%v state=%q", allowed, state)
	}

	// After cooldown — half-open.
	now = now.Add(2 * time.Second)
	allowed, state = b.Allow()
	if !allowed {
		t.Error("probe should be allowed after cooldown")
	}
	if state != StateHalfOpen {
		t.Errorf("state: got %q, want half_open", state)
	}
}

func TestHalfOpen_OnlyOneConcurrentProbe(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	b := newWithClock(tripPolicy(), clock)

	for i := 0; i < 3; i++ {
		b.RecordFailure("upstream_failure")
	}
	// Advance past cooldown.
	now = now.Add(31 * time.Second)

	// First caller gets the probe.
	allowed1, state1 := b.Allow()
	if !allowed1 || state1 != StateHalfOpen {
		t.Fatalf("first probe: allowed=%v state=%q", allowed1, state1)
	}

	// Second concurrent caller must be blocked.
	allowed2, state2 := b.Allow()
	if allowed2 {
		t.Error("second concurrent probe should not be allowed")
	}
	if state2 != StateHalfOpen {
		t.Errorf("state2: got %q, want half_open", state2)
	}
}

func TestHalfOpen_ProbeSuccessCloses(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	b := newWithClock(tripPolicy(), clock)

	for i := 0; i < 3; i++ {
		b.RecordFailure("upstream_failure")
	}
	now = now.Add(31 * time.Second)
	b.Allow() // take probe slot

	b.RecordSuccess()
	if b.State() != StateClosed {
		t.Errorf("state: got %q, want closed after probe success", b.State())
	}
	if b.Stats().ConsecFailures != 0 {
		t.Errorf("ConsecFailures: got %d, want 0", b.Stats().ConsecFailures)
	}
}

func TestHalfOpen_ProbeFailureReopens(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	b := newWithClock(tripPolicy(), clock)

	for i := 0; i < 3; i++ {
		b.RecordFailure("upstream_failure")
	}
	tripAt := now
	now = now.Add(31 * time.Second)
	b.Allow() // take probe slot

	b.RecordFailure("upstream_failure")
	if b.State() != StateOpen {
		t.Errorf("state: got %q, want open after probe failure", b.State())
	}
	// tripTime must have been reset (not the original trip time).
	stats := b.Stats()
	if !stats.TripTime.After(tripAt) {
		t.Errorf("tripTime should be reset after probe failure; got %v, original %v", stats.TripTime, tripAt)
	}
}

func TestHalfOpen_ProbeFailureResetsCooldown(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	b := newWithClock(tripPolicy(), clock)

	for i := 0; i < 3; i++ {
		b.RecordFailure("upstream_failure")
	}
	now = now.Add(31 * time.Second)
	b.Allow() // take probe
	b.RecordFailure("upstream_failure")

	// Cooldown should have restarted — should still be open at 29s after probe failure.
	now = now.Add(29 * time.Second)
	allowed, _ := b.Allow()
	if allowed {
		t.Error("should still be open within new cooldown window")
	}
}

func TestDisabled_AlwaysAllows(t *testing.T) {
	b := New(Policy{Disabled: true})
	for i := 0; i < 100; i++ {
		b.RecordFailure("upstream_failure")
	}
	allowed, state := b.Allow()
	if !allowed {
		t.Error("disabled breaker must always allow")
	}
	if state != StateClosed {
		t.Errorf("state: got %q, want closed", state)
	}
	if b.State() != StateClosed {
		t.Errorf("State(): got %q, want closed", b.State())
	}
}

func TestDisabled_RecordFailureIsNoOp(t *testing.T) {
	b := New(Policy{Disabled: true})
	b.RecordFailure("upstream_failure")
	b.RecordFailure("upstream_failure")
	if s := b.Stats(); s.ConsecFailures != 0 || s.State != StateClosed {
		t.Errorf("disabled breaker stats should be zero; got %+v", s)
	}
}

func TestConcurrentAllowAndRecord(t *testing.T) {
	b := New(tripPolicy())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			b.Allow()
		}()
		go func() {
			defer wg.Done()
			b.RecordFailure("upstream_failure")
		}()
		go func() {
			defer wg.Done()
			b.RecordSuccess()
		}()
	}
	wg.Wait()
}

func TestRegistry_ForCreatesBreaker(t *testing.T) {
	r := NewRegistry(DefaultPolicy())
	b := r.For("p1")
	if b == nil {
		t.Fatal("For returned nil")
	}
	// Second call returns the same instance.
	b2 := r.For("p1")
	if b != b2 {
		t.Error("For should return the same Breaker on repeated calls")
	}
}

func TestRegistry_SetPolicyOverridesDefault(t *testing.T) {
	r := NewRegistry(DefaultPolicy())
	r.SetPolicy("custom", Policy{Disabled: true})
	b := r.For("custom")
	allowed, _ := b.Allow()
	if !allowed {
		t.Error("custom disabled policy should always allow")
	}
}

func TestRegistry_SetPolicyResetsExistingBreaker(t *testing.T) {
	r := NewRegistry(tripPolicy())
	b1 := r.For("p")
	// Trip it.
	for i := 0; i < 3; i++ {
		b1.RecordFailure("upstream_failure")
	}
	// Override with disabled — old breaker discarded.
	r.SetPolicy("p", Policy{Disabled: true})
	b2 := r.For("p")
	if b1 == b2 {
		t.Error("SetPolicy should have replaced the breaker")
	}
	allowed, _ := b2.Allow()
	if !allowed {
		t.Error("new disabled breaker should always allow")
	}
}

func TestRegistry_AllStats(t *testing.T) {
	r := NewRegistry(DefaultPolicy())
	r.For("a")
	r.For("b")
	stats := r.AllStats()
	if len(stats) != 2 {
		t.Errorf("AllStats: got %d entries, want 2", len(stats))
	}
	if _, ok := stats["a"]; !ok {
		t.Error("AllStats: missing entry for 'a'")
	}
}
