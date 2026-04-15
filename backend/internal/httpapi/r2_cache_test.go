package httpapi

import (
	"testing"
	"time"
)

func TestResponseCacheKeepsExpiredItemForStaleLookup(t *testing.T) {
	cache := newResponseCache()
	now := time.Now()
	cache.items["r2:list:/"] = responseCacheItem{
		createdAt:      now.Add(-time.Minute),
		data:           []byte(`{"path":"/"}`),
		expiresAt:      now.Add(-time.Second),
		staleExpiresAt: now.Add(time.Minute),
	}
	cache.totalBytes = len(cache.items["r2:list:/"].data)

	var fresh filesResponse
	if cache.GetJSON("r2:list:/", &fresh) {
		t.Fatal("fresh lookup unexpectedly returned an expired item")
	}

	var stale filesResponse
	if !cache.GetStaleJSON("r2:list:/", &stale) {
		t.Fatal("stale lookup did not return the expired item")
	}
	if stale.Path != "/" {
		t.Fatalf("stale path = %q, want /", stale.Path)
	}
}

func TestResponseCacheDeletesItemPastStaleTTL(t *testing.T) {
	cache := newResponseCache()
	now := time.Now()
	cache.items["r2:list:/"] = responseCacheItem{
		createdAt:      now.Add(-time.Minute),
		data:           []byte(`{"path":"/"}`),
		expiresAt:      now.Add(-2 * time.Second),
		staleExpiresAt: now.Add(-time.Second),
	}
	cache.totalBytes = len(cache.items["r2:list:/"].data)

	var stale filesResponse
	if cache.GetStaleJSON("r2:list:/", &stale) {
		t.Fatal("stale lookup unexpectedly returned an item past stale TTL")
	}
	if _, ok := cache.items["r2:list:/"]; ok {
		t.Fatal("item past stale TTL was not removed")
	}
	if cache.totalBytes != 0 {
		t.Fatalf("totalBytes = %d, want 0", cache.totalBytes)
	}
}
