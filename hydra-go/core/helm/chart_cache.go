package helm

import (
	"fmt"
	"sync"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"helm.sh/helm/v4/pkg/chart"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// ChartCacheEntry holds a cached chart with optional error
type ChartCacheEntry struct {
	Charter chart.Charter
	Error   error
}

// ChartCache caches loaded charts based on path and mode
type ChartCache struct {
	l        log.Logger
	disabled bool
	mu       sync.RWMutex
	cache    map[string]ChartCacheEntry
}

// NewChartCache creates a new chart cache
func NewChartCache(l log.Logger) *ChartCache {
	return &ChartCache{
		l:     l,
		mu:    sync.RWMutex{},
		cache: make(map[string]ChartCacheEntry),
	}
}

// SetDisabled when true makes GetOrLoad always invoke loader without storing entries.
func (c *ChartCache) SetDisabled(disabled bool) {
	c.mu.Lock()
	c.disabled = disabled
	c.mu.Unlock()
}

// GetOrLoad retrieves a chart from the cache or loads it using the provided loader function
func (c *ChartCache) GetOrLoad(path string, mode types.HelmNetworkMode, loader func() ChartCacheEntry) (chart.Charter, error) {
	c.mu.RLock()
	disabled := c.disabled
	c.mu.RUnlock()
	if disabled {
		entry := loader()
		return entry.Charter, entry.Error
	}

	key := c.cacheKey(path, mode)
	keyOffline := c.cacheKey(path, types.HelmNetworkModeOffline)

	// Try to get from cache first
	c.mu.RLock()
	entry, ok := c.cache[key]
	c.mu.RUnlock()

	t := "cache hit"
	if !ok {
		t = "cache miss"
	}

	c.l.DebugLog(logIdChartCache, "{type} for cache '{name}' with key '{key}'",
		log.String("type", t), log.String("name", "helm-charts"), log.String("key", string(key)))

	if ok {
		return entry.Charter, entry.Error
	}

	// Load if not cached
	entry = loader()

	// Store in cache
	c.mu.Lock()
	c.cache[key] = entry
	// Also cache with offline mode
	if mode == types.HelmNetworkModeOnline {
		c.cache[keyOffline] = entry
	}
	c.mu.Unlock()

	return entry.Charter, entry.Error
}

// cacheKey generates a cache key from path and mode
func (c *ChartCache) cacheKey(path string, mode types.HelmNetworkMode) string {
	return fmt.Sprintf("%s:%s", path, mode.String())
}
