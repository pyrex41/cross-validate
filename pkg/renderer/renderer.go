// Package renderer renders Argo CD Application sources (Helm, Kustomize, …)
// into concrete Kubernetes YAML so the rest of the checker can validate the
// actual manifests Argo will apply. This package is implementation-agnostic:
// the Renderer interface is the only contract the builder depends on.
package renderer

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Renderer turns one Argo source into rendered Kubernetes YAML bytes.
// Implementations are responsible for their own caching and external tool
// invocations. `workdir` is the directory relative to which `src.Path`
// (chart dir, kustomize dir) is resolved.
type Renderer interface {
	Render(src types.ArgoSource, workdir string) ([]byte, error)
}

// ErrHelmAbsent is returned by HelmRenderer when the helm binary cannot be
// found on PATH or the configured HelmBin path. Callers translate this into
// a warning-severity RenderResult rather than a hard error.
var ErrHelmAbsent = errors.New("renderer: helm binary absent")

// ErrRendererUnsupported is returned when a renderer is asked to handle a
// source whose Renderer kind it doesn't implement.
var ErrRendererUnsupported = errors.New("renderer: unsupported source kind")

// ResolveChart returns the absolute filesystem path to the chart directory
// for `src`, resolved relative to `cwd` (usually the directory of the
// Application YAML). For fg-manifold charts are co-located with the
// Application, so `src.Path` is treated as a relative path. Remote-repo
// resolution is deferred — if `src.Chart` is non-empty and `src.Path` is
// empty, we return ErrRendererUnsupported.
func ResolveChart(src types.ArgoSource, cwd string) (string, error) {
	if src.Path == "" {
		if src.Chart != "" {
			return "", fmt.Errorf("%w: remote Helm chart %q not supported (Path required)", ErrRendererUnsupported, src.Chart)
		}
		return "", fmt.Errorf("%w: source has no path", ErrRendererUnsupported)
	}
	if filepath.IsAbs(src.Path) {
		return filepath.Clean(src.Path), nil
	}
	return filepath.Clean(filepath.Join(cwd, src.Path)), nil
}
