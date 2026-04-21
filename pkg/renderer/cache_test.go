package renderer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
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

// TestKustomizeKeyStability covers cache-key stability for the Kustomize
// renderer. The key input combines the overlay tree digest, kustomize
// version, and the Argo-side ArgoKustomizeSource overrides; changing any
// input must flip the key. Same inputs → same key is the real lifeblood
// invariant — without it we'd never get a cache hit.
func TestKustomizeKeyStability(t *testing.T) {
	tree := "abc123"
	version := "v5.8.1"
	src := &types.ArgoKustomizeSource{
		NamePrefix:   "prod-",
		Images:       []string{"app=app:v1", "db=db:v2"},
		CommonLabels: map[string]string{"tier": "web"},
	}

	k1 := kustomizeKey(tree, version, src)
	k2 := kustomizeKey(tree, version, src)
	if k1 != k2 {
		t.Fatalf("kustomizeKey not deterministic: %q vs %q", k1, k2)
	}

	// Tree change busts key.
	if kustomizeKey("different", version, src) == k1 {
		t.Fatalf("tree change did not alter key")
	}
	// Version change busts key.
	if kustomizeKey(tree, "v5.9.0", src) == k1 {
		t.Fatalf("version change did not alter key")
	}
	// NamePrefix change busts key.
	src2 := *src
	src2.NamePrefix = "dev-"
	if kustomizeKey(tree, version, &src2) == k1 {
		t.Fatalf("namePrefix change did not alter key")
	}
	// Image order must NOT matter (we sort before hashing).
	src3 := *src
	src3.Images = []string{"db=db:v2", "app=app:v1"}
	if kustomizeKey(tree, version, &src3) != k1 {
		t.Fatalf("image-order swap altered key; expected sorted-stable hashing")
	}
	// nil source still produces a deterministic key.
	k4 := kustomizeKey(tree, version, nil)
	k5 := kustomizeKey(tree, version, nil)
	if k4 != k5 {
		t.Fatalf("kustomizeKey(nil) not deterministic")
	}
}

func TestHashKustomizeDirDeterministic(t *testing.T) {
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
	write("kustomization.yaml", "kind: Kustomization\n")
	write("base/a.yaml", "kind: ConfigMap\n")
	write("base/b.yaml", "kind: Secret\n")

	h1, err := HashKustomizeDir(dir)
	if err != nil {
		t.Fatalf("HashKustomizeDir: %v", err)
	}
	h2, err := HashKustomizeDir(dir)
	if err != nil {
		t.Fatalf("HashKustomizeDir: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("HashKustomizeDir not deterministic: %q vs %q", h1, h2)
	}

	write("base/b.yaml", "kind: Secret2\n")
	h3, err := HashKustomizeDir(dir)
	if err != nil {
		t.Fatalf("HashKustomizeDir: %v", err)
	}
	if h3 == h1 {
		t.Fatalf("HashKustomizeDir did not change after content edit")
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

// TestCacheCreatesNestedDirEagerly guards the previous silent failure:
// MkdirAll was deferred to first Put and its error swallowed, so a caller
// pointing at a non-existent parent would never see writes land. Now the
// directory is created at construction time, and subsequent Puts hit disk.
func TestCacheCreatesNestedDirEagerly(t *testing.T) {
	parent := t.TempDir()
	nested := filepath.Join(parent, "new-parent", "renders")
	c := NewCache(nested)
	if c.DiskDir != nested {
		t.Fatalf("expected DiskDir=%q, got %q", nested, c.DiskDir)
	}
	info, err := os.Stat(nested)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected NewCache to MkdirAll %q: err=%v info=%+v", nested, err, info)
	}
	c.Put("k", []byte("v"))
	if _, err := os.Stat(filepath.Join(nested, "k")); err != nil {
		t.Fatalf("Put did not persist to disk: %v", err)
	}
}

// TestCacheDegradesOnUnwritableDir verifies the cache falls back to memory-
// only when the disk dir can't be created. On a read-only parent the
// MkdirAll in NewCache fails and DiskDir is zeroed, so subsequent Puts skip
// disk cleanly instead of panicking or spinning.
func TestCacheDegradesOnUnwritableDir(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Skipf("chmod: %v (non-unix?)", err)
	}
	defer os.Chmod(parent, 0o700)

	nested := filepath.Join(parent, "cannot-make-this")
	c := NewCache(nested)
	if c.DiskDir != "" {
		t.Fatalf("expected DiskDir zeroed after MkdirAll failure, got %q", c.DiskDir)
	}
	// Memory tier still works.
	c.Put("k", []byte("v"))
	got, ok := c.Get("k")
	if !ok || string(got) != "v" {
		t.Fatalf("memory-only cache broken: ok=%v got=%q", ok, got)
	}
}
