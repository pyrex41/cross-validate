package renderer

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CompositionCache is a two-tier SHA-256 keyed cache for `crossplane render`
// outputs. Separate from Cache (which is helm-specific) so cache lifecycles
// and eviction reasons don't accidentally entangle — a helm upgrade should
// not bust composition cache entries, and vice versa.
//
// Design is parallel to Cache: in-memory map + disk tier at DiskDir. Same
// DefaultTTL. Write failures zero DiskDir so broken disks don't cause
// per-Put retries.
type CompositionCache struct {
	DiskDir string

	mu  sync.Mutex
	mem map[string]cacheEntry
}

// NewCompositionCache constructs a CompositionCache. Empty diskDir defaults
// to ~/.cache/xpc/compositions/. The directory is created eagerly; a failed
// mkdir degrades the cache to memory-only (DiskDir zeroed).
func NewCompositionCache(diskDir string) *CompositionCache {
	if diskDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			diskDir = filepath.Join(home, ".cache", "xpc", "compositions")
		}
	}
	if diskDir != "" {
		if err := os.MkdirAll(diskDir, 0o755); err != nil {
			diskDir = ""
		}
	}
	return &CompositionCache{DiskDir: diskDir, mem: map[string]cacheEntry{}}
}

// Get returns cached bytes and true if the entry exists and is fresh.
// Falls through memory → disk. Stale entries are evicted on access.
func (c *CompositionCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.mem[key]; ok {
		if time.Since(e.Rendered) < DefaultTTL {
			return e.Bytes, true
		}
		delete(c.mem, key)
	}
	if c.DiskDir == "" {
		return nil, false
	}
	path := filepath.Join(c.DiskDir, key)
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if time.Since(info.ModTime()) >= DefaultTTL {
		_ = os.Remove(path)
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	c.mem[key] = cacheEntry{Bytes: data, Rendered: info.ModTime()}
	return data, true
}

// Put stores bytes under the given key in both tiers. Memory tier always
// succeeds; disk failure zeroes DiskDir for the rest of this process.
func (c *CompositionCache) Put(key string, data []byte) {
	c.mu.Lock()
	c.mem[key] = cacheEntry{Bytes: append([]byte(nil), data...), Rendered: time.Now()}
	dir := c.DiskDir
	c.mu.Unlock()

	if dir == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, key), data, 0o644); err != nil {
		c.mu.Lock()
		c.DiskDir = ""
		c.mu.Unlock()
	}
}
