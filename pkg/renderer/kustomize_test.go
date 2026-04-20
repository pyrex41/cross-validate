package renderer

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestProbeAbsentKustomize confirms that when KustomizeBin points at a
// non-existent path we return an ErrKustomizeAbsent-wrapped error rather
// than crashing or hanging. Mirrors TestProbeAbsentHelm.
func TestProbeAbsentKustomize(t *testing.T) {
	k := &KustomizeRenderer{KustomizeBin: "/nonexistent/xpc-test-kustomize"}
	_, err := k.Render(types.ArgoSource{Renderer: types.RendererKustomize, Path: "."}, t.TempDir())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsKustomizeAbsent(err) {
		t.Fatalf("expected ErrKustomizeAbsent wrapper, got %v", err)
	}
	if !errors.Is(err, ErrKustomizeAbsent) {
		t.Fatalf("errors.Is(err, ErrKustomizeAbsent) = false, want true")
	}
}

// TestKustomizeHappyPath dispatches an actual `kustomize build` call
// against a minimal fixture. Skipped when kustomize is not on PATH so
// hermetic CI stays green.
func TestKustomizeHappyPath(t *testing.T) {
	kustomizeBin, err := exec.LookPath("kustomize")
	if err != nil {
		t.Skip("kustomize not on PATH; skipping")
	}

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
	write("kustomization.yaml", `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - cm.yaml
`)
	write("cm.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  x: "1"
`)

	k := NewKustomizeRenderer(kustomizeBin)
	out, err := k.RenderOverlay(dir, &types.ArgoKustomizeSource{})
	if err != nil {
		t.Fatalf("RenderOverlay: %v", err)
	}
	if !strings.Contains(string(out), "kind: ConfigMap") {
		t.Fatalf("expected rendered ConfigMap in output, got:\n%s", out)
	}
	if !strings.Contains(string(out), "test-cm") {
		t.Fatalf("expected ConfigMap name in output, got:\n%s", out)
	}

	// Second call must be cached — pointing a second renderer at the
	// same cache should serve from memory/disk.
	k2 := &KustomizeRenderer{KustomizeBin: kustomizeBin, Cache: k.Cache}
	out2, err := k2.RenderOverlay(dir, &types.ArgoKustomizeSource{})
	if err != nil {
		t.Fatalf("second RenderOverlay: %v", err)
	}
	if string(out) != string(out2) {
		t.Fatalf("cached render differs from first")
	}
}

// TestKustomizeBuildFails asserts that a broken overlay surfaces through
// RenderOverlay as a non-nil error that classifyKustomizeError can route
// to kustomize-build-failed. Requires kustomize on PATH.
func TestKustomizeBuildFails(t *testing.T) {
	kustomizeBin, err := exec.LookPath("kustomize")
	if err != nil {
		t.Skip("kustomize not on PATH; skipping")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - does-not-exist.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}

	k := NewKustomizeRenderer(kustomizeBin)
	_, err = k.RenderOverlay(dir, nil)
	if err == nil {
		t.Fatal("expected error from broken overlay, got nil")
	}
	if !strings.Contains(err.Error(), "kustomize build") {
		t.Errorf("expected 'kustomize build' in error, got %q", err.Error())
	}
}
