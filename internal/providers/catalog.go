package providers

import "sync"

type Catalog struct {
	mu    sync.RWMutex
	items map[string]RuntimeMetadata
}

func NewCatalog() *Catalog {
	return &Catalog{
		items: map[string]RuntimeMetadata{},
	}
}

func (c *Catalog) Set(name string, md RuntimeMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[name] = md
}

func (c *Catalog) Get(name string) (RuntimeMetadata, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.items[name]
	return v, ok
}

func (c *Catalog) List() []RuntimeMetadata {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]RuntimeMetadata, 0, len(c.items))
	for _, v := range c.items {
		out = append(out, v)
	}
	return out
}

// SupportsModel returns the names of all enabled providers that explicitly list
// modelID in their SupportedModels slice. Unconstrained providers (empty
// SupportedModels) are not included.
func (c *Catalog) SupportsModel(modelID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []string
	for name, md := range c.items {
		if !md.Enabled || len(md.SupportedModels) == 0 {
			continue
		}
		for _, m := range md.SupportedModels {
			if m == modelID {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// Priority returns the configured priority for providerName, or 0 if unknown.
func (c *Catalog) Priority(providerName string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	md, ok := c.items[providerName]
	if !ok {
		return 0
	}
	return md.Priority
}

// ModelSupported reports whether the named provider supports modelID.
// Returns true when the provider is unconstrained (empty SupportedModels) or
// when modelID appears in the list. Returns false if the provider is unknown.
func (c *Catalog) ModelSupported(providerName, modelID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	md, ok := c.items[providerName]
	if !ok {
		return false
	}
	if len(md.SupportedModels) == 0 {
		return true
	}
	for _, m := range md.SupportedModels {
		if m == modelID {
			return true
		}
	}
	return false
}
