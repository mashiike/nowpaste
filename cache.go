package nowpaste

import (
	"context"
	"sync"
	"time"
)

type ChannelCacheEntry struct {
	ChannelID   string
	ChannelName string
	TTL         time.Time
}

type ChannelCache interface {
	Get(ctx context.Context, channelName string) (channelID string, ok bool, err error)
	SetMulti(ctx context.Context, entries []ChannelCacheEntry) error
}

type InmemoryChannelCache struct {
	mu    sync.RWMutex
	cache map[string]ChannelCacheEntry
}

var _ ChannelCache = (*InmemoryChannelCache)(nil)

func NewInmemoryChannelCache() *InmemoryChannelCache {
	return &InmemoryChannelCache{
		cache: make(map[string]ChannelCacheEntry),
	}
}

func (c *InmemoryChannelCache) Get(ctx context.Context, channelName string) (channelID string, ok bool, err error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[channelName]
	if !ok {
		return "", false, nil
	}
	if entry.TTL.Before(time.Now()) {
		delete(c.cache, channelName)
		return "", false, nil
	}
	return entry.ChannelID, true, nil
}

func (c *InmemoryChannelCache) SetMulti(ctx context.Context, entries []ChannelCacheEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range entries {
		c.cache[entry.ChannelName] = entry
	}
	return nil
}
