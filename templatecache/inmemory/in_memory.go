package inmemory

import (
	"strings"
	"sync"

	"github.com/gobuffalo/plush/v5"
)

type MemoryCache struct {
	mu    sync.RWMutex
	store map[string]*plush.Template
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		store: make(map[string]*plush.Template),
	}
}

func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]*plush.Template)
}

func (c *MemoryCache) Get(key string) (*plush.Template, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.store[key]
	return t, ok
}

func (c *MemoryCache) Set(key string, t *plush.Template) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = t
}

func (c *MemoryCache) Delete(key ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range key {
		if k == "" {
			continue
		}
		delete(c.store, k)
	}
}
