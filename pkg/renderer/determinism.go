// determinism.go — render each source twice and compare bytes.
//
// Non-determinism is a warning, not an error: fg-manifold legitimately uses
// `randAlphaNum` in some charts. The R20 kernel rule documents offenders so
// the team can decide which to suppress and which to fix.
//
// This module is *offline-safe*: if the renderer can't produce output (e.g.
// the binary is absent) we skip silently — R18 / the kustomize-renders path
// already surface that condition. Double-rendering an unrenderable source
// would just produce two matching "failed" results, which is noise.

package renderer

import (
	"bytes"
	"fmt"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// DoubleRenderHelm renders a Helm source twice through the given renderer
// and returns (mismatch, diffSummary, err). `err` is non-nil only when the
// render itself errored; in that case the caller should skip rather than
// flag a determinism mismatch.
//
// Caching is intentionally disabled on the second call — a cache hit would
// mask true non-determinism. We do not mutate the caller's renderer; a
// local, cache-less copy does the work.
func DoubleRenderHelm(h *HelmRenderer, chartPath, namespace string, helmSrc *types.ArgoHelmSource) (bool, string, error) {
	noCache := &HelmRenderer{HelmBin: h.HelmBin, ChartCacheDir: ""}
	a, err := noCache.RenderChart(chartPath, helmSrc, namespace)
	if err != nil {
		return false, "", err
	}
	b, err := noCache.RenderChart(chartPath, helmSrc, namespace)
	if err != nil {
		return false, "", err
	}
	if bytes.Equal(a, b) {
		return false, "", nil
	}
	return true, summarizeDiff(a, b), nil
}

// DoubleRenderKustomize renders a Kustomize overlay twice and returns the
// same triple as DoubleRenderHelm.
func DoubleRenderKustomize(k *KustomizeRenderer, overlayPath string, kustSrc *types.ArgoKustomizeSource) (bool, string, error) {
	noCache := &KustomizeRenderer{KustomizeBin: k.KustomizeBin}
	a, err := noCache.RenderOverlay(overlayPath, kustSrc)
	if err != nil {
		return false, "", err
	}
	b, err := noCache.RenderOverlay(overlayPath, kustSrc)
	if err != nil {
		return false, "", err
	}
	if bytes.Equal(a, b) {
		return false, "", nil
	}
	return true, summarizeDiff(a, b), nil
}

// summarizeDiff builds a one-line summary of the first divergence between
// two render outputs. Keeps diagnostic messages short; the full YAML payload
// would bury the signal.
func summarizeDiff(a, b []byte) string {
	if len(a) != len(b) {
		return fmt.Sprintf("outputs differ in length: %d vs %d bytes", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			start := i - 16
			if start < 0 {
				start = 0
			}
			end := i + 16
			if end > len(a) {
				end = len(a)
			}
			return fmt.Sprintf("outputs differ at byte %d: %q vs %q", i, string(a[start:end]), string(b[start:end]))
		}
	}
	return "outputs differ (summary unavailable)"
}
