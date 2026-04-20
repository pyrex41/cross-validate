package renderer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// KustomizeTimeout caps each `kustomize build` invocation. The 30s ceiling
// matches HelmTimeout so users only have one number to remember.
const KustomizeTimeout = 30 * time.Second

// ErrKustomizeAbsent is returned by KustomizeRenderer when the kustomize
// binary cannot be found on PATH or the configured KustomizeBin path.
// Callers translate this into a warning-severity RenderResult rather than
// a hard error, mirroring ErrHelmAbsent.
var ErrKustomizeAbsent = errors.New("renderer: kustomize binary absent")

// KustomizeRenderer runs `kustomize build` to produce Kubernetes YAML. Safe
// for concurrent use; each .Render call shells out in a fresh subprocess.
// The shape of this type mirrors HelmRenderer; we intentionally do NOT
// abstract a shared helper — two concrete renderers do not define a line.
type KustomizeRenderer struct {
	// KustomizeBin is the path to the kustomize binary. Empty means "look
	// up `kustomize` on PATH at first use".
	KustomizeBin string
	// Cache is the two-tier render cache. Nil means "no caching".
	Cache *Cache

	mu         sync.Mutex
	resolved   string
	version    string
	probed     bool
	probeError error
}

// NewKustomizeRenderer constructs a KustomizeRenderer with the default
// disk-cache dir.
func NewKustomizeRenderer(kustomizeBin string) *KustomizeRenderer {
	return &KustomizeRenderer{
		KustomizeBin: kustomizeBin,
		Cache:        NewCache(""),
	}
}

// probe looks up the kustomize binary and runs `kustomize version` so we can
// emit ErrKustomizeAbsent eagerly and fold the version into the cache key.
// Idempotent.
func (k *KustomizeRenderer) probe() (string, string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.probed {
		return k.resolved, k.version, k.probeError
	}
	k.probed = true

	bin := k.KustomizeBin
	if bin == "" {
		bin = "kustomize"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		k.probeError = fmt.Errorf("%w: %v", ErrKustomizeAbsent, err)
		return "", "", k.probeError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, resolved, "version").Output()
	if err != nil {
		k.probeError = fmt.Errorf("%w: `kustomize version` failed: %v", ErrKustomizeAbsent, err)
		return "", "", k.probeError
	}
	k.resolved = resolved
	k.version = strings.TrimSpace(string(out))
	return k.resolved, k.version, nil
}

// Render is the Renderer interface. It renders src as a Kustomize overlay
// located at src.Path relative to workdir.
func (k *KustomizeRenderer) Render(src types.ArgoSource, workdir string) ([]byte, error) {
	if src.Renderer != types.RendererKustomize {
		return nil, fmt.Errorf("%w: expected kustomize, got %s", ErrRendererUnsupported, src.Renderer)
	}
	overlayPath, err := ResolveChart(src, workdir)
	if err != nil {
		return nil, err
	}
	return k.RenderOverlay(overlayPath, src.Kustomize)
}

// RenderOverlay is the lower-level entry point used by Render and by tests
// that want to skip ArgoSource assembly. `kustSrc` is the parsed Kustomize
// source config (may be nil — we treat that as "no overrides").
func (k *KustomizeRenderer) RenderOverlay(overlayPath string, kustSrc *types.ArgoKustomizeSource) ([]byte, error) {
	bin, version, err := k.probe()
	if err != nil {
		return nil, err
	}

	// Cache lookup. The overlay tree digest plus the kustomize version plus
	// a small summary of the Argo-side overrides constitute the key.
	var cacheKey string
	if k.Cache != nil {
		treeDigest, err := HashKustomizeDir(overlayPath)
		if err == nil {
			cacheKey = kustomizeKey(treeDigest, version, kustSrc)
			if data, ok := k.Cache.Get(cacheKey); ok {
				return data, nil
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), KustomizeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "build", overlayPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kustomize build %s failed: %v: %s", overlayPath, err, strings.TrimSpace(stderr.String()))
	}

	out := stdout.Bytes()
	if k.Cache != nil && cacheKey != "" {
		k.Cache.Put(cacheKey, out)
	}
	return out, nil
}

// IsKustomizeAbsent reports whether err is an ErrKustomizeAbsent wrapper.
func IsKustomizeAbsent(err error) bool { return errors.Is(err, ErrKustomizeAbsent) }

// HashKustomizeDir computes a stable SHA-256 of the contents of an overlay
// directory by walking its file tree in sorted order. Symlinks are followed
// once; errors surface so the caller can decide whether to bail or treat
// them as a cache miss. Mirrors HashChartDir.
func HashKustomizeDir(dir string) (string, error) {
	h := sha256.New()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
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
		rel, err := filepath.Rel(dir, f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "file:%s\n", rel)
		data, err := readFile(f)
		if err != nil {
			return "", err
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// kustomizeKey is the cache key builder for Kustomize renders. Separate from
// Key/CacheKeyInput (which is Helm-shaped) because Kustomize's inputs do not
// overlap with Helm's enough to share a struct. The `overrides` field folds
// in the Argo-side overlay tweaks so a namePrefix change busts the cache.
func kustomizeKey(treeDigest, version string, kustSrc *types.ArgoKustomizeSource) string {
	h := sha256.New()
	fmt.Fprintf(h, "tree:%s\n", treeDigest)
	fmt.Fprintf(h, "kustomize:%s\n", version)
	if kustSrc != nil {
		fmt.Fprintf(h, "namePrefix:%s\n", kustSrc.NamePrefix)
		fmt.Fprintf(h, "nameSuffix:%s\n", kustSrc.NameSuffix)
		imgs := append([]string(nil), kustSrc.Images...)
		sort.Strings(imgs)
		for _, im := range imgs {
			fmt.Fprintf(h, "image:%s\n", im)
		}
		labelKeys := make([]string, 0, len(kustSrc.CommonLabels))
		for k := range kustSrc.CommonLabels {
			labelKeys = append(labelKeys, k)
		}
		sort.Strings(labelKeys)
		for _, lk := range labelKeys {
			fmt.Fprintf(h, "label:%s=%s\n", lk, kustSrc.CommonLabels[lk])
		}
		annKeys := make([]string, 0, len(kustSrc.CommonAnnotations))
		for k := range kustSrc.CommonAnnotations {
			annKeys = append(annKeys, k)
		}
		sort.Strings(annKeys)
		for _, ak := range annKeys {
			fmt.Fprintf(h, "annotation:%s=%s\n", ak, kustSrc.CommonAnnotations[ak])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
