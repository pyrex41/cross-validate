package renderer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKeyDeterminism guards against a foot-gun: if the cache key changed
// from run to run (e.g. because we iterated a map without sorting), we'd
// never get a cache hit. Same input → same key.
func TestKeyDeterminism(t *testing.T) {
	in := CacheKeyInput{
		ChartDirDigest: "abc",
		ValuesBytes:    []byte(`{"a":1,"b":2}`),
		HelmVersion:    "v3.14.0",
		ReleaseName:    "r",
		Namespace:      "ns",
	}
	k1 := Key(in)
	k2 := Key(in)
	if k1 != k2 {
		t.Fatalf("Key not deterministic: %q vs %q", k1, k2)
	}
	if len(k1) != 64 {
		t.Fatalf("Key not sha256 length: %q", k1)
	}

	// Changing any field must change the key.
	in2 := in
	in2.HelmVersion = "v3.14.1"
	if Key(in2) == k1 {
		t.Fatalf("HelmVersion change did not alter key")
	}
	in3 := in
	in3.Namespace = "other"
	if Key(in3) == k1 {
		t.Fatalf("Namespace change did not alter key")
	}
}

func TestHashChartDirDeterministic(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Chart.yaml", "apiVersion: v2\nname: t\nversion: 0\n")
	write("templates/a.yaml", "kind: A\n")
	write("templates/b.yaml", "kind: B\n")

	h1, err := HashChartDir(dir)
	if err != nil {
		t.Fatalf("HashChartDir: %v", err)
	}
	h2, err := HashChartDir(dir)
	if err != nil {
		t.Fatalf("HashChartDir: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("HashChartDir not deterministic: %q vs %q", h1, h2)
	}

	// A content change must flip the hash.
	write("templates/b.yaml", "kind: B2\n")
	h3, err := HashChartDir(dir)
	if err != nil {
		t.Fatalf("HashChartDir: %v", err)
	}
	if h3 == h1 {
		t.Fatalf("HashChartDir did not change after content edit")
	}
}

func TestCachePutGetRoundtrip(t *testing.T) {
	c := NewCache(t.TempDir())
	c.Put("k1", []byte("payload"))
	got, ok := c.Get("k1")
	if !ok {
		t.Fatal("Get miss after Put")
	}
	if string(got) != "payload" {
		t.Fatalf("Get returned %q, want payload", got)
	}

	// Fresh cache pointed at the same disk dir should see the entry.
	c2 := &Cache{DiskDir: c.DiskDir, mem: map[string]cacheEntry{}}
	got2, ok := c2.Get("k1")
	if !ok {
		t.Fatal("Get miss on disk after fresh cache")
	}
	if string(got2) != "payload" {
		t.Fatalf("disk-tier Get returned %q, want payload", got2)
	}
}
