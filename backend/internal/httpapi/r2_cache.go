package httpapi

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

type responseCache struct {
	mu    sync.RWMutex
	items map[string]responseCacheItem
}

type responseCacheItem struct {
	data      []byte
	expiresAt time.Time
}

func newResponseCache() *responseCache {
	return &responseCache{items: make(map[string]responseCacheItem)}
}

func (cache *responseCache) GetJSON(key string, dest any) bool {
	cache.mu.RLock()
	item, ok := cache.items[key]
	cache.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(item.expiresAt) {
		cache.mu.Lock()
		delete(cache.items, key)
		cache.mu.Unlock()
		return false
	}
	return json.Unmarshal(item.data, dest) == nil
}

func (cache *responseCache) SetJSON(key string, value any, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	cache.mu.Lock()
	cache.items[key] = responseCacheItem{data: data, expiresAt: time.Now().Add(ttl)}
	cache.mu.Unlock()
}

func (cache *responseCache) DeletePatterns(patterns ...string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for key := range cache.items {
		for _, pattern := range patterns {
			if cachePatternMatches(pattern, key) {
				delete(cache.items, key)
				break
			}
		}
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
