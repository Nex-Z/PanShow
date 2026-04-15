package httpapi

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

const responseCacheMaxItems = 128
const responseCacheMaxBytes = 16 * 1024 * 1024

type responseCache struct {
	mu         sync.RWMutex
	items      map[string]responseCacheItem
	totalBytes int
}

type responseCacheItem struct {
	createdAt      time.Time
	data           []byte
	expiresAt      time.Time
	staleExpiresAt time.Time
}

func newResponseCache() *responseCache {
	return &responseCache{items: make(map[string]responseCacheItem)}
}

func (cache *responseCache) GetJSON(key string, dest any) bool {
	return cache.getJSON(key, dest, false)
}

func (cache *responseCache) GetStaleJSON(key string, dest any) bool {
	return cache.getJSON(key, dest, true)
}

func (cache *responseCache) getJSON(key string, dest any, allowStale bool) bool {
	cache.mu.RLock()
	item, ok := cache.items[key]
	cache.mu.RUnlock()
	if !ok {
		return false
	}
	now := time.Now()
	if now.After(item.staleExpiresAt) {
		cache.mu.Lock()
		if current, ok := cache.items[key]; ok && current.staleExpiresAt.Equal(item.staleExpiresAt) {
			cache.deleteLocked(key)
		}
		cache.mu.Unlock()
		return false
	}
	if !allowStale && now.After(item.expiresAt) {
		return false
	}
	return json.Unmarshal(item.data, dest) == nil
}

func (cache *responseCache) SetJSON(key string, value any, ttl, staleTTL time.Duration) {
	if ttl <= 0 {
		return
	}
	if staleTTL < 0 {
		staleTTL = 0
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	if len(data) > responseCacheMaxBytes {
		cache.mu.Lock()
		cache.deleteLocked(key)
		cache.mu.Unlock()
		return
	}
	now := time.Now()
	cache.mu.Lock()
	cache.deleteLocked(key)
	cache.items[key] = responseCacheItem{createdAt: now, data: data, expiresAt: now.Add(ttl), staleExpiresAt: now.Add(ttl + staleTTL)}
	cache.totalBytes += len(data)
	cache.pruneLocked(now)
	cache.mu.Unlock()
}

func (cache *responseCache) DeletePatterns(patterns ...string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for key := range cache.items {
		for _, pattern := range patterns {
			if cachePatternMatches(pattern, key) {
				cache.deleteLocked(key)
				break
			}
		}
	}
}

func (cache *responseCache) pruneLocked(now time.Time) {
	for key, item := range cache.items {
		if now.After(item.staleExpiresAt) {
			cache.deleteLocked(key)
		}
	}

	for (len(cache.items) > responseCacheMaxItems || cache.totalBytes > responseCacheMaxBytes) && len(cache.items) > 0 {
		var oldestKey string
		var oldestAt time.Time
		for key, item := range cache.items {
			if oldestKey == "" || item.createdAt.Before(oldestAt) {
				oldestKey = key
				oldestAt = item.createdAt
			}
		}
		cache.deleteLocked(oldestKey)
	}
}

func (cache *responseCache) deleteLocked(key string) {
	if item, ok := cache.items[key]; ok {
		cache.totalBytes -= len(item.data)
		delete(cache.items, key)
	}
}

func cachePatternMatches(pattern, key string) bool {
	pattern = unescapeCachePattern(pattern)
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(key, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == key
}

func unescapeCachePattern(value string) string {
	replacer := strings.NewReplacer(
		"\\\\", "\\",
		"\\*", "*",
		"\\?", "?",
		"\\[", "[",
		"\\]", "]",
	)
	return replacer.Replace(value)
}
