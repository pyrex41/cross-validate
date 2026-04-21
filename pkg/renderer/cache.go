package renderer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// DefaultTTL is the render cache TTL. Same as the snapshot cache so users only
// have one number to remember.
const DefaultTTL = 15 * time.Minute

// CacheKeyInput is the union of all inputs that affect the rendered output
// for a single invocation. SHA-256 over the deterministic serialization of
// this struct is the cache key.
type CacheKeyInput struct {
	// ChartDirDigest is the SHA-256 of the chart directory tree (sorted by
	// relative path, file bytes concatenated).
	ChartDirDigest string
	// ValuesBytes is the canonical, sorted-key serialization of the merged
	// values map (values.yaml + additional valueFiles + valuesInline + --set).
	ValuesBytes []byte
	// HelmVersion is the output of `helm version --short` for the renderer in
	// use. A helm upgrade must bust every cached entry.
	HelmVersion string
	// ReleaseName is passed to `helm template` and affects output.
	ReleaseName string
	// Namespace is passed via --namespace.
	Namespace string
}

// cacheEntry holds an in-memory copy of the rendered bytes plus the time
// they were produced so we can honour DefaultTTL.
type cacheEntry struct {
	Bytes    []byte
	Rendered time.Time
}

// Cache is a two-tier SHA-256-keyed cache: a process-local memory map plus
// an on-disk tier rooted at DiskDir. TTL is DefaultTTL for both.
type Cache struct {
	DiskDir string

	mu  sync.Mutex
	mem map[string]cacheEntry
}

// NewCache constructs a Cache backed by the given on-disk directory. If
// diskDir is empty, defaults to ~/.cache/xpc/renders/. The directory is
// created eagerly here — if creation fails (read-only home, permission
// denied), DiskDir is zeroed and the cache degrades to memory-only rather
// than re-failing on every Put. The previous implementation deferred
// MkdirAll to the first Put and silently swallowed its error, which is why
// ~/.cache/xpc/ never populated under some host configurations.
func NewCache(diskDir string) *Cache {
	if diskDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			diskDir = filepath.Join(home, ".cache", "xpc", "renders")
		}
	}
	if diskDir != "" {
		if err := os.MkdirAll(diskDir, 0o755); err != nil {
			diskDir = ""
		}
	}
	return &Cache{DiskDir: diskDir, mem: map[string]cacheEntry{}}
}

// Key computes the SHA-256 cache key for the given input. Deterministic:
// same input → same hex digest.
func Key(in CacheKeyInput) string {
	h := sha256.New()
	fmt.Fprintf(h, "chart:%s\n", in.ChartDirDigest)
	fmt.Fprintf(h, "helm:%s\n", in.HelmVersion)
	fmt.Fprintf(h, "release:%s\n", in.ReleaseName)
	fmt.Fprintf(h, "ns:%s\n", in.Namespace)
	h.Write([]byte("values:"))
	h.Write(in.ValuesBytes)
	return hex.EncodeToString(h.Sum(nil))
}

// Get returns the cached bytes and true if the entry exists and is fresh.
// Falls through memory → disk. Stale entries are evicted on access.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.mem[key]; ok {
		if time.Since(entry.Rendered) < DefaultTTL {
			return entry.Bytes, true
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

// Put stores bytes under the given key in both tiers. The memory tier
// always succeeds. A disk-tier write failure zeroes DiskDir so subsequent
// Puts don't keep retrying a broken disk — this process stays memory-only
// for the rest of the run.
func (c *Cache) Put(key string, data []byte) {
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

// HashChartDir computes a stable SHA-256 of the contents of chartPath by
// walking its file tree in sorted order. Symlinks are followed once; errors
// are surfaced so the caller can decide whether to bail or treat them as a
// cache miss.
func HashChartDir(chartPath string) (string, error) {
	h := sha256.New()
	var files []string
	err := filepath.WalkDir(chartPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	for _, f := range files {
		rel, err := filepath.Rel(chartPath, f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "file:%s\n", rel)
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
