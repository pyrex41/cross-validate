package renderer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CompositionRenderTimeout caps each `crossplane render` invocation. Same
// rationale as HelmTimeout: a composition that can't render in 30s is stuck.
const CompositionRenderTimeout = 30 * time.Second

// ErrCrossplaneAbsent is returned by CompositionRenderer when the crossplane
// binary cannot be found on PATH or the configured CrossplaneBin path.
// Callers translate this into a warning-severity result rather than a hard
// error, mirroring ErrHelmAbsent.
var ErrCrossplaneAbsent = errors.New("renderer: crossplane binary absent")

// CompositionRenderer wraps the `crossplane render` CLI. It is safe for
// concurrent use; each Render call invokes crossplane in a fresh subprocess.
// Same shape as HelmRenderer — absent-binary sentinel, process-probe, two-tier
// cache — so integration and testing patterns carry over.
type CompositionRenderer struct {
	// CrossplaneBin is the path to the crossplane binary. Empty means "look
	// up `crossplane` on PATH at first use".
	CrossplaneBin string
	// Cache is the two-tier render cache. Nil means "no caching".
	Cache *CompositionCache

	mu         sync.Mutex
	resolved   string
	version    string
	probed     bool
	probeError error
}

// NewCompositionRenderer constructs a CompositionRenderer with the default
// disk-cache dir (~/.cache/xpc/compositions/). Pass a non-empty bin to pin
// an explicit crossplane binary path.
func NewCompositionRenderer(bin string) *CompositionRenderer {
	return &CompositionRenderer{
		CrossplaneBin: bin,
		Cache:         NewCompositionCache(""),
	}
}

// probe looks up the crossplane binary and runs `crossplane version` so we
// can emit ErrCrossplaneAbsent eagerly and fold the version into the cache
// key. Idempotent.
func (r *CompositionRenderer) probe() (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.probed {
		return r.resolved, r.version, r.probeError
	}
	r.probed = true

	bin := r.CrossplaneBin
	if bin == "" {
		bin = "crossplane"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		r.probeError = fmt.Errorf("%w: %v", ErrCrossplaneAbsent, err)
		return "", "", r.probeError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, resolved, "version").Output()
	if err != nil {
		// Some builds respond to `version`, others to `--version`. Try
		// the flag form before giving up.
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		out, err = exec.CommandContext(ctx2, resolved, "--version").Output()
		if err != nil {
			r.probeError = fmt.Errorf("%w: `crossplane version` failed: %v", ErrCrossplaneAbsent, err)
			return "", "", r.probeError
		}
	}
	r.resolved = resolved
	r.version = strings.TrimSpace(string(out))
	return r.resolved, r.version, nil
}

// Render invokes `crossplane render` against the given XR, Composition, and
// function-bindings YAML bytes. Returns the raw stdout (concatenated
// rendered manifests, YAML stream separated by ---).
//
// The input shapes are whatever `crossplane render` expects:
//   - xrYAML: a single XR document
//   - compYAML: a single Composition document
//   - fnsYAML: a YAML stream of Function resources with
//     `render.crossplane.io/runtime` annotations
//
// When no cache is configured, each call spawns a subprocess unconditionally.
// When cached, the key is SHA-256 over all three inputs plus the crossplane
// version.
func (r *CompositionRenderer) Render(xrYAML, compYAML, fnsYAML []byte) ([]byte, error) {
	bin, version, err := r.probe()
	if err != nil {
		return nil, err
	}

	var cacheKey string
	if r.Cache != nil {
		cacheKey = compositionKey(version, xrYAML, compYAML, fnsYAML)
		if cached, ok := r.Cache.Get(cacheKey); ok {
			return cached, nil
		}
	}

	// crossplane render consumes file paths — dump inputs into a temp dir.
	tmpDir, err := os.MkdirTemp("", "xpc-composition-")
	if err != nil {
		return nil, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	xrPath := filepath.Join(tmpDir, "xr.yaml")
	compPath := filepath.Join(tmpDir, "composition.yaml")
	fnsPath := filepath.Join(tmpDir, "functions.yaml")
	for _, w := range []struct {
		path string
		data []byte
	}{
		{xrPath, xrYAML},
		{compPath, compYAML},
		{fnsPath, fnsYAML},
	} {
		if err := os.WriteFile(w.path, w.data, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", w.path, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), CompositionRenderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "render", xrPath, compPath, fnsPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("crossplane render: timed out after %s", CompositionRenderTimeout)
		}
		return nil, fmt.Errorf("crossplane render: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	out := stdout.Bytes()
	if r.Cache != nil {
		r.Cache.Put(cacheKey, out)
	}
	return out, nil
}

// compositionKey mixes the crossplane version with the raw inputs. Each
// field is bracketed by a length prefix so re-ordering or concatenation
// can't collide two distinct inputs onto the same hash.
func compositionKey(version string, parts ...[]byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "crossplane:%s\n", version)
	// Sort by hash-of-part for stability regardless of caller's argument
	// order — composition first or functions first should produce the same
	// cache entry. Callers always pass (xr, comp, fns) today, but locking
	// the ordering here avoids a future bug.
	hashes := make([]string, 0, len(parts))
	byHash := map[string][]byte{}
	for _, p := range parts {
		sum := sha256.Sum256(p)
		k := hex.EncodeToString(sum[:])
		hashes = append(hashes, k)
		byHash[k] = p
	}
	sort.Strings(hashes)
	for _, k := range hashes {
		p := byHash[k]
		fmt.Fprintf(h, "part:%d:", len(p))
		h.Write(p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ClassifyCompositionError maps a composition-render error to the lowercase-
// dashed symbol the Shen kernel pattern-matches on.
func ClassifyCompositionError(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, ErrCrossplaneAbsent) {
		return "crossplane-absent"
	}
	msg := err.Error()
	if strings.Contains(msg, "timed out") {
		return "crossplane-timeout"
	}
	return "crossplane-render-failed"
}
