package renderer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// HelmTimeout caps each `helm template` invocation. A chart that can't render
// in 30s is almost certainly stuck — better to fail with a clear diagnostic
// than to stall CI.
const HelmTimeout = 30 * time.Second

// HelmPullTimeout caps `helm pull` for remote charts. Network-backed, so a
// bit more generous than template.
const HelmPullTimeout = 60 * time.Second

// HelmRenderer runs `helm template` to produce Kubernetes YAML. It is safe
// for concurrent use; each .Render call invokes helm in a fresh subprocess.
type HelmRenderer struct {
	// HelmBin is the path to the helm binary. Empty means "look up `helm`
	// on PATH at first use".
	HelmBin string
	// Cache is the two-tier render cache. Nil means "no caching".
	Cache *Cache
	// ChartCacheDir is the on-disk root for pulled remote charts. Empty
	// disables remote-chart support; PullRemote returns an error when
	// called with no ChartCacheDir.
	ChartCacheDir string

	mu         sync.Mutex
	resolved   string // cached result of resolving HelmBin
	version    string // cached `helm version --short` output
	probed     bool
	probeError error
}

// NewHelmRenderer constructs a HelmRenderer with the default disk-cache dir.
// cacheDir is the ChartCacheDir for remote chart pulls; "" disables remote.
func NewHelmRenderer(helmBin, cacheDir string) *HelmRenderer {
	return &HelmRenderer{
		HelmBin:       helmBin,
		Cache:         NewCache(""),
		ChartCacheDir: cacheDir,
	}
}

// probe looks up the helm binary and runs `helm version --short` so we can
// emit ErrHelmAbsent eagerly and fold the version into the cache key.
// Idempotent.
func (h *HelmRenderer) probe() (string, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.probed {
		return h.resolved, h.version, h.probeError
	}
	h.probed = true

	bin := h.HelmBin
	if bin == "" {
		bin = "helm"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		h.probeError = fmt.Errorf("%w: %v", ErrHelmAbsent, err)
		return "", "", h.probeError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, resolved, "version", "--short").Output()
	if err != nil {
		h.probeError = fmt.Errorf("%w: `helm version` failed: %v", ErrHelmAbsent, err)
		return "", "", h.probeError
	}
	h.resolved = resolved
	h.version = strings.TrimSpace(string(out))
	return h.resolved, h.version, nil
}

// Render is the Renderer interface. It renders src as a Helm chart located
// at src.Path relative to workdir. The destination namespace defaults to
// empty; callers that want a specific namespace (e.g. the Application's
// destination.namespace) should use RenderChart.
func (h *HelmRenderer) Render(src types.ArgoSource, workdir string) ([]byte, error) {
	if src.Renderer != types.RendererHelm {
		return nil, fmt.Errorf("%w: expected helm, got %s", ErrRendererUnsupported, src.Renderer)
	}
	chartPath, err := ResolveChart(src, workdir)
	if err != nil {
		return nil, err
	}
	return h.RenderChart(chartPath, src.Helm, "")
}

// RenderChart is the lower-level entry point used by Render and by tests
// that want to skip ArgoSource assembly. `helm` is the parsed Helm source
// config (may be nil — we treat that as "no values, no release-name").
// `namespace` is the --namespace argument.
func (h *HelmRenderer) RenderChart(chartPath string, helmSrc *types.ArgoHelmSource, namespace string) ([]byte, error) {
	bin, version, err := h.probe()
	if err != nil {
		return nil, err
	}

	releaseName := "release"
	var valueFiles []string
	var valuesObject map[string]interface{}
	var inlineValues string
	if helmSrc != nil {
		if helmSrc.ReleaseName != "" {
			releaseName = helmSrc.ReleaseName
		}
		valueFiles = helmSrc.ValueFiles
		valuesObject = helmSrc.ValuesObject
		inlineValues = helmSrc.Values
	}

	mergedValuesBytes, err := mergeValuesBytes(chartPath, valueFiles, valuesObject, inlineValues)
	if err != nil {
		return nil, err
	}

	// Cache lookup.
	var cacheKey string
	if h.Cache != nil {
		chartDigest, err := HashChartDir(chartPath)
		if err == nil {
			cacheKey = Key(CacheKeyInput{
				ChartDirDigest: chartDigest,
				ValuesBytes:    mergedValuesBytes,
				HelmVersion:    version,
				ReleaseName:    releaseName,
				Namespace:      namespace,
			})
			if data, ok := h.Cache.Get(cacheKey); ok {
				return data, nil
			}
		}
	}

	args := []string{"template", releaseName, chartPath}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	// Feed merged values as a single stdin-equivalent file so the cache key
	// matches what helm actually saw.
	for _, f := range valueFiles {
		resolved := f
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(chartPath, f)
		}
		args = append(args, "-f", resolved)
	}
	if len(valuesObject) > 0 || inlineValues != "" {
		// Write a transient values file so we get exactly one --values pass.
		tmp, err := writeTempValues(mergedValuesBytes)
		if err != nil {
			return nil, err
		}
		defer tmp.cleanup()
		args = append(args, "-f", tmp.path)
	}
	if helmSrc != nil {
		for _, p := range helmSrc.Parameters {
			if p.Name == "" {
				continue
			}
			args = append(args, "--set", fmt.Sprintf("%s=%s", p.Name, p.Value))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), HelmTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm template %s failed: %v: %s", chartPath, err, subprocessErrTail(&stdout, &stderr))
	}

	out := stdout.Bytes()
	if h.Cache != nil && cacheKey != "" {
		h.Cache.Put(cacheKey, out)
	}
	return out, nil
}

// PullRemote downloads the remote Helm chart named by src.Chart from
// src.RepoURL (optionally src.TargetRevision) into
// h.ChartCacheDir/charts/<hash>. Returns the absolute path to the untarred
// chart dir. Cache hit: stat(h.ChartCacheDir/charts/<hash>). Cache miss:
// `helm pull --repo --version --destination tmp --untar`, then rename the
// untarred chart dir into place. Requires ChartCacheDir; returns an error
// otherwise.
func (h *HelmRenderer) PullRemote(src types.ArgoSource) (string, error) {
	if h.ChartCacheDir == "" {
		return "", fmt.Errorf("remote charts require a configured ChartCacheDir (use --helm-cache-dir)")
	}
	if src.Chart == "" {
		return "", fmt.Errorf("remote chart pull requires src.Chart to be set")
	}
	if src.RepoURL == "" {
		return "", fmt.Errorf("remote chart pull requires src.RepoURL to be set")
	}
	chartKey := fmt.Sprintf("%s/%s/%s", src.RepoURL, src.Chart, src.TargetRevision)
	sum := sha256.Sum256([]byte(chartKey))
	hash := fmt.Sprintf("%x", sum)
	cached := filepath.Join(h.ChartCacheDir, "charts", hash)
	if info, err := os.Stat(cached); err == nil && info.IsDir() {
		return cached, nil
	}
	if err := os.MkdirAll(filepath.Join(h.ChartCacheDir, "charts"), 0o755); err != nil {
		return "", err
	}
	tmpRoot := filepath.Join(h.ChartCacheDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(tmpRoot, hash+"-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	bin, _, err := h.probe()
	if err != nil {
		return "", err
	}
	args := []string{"pull", src.Chart, "--repo", src.RepoURL, "--destination", tmp, "--untar"}
	if src.TargetRevision != "" {
		args = append(args, "--version", src.TargetRevision)
	}
	ctx, cancel := context.WithTimeout(context.Background(), HelmPullTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		ver := ""
		if src.TargetRevision != "" {
			ver = " --version " + src.TargetRevision
		}
		return "", fmt.Errorf("helm pull %s --repo %s%s: %v:\n%s", src.Chart, src.RepoURL, ver, err, strings.TrimSpace(string(out)))
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", fmt.Errorf("reading helm pull tmp dir %s: %w", tmp, err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return "", fmt.Errorf("helm pull produced %d entries in %s, want exactly 1 untarred chart dir", len(entries), tmp)
	}
	chartTmp := filepath.Join(tmp, entries[0].Name())
	if err := os.Rename(chartTmp, cached); err != nil {
		// A concurrent pull may have already placed the chart here; treat
		// that as a successful cache hit.
		if info, statErr := os.Stat(cached); statErr == nil && info.IsDir() {
			return cached, nil
		}
		return "", fmt.Errorf("rename %s → %s: %w", chartTmp, cached, err)
	}
	return cached, nil
}

// IsHelmAbsent reports whether err is an ErrHelmAbsent wrapper.
func IsHelmAbsent(err error) bool { return errors.Is(err, ErrHelmAbsent) }

// subprocessErrTail picks the most informative tail from a failed subprocess
// run. Prefer stderr (where helm and kustomize write template errors); fall
// back to stdout (some broken wrappers mis-route errors), and cap at 4 KiB
// so a malformed chart can't blow up a diagnostic. Shared by helm.go and
// kustomize.go — the propagation path into XPC.H.{helm,kustomize}-renders'
// Detail field is regression-tested in helm_test.go.
func subprocessErrTail(stdout, stderr *bytes.Buffer) string {
	pick := strings.TrimSpace(stderr.String())
	if pick == "" {
		pick = strings.TrimSpace(stdout.String())
	}
	if pick == "" {
		return "(no output)"
	}
	const maxTail = 4096
	if len(pick) > maxTail {
		pick = "..." + pick[len(pick)-maxTail:]
	}
	return pick
}

// mergeValuesBytes produces the canonical bytes the cache hashes and helm
// sees as the effective values. It unions (in order of precedence) the
// chart's default values.yaml, the listed valueFiles, valuesObject, and the
// inline values string. The output is sorted-key YAML.
func mergeValuesBytes(chartPath string, valueFiles []string, valuesObject map[string]interface{}, inlineValues string) ([]byte, error) {
	merged := map[string]interface{}{}

	// 1. Chart defaults (values.yaml if present).
	defaultsPath := filepath.Join(chartPath, "values.yaml")
	if b, err := readFileIfExists(defaultsPath); err == nil && len(b) > 0 {
		if err := yaml.Unmarshal(b, &merged); err != nil {
			return nil, fmt.Errorf("decoding chart values.yaml: %w", err)
		}
	}

	// 2. Listed valueFiles (paths are resolved relative to chartPath).
	for _, f := range valueFiles {
		resolved := f
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(chartPath, f)
		}
		b, err := readFileIfExists(resolved)
		if err != nil {
			return nil, fmt.Errorf("reading values file %s: %w", resolved, err)
		}
		if len(b) == 0 {
			continue
		}
		tmp := map[string]interface{}{}
		if err := yaml.Unmarshal(b, &tmp); err != nil {
			return nil, fmt.Errorf("decoding values file %s: %w", resolved, err)
		}
		merged = deepMerge(merged, tmp)
	}

	// 3. valuesObject (typed map from the Application YAML).
	if len(valuesObject) > 0 {
		merged = deepMerge(merged, valuesObject)
	}

	// 4. Inline values (string of YAML).
	if inlineValues != "" {
		tmp := map[string]interface{}{}
		if err := yaml.Unmarshal([]byte(inlineValues), &tmp); err != nil {
			return nil, fmt.Errorf("decoding inline values: %w", err)
		}
		merged = deepMerge(merged, tmp)
	}

	// Canonicalize to sorted-key JSON (stable across runs; yaml.Marshal does
	// not guarantee key order for map[string]interface{}).
	return sortedJSON(merged)
}

// MergedValues rebuilds the same merged map RenderChart uses, so callers
// (e.g. values-schema validation) can validate exactly what helm will see.
func MergedValues(chartPath string, helmSrc *types.ArgoHelmSource) (map[string]interface{}, error) {
	var valueFiles []string
	var valuesObject map[string]interface{}
	var inlineValues string
	if helmSrc != nil {
		valueFiles = helmSrc.ValueFiles
		valuesObject = helmSrc.ValuesObject
		inlineValues = helmSrc.Values
	}
	merged := map[string]interface{}{}
	if b, err := readFileIfExists(filepath.Join(chartPath, "values.yaml")); err == nil && len(b) > 0 {
		_ = yaml.Unmarshal(b, &merged)
	}
	for _, f := range valueFiles {
		resolved := f
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(chartPath, f)
		}
		if b, err := readFileIfExists(resolved); err == nil && len(b) > 0 {
			tmp := map[string]interface{}{}
			if err := yaml.Unmarshal(b, &tmp); err == nil {
				merged = deepMerge(merged, tmp)
			}
		}
	}
	if len(valuesObject) > 0 {
		merged = deepMerge(merged, valuesObject)
	}
	if inlineValues != "" {
		tmp := map[string]interface{}{}
		if err := yaml.Unmarshal([]byte(inlineValues), &tmp); err == nil {
			merged = deepMerge(merged, tmp)
		}
	}
	return merged, nil
}

func deepMerge(a, b map[string]interface{}) map[string]interface{} {
	if a == nil {
		a = map[string]interface{}{}
	}
	for k, v := range b {
		if existing, ok := a[k]; ok {
			if em, ok := existing.(map[string]interface{}); ok {
				if nm, ok := v.(map[string]interface{}); ok {
					a[k] = deepMerge(em, nm)
					continue
				}
			}
		}
		a[k] = v
	}
	return a
}

// sortedJSON serializes v as JSON with map keys sorted recursively. This
// gives us a cache-friendly canonical form.
func sortedJSON(v interface{}) ([]byte, error) {
	return json.Marshal(canonicalize(v))
}

func canonicalize(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]keyVal, 0, len(keys))
		for _, k := range keys {
			out = append(out, keyVal{Key: k, Val: canonicalize(x[k])})
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i := range x {
			out[i] = canonicalize(x[i])
		}
		return out
	}
	return v
}

type keyVal struct {
	Key string
	Val interface{}
}

func (kv keyVal) MarshalJSON() ([]byte, error) {
	inner, err := json.Marshal(kv.Val)
	if err != nil {
		return nil, err
	}
	keyEnc, err := json.Marshal(kv.Key)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	buf.Write(keyEnc)
	buf.WriteByte(':')
	buf.Write(inner)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func readFileIfExists(path string) ([]byte, error) {
	b, err := readFile(path)
	if err != nil && isNotExist(err) {
		return nil, nil
	}
	return b, err
}
