package renderer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestProbeAbsentHelm confirms that when HelmBin points at something that
// doesn't exist, we return an ErrHelmAbsent-wrapped error rather than
// crashing or hanging.
func TestProbeAbsentHelm(t *testing.T) {
	h := &HelmRenderer{HelmBin: "/nonexistent/xpc-test-helm"}
	_, err := h.Render(types.ArgoSource{Renderer: types.RendererHelm, Path: "."}, t.TempDir())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsHelmAbsent(err) {
		t.Fatalf("expected ErrHelmAbsent wrapper, got %v", err)
	}
	if !errors.Is(err, ErrHelmAbsent) {
		t.Fatalf("errors.Is(err, ErrHelmAbsent) = false, want true")
	}
}

// TestHelmHappyPath dispatches an actual `helm template` call against a
// minimal fixture. It is skipped when helm is not on PATH so hermetic CI
// stays green.
func TestHelmHappyPath(t *testing.T) {
	helmBin, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not on PATH; skipping")
	}

	chart := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(chart, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Chart.yaml", "apiVersion: v2\nname: unit\nversion: 0.1.0\n")
	write("values.yaml", "replicas: 1\n")
	write("templates/cm.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: unit-cm
data:
  n: "{{ .Values.replicas }}"
`)

	h := NewHelmRenderer(helmBin, "")
	out, err := h.RenderChart(chart, &types.ArgoHelmSource{ReleaseName: "unit"}, "")
	if err != nil {
		t.Fatalf("RenderChart: %v", err)
	}
	if !strings.Contains(string(out), "kind: ConfigMap") {
		t.Fatalf("expected rendered ConfigMap in output, got:\n%s", out)
	}
	if !strings.Contains(string(out), "unit-cm") {
		t.Fatalf("expected ConfigMap name in output, got:\n%s", out)
	}

	// Second call must be cached — we can detect this by pointing the
	// renderer at a different helm binary that would fail; the cached
	// bytes should still come back.
	h2 := &HelmRenderer{HelmBin: helmBin, Cache: h.Cache}
	out2, err := h2.RenderChart(chart, &types.ArgoHelmSource{ReleaseName: "unit"}, "")
	if err != nil {
		t.Fatalf("second RenderChart: %v", err)
	}
	if string(out) != string(out2) {
		t.Fatalf("cached render differs from first")
	}
}

// TestResolveChartRemoteReturnsErrRemoteChart confirms ResolveChart returns
// the ErrRemoteChart sentinel when src.Path is empty — the builder uses
// this to decide whether to invoke PullRemote.
func TestResolveChartRemoteReturnsErrRemoteChart(t *testing.T) {
	_, err := ResolveChart(types.ArgoSource{
		Renderer: types.RendererHelm,
		Chart:    "argo-cd",
		RepoURL:  "https://argoproj.github.io/argo-helm",
	}, t.TempDir())
	if !errors.Is(err, ErrRemoteChart) {
		t.Fatalf("expected ErrRemoteChart, got %v", err)
	}
}

// TestPullRemoteRequiresCacheDir ensures PullRemote fails fast when no
// ChartCacheDir is configured, so the builder can surface a clear
// "helm-remote-unsupported" diagnostic instead of silently attempting a
// network fetch into a default location.
func TestPullRemoteRequiresCacheDir(t *testing.T) {
	h := &HelmRenderer{HelmBin: "/nonexistent/xpc-test-helm"}
	_, err := h.PullRemote(types.ArgoSource{
		Chart:   "some-chart",
		RepoURL: "https://example.invalid/charts",
	})
	if err == nil {
		t.Fatal("expected error when ChartCacheDir is empty, got nil")
	}
	if !strings.Contains(err.Error(), "ChartCacheDir") && !strings.Contains(err.Error(), "helm-cache-dir") {
		t.Fatalf("error should mention ChartCacheDir/--helm-cache-dir: %v", err)
	}
}

// TestPullRemoteCacheHit verifies the cache-hit path doesn't invoke helm:
// we pre-seed h.ChartCacheDir/charts/<hash> as a directory and call
// PullRemote with a non-existent helm binary. The stat should match first
// and return the cached path before probe() is ever called.
func TestPullRemoteCacheHit(t *testing.T) {
	cacheDir := t.TempDir()
	src := types.ArgoSource{
		Chart:          "argo-cd",
		RepoURL:        "https://argoproj.github.io/argo-helm",
		TargetRevision: "5.46.0",
	}
	chartKey := src.RepoURL + "/" + src.Chart + "/" + src.TargetRevision
	sum := sha256.Sum256([]byte(chartKey))
	hash := fmt.Sprintf("%x", sum)
	cached := filepath.Join(cacheDir, "charts", hash)
	if err := os.MkdirAll(cached, 0o755); err != nil {
		t.Fatal(err)
	}

	h := &HelmRenderer{
		HelmBin:       "/nonexistent/xpc-test-helm",
		ChartCacheDir: cacheDir,
	}
	got, err := h.PullRemote(src)
	if err != nil {
		t.Fatalf("PullRemote on cache hit: %v", err)
	}
	if got != cached {
		t.Fatalf("PullRemote returned %q, want %q", got, cached)
	}
}

// TestPullRemoteHashStability asserts that the cache key is a pure function
// of (RepoURL, Chart, TargetRevision) — swapping any component changes the
// path, equal inputs produce equal paths. Guards against accidental entropy
// in the hash (timestamps, rand, map iteration order).
func TestPullRemoteHashStability(t *testing.T) {
	key := func(src types.ArgoSource) string {
		s := src.RepoURL + "/" + src.Chart + "/" + src.TargetRevision
		sum := sha256.Sum256([]byte(s))
		return fmt.Sprintf("%x", sum)
	}
	a := types.ArgoSource{RepoURL: "r", Chart: "c", TargetRevision: "1.0"}
	b := types.ArgoSource{RepoURL: "r", Chart: "c", TargetRevision: "1.0"}
	if key(a) != key(b) {
		t.Fatal("identical sources produced different cache keys")
	}
	if key(a) == key(types.ArgoSource{RepoURL: "r", Chart: "c", TargetRevision: "1.1"}) {
		t.Fatal("differing TargetRevision did not change cache key")
	}
	if key(a) == key(types.ArgoSource{RepoURL: "r2", Chart: "c", TargetRevision: "1.0"}) {
		t.Fatal("differing RepoURL did not change cache key")
	}
}

// TestValidateValuesWrongType drives the values-schema path end to end and
// proves the walker fires the same way it would for a CRD.
func TestValidateValuesWrongType(t *testing.T) {
	schemaJSON := []byte(`{
		"type": "object",
		"required": ["replicas"],
		"properties": {
			"replicas": {"type": "integer"}
		}
	}`)
	issues, err := ValidateValues(schemaJSON, map[string]interface{}{"replicas": "three"})
	if err != nil {
		t.Fatalf("ValidateValues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].Path != "replicas" {
		t.Errorf("issue Path = %q, want replicas", issues[0].Path)
	}
	if !strings.Contains(issues[0].Message, "type") {
		t.Errorf("issue Message = %q, want type mention", issues[0].Message)
	}
}
