package monitor

import (
	"sync"
	"time"
)

type CachedValue[T any] struct {
	mu       sync.Mutex
	value    T
	last     time.Time
	ttl      time.Duration
	fetching bool // prevents concurrent fetches (TOCTOU guard)
}

func NewCachedValue[T any](ttl time.Duration) *CachedValue[T] {
	return &CachedValue[T]{ttl: ttl}
}

func (c *CachedValue[T]) Get(fetch func() T) T {
	c.mu.Lock()
	if !c.last.IsZero() && time.Since(c.last) < c.ttl {
		v := c.value
		c.mu.Unlock()
		return v
	}

	if c.fetching {
		v := c.value
		c.mu.Unlock()
		return v
	}
	c.fetching = true
	c.mu.Unlock()

	result := fetch()

	c.mu.Lock()
	c.value = result
	c.last = time.Now()
	c.fetching = false
	c.mu.Unlock()
	return result
}
