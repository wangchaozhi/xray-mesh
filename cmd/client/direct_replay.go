package main

import (
	"errors"
	"sync"
	"time"
)

var ErrDirectPayloadReplay = errors.New("direct payload replay detected")

type directReplayCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

var directReplayCaches sync.Map // map[*p2pRuntime]*directReplayCache

func (r *p2pRuntime) acceptDirectReplayKey(key string, expiresAt, now time.Time) bool {
	value, _ := directReplayCaches.LoadOrStore(r, &directReplayCache{entries: make(map[string]time.Time)})
	cache := value.(*directReplayCache)

	now = now.UTC()
	if expiresAt.IsZero() || !now.Before(expiresAt.UTC()) {
		// Live tickets normally have an expiry. Keep a conservative short bound
		// for malformed legacy state instead of growing the cache indefinitely.
		expiresAt = now.Add(30 * time.Second)
	} else {
		expiresAt = expiresAt.UTC()
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	for replayKey, expiry := range cache.entries {
		if !now.Before(expiry) {
			delete(cache.entries, replayKey)
		}
	}
	if _, exists := cache.entries[key]; exists {
		return false
	}
	cache.entries[key] = expiresAt
	return true
}
