package breaker

import "sync"

// Registry holds one Breaker per provider name, created on first access.
// All methods are safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	breakers  map[string]*Breaker
	defaults  Policy
	overrides map[string]Policy
}

// NewRegistry returns a Registry that applies defaultPolicy to any provider
// that does not have a per-provider override set.
func NewRegistry(defaultPolicy Policy) *Registry {
	return &Registry{
		breakers:  map[string]*Breaker{},
		defaults:  defaultPolicy,
		overrides: map[string]Policy{},
	}
}

// SetPolicy stores a per-provider policy override and discards any existing
// Breaker for that provider so the next For call uses the new policy.
func (r *Registry) SetPolicy(providerName string, p Policy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[providerName] = p
	delete(r.breakers, providerName)
}

// For returns the Breaker for a given provider, creating one on first call.
func (r *Registry) For(providerName string) *Breaker {
	r.mu.RLock()
	b, ok := r.breakers[providerName]
	r.mu.RUnlock()
	if ok {
		return b
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok = r.breakers[providerName]; ok {
		return b
	}
	policy := r.defaults
	if override, ok := r.overrides[providerName]; ok {
		policy = override
	}
	b = New(policy)
	r.breakers[providerName] = b
	return b
}

// AllStats returns a snapshot of every known breaker keyed by provider name.
func (r *Registry) AllStats() map[string]Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]Stats, len(r.breakers))
	for name, b := range r.breakers {
		result[name] = b.Stats()
	}
	return result
}
