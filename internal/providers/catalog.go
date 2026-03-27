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
