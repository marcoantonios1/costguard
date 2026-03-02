package providers

import "fmt"

type Registry struct {
	m map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Provider)}
}

func (r *Registry) Register(name string, p Provider) {
	r.m[name] = p
}

func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	return p, nil
}
