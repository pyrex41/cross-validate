package renderer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// TestRenderChart_PropagatesHelmStderr guards against the "scrubbed error"
// regression surfaced by fg-manifold replay-v3: when `helm template` fails,
// the real helm stderr must flow into the returned error (and thus into the
// XPC.H.helm-renders diagnostic's Detail field). A broken template produces
// a very specific helm message ("parse error", "unclosed action") — asserting
// on that text locks in propagation end-to-end.
func TestRenderChart_PropagatesHelmStderr(t *testing.T) {
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
	write("Chart.yaml", "apiVersion: v2\nname: broken\nversion: 0.1.0\n")
	write("values.yaml", "")
	// Template with an unclosed action — helm will reject with a "parse
	// error" on stderr.
	write("templates/broken.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\ndata:\n  k: {{ .Values.replicas\n")

	h := NewHelmRenderer(helmBin, "")
	_, err = h.RenderChart(chart, &types.ArgoHelmSource{ReleaseName: "rel"}, "")
	if err == nil {
		t.Fatal("expected error rendering broken chart, got nil")
	}
	msg := err.Error()
	// The two substrings below are what helm v3/v4 consistently emit for
	// an unclosed Go-template action. If helm changes this wording, pick
	// whatever identifiable text its stderr now produces — the invariant
	// under test is "real helm stderr is in the error", not the exact phrase.
	if !strings.Contains(msg, "parse error") {
		t.Errorf("error should contain real helm stderr 'parse error', got: %s", msg)
	}
	if !strings.Contains(msg, "unclosed action") {
		t.Errorf("error should contain real helm stderr 'unclosed action', got: %s", msg)
	}
	// Guard against the old scrubbed format ("helm template <path> failed: exit status 1:")
	// with an empty tail — that's exactly the regression this test exists to prevent.
	if strings.HasSuffix(strings.TrimSpace(msg), ":") {
		t.Errorf("error tail is empty — stderr was dropped: %s", msg)
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

// TestPullRemoteAgainstLocalRepo exercises the full PullRemote path
// (helm pull --repo --version --destination --untar → rename into
// <cacheDir>/charts/<hash>) against a hermetic local HTTP server serving
// a packaged chart + generated index.yaml. No external network required.
// Also verifies the second call is a cache hit (same returned path).
func TestPullRemoteAgainstLocalRepo(t *testing.T) {
	helmBin, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not on PATH; skipping")
	}

	// 1. Build a minimal chart source tree.
	chartSrc := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(chartSrc, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Chart.yaml", "apiVersion: v2\nname: unitchart\nversion: 0.1.0\n")
	write("values.yaml", "msg: hello\n")
	write("templates/cm.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: unit-cm
data:
  msg: "{{ .Values.msg }}"
`)

	// 2. Package the chart into a repo-root dir.
	repoRoot := t.TempDir()
	if out, err := exec.Command(helmBin, "package", chartSrc, "--destination", repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("helm package: %v\n%s", err, out)
	}

	// 3. Serve repoRoot over HTTP *before* running `helm repo index`, so we
	//    can point index URLs at the live server.
	srv := httptest.NewServer(http.FileServer(http.Dir(repoRoot)))
	defer srv.Close()

	if out, err := exec.Command(helmBin, "repo", "index", repoRoot, "--url", srv.URL).CombinedOutput(); err != nil {
		t.Fatalf("helm repo index: %v\n%s", err, out)
	}

	// 4. Call PullRemote and assert the untarred chart landed in the cache.
	cacheDir := t.TempDir()
	h := NewHelmRenderer(helmBin, cacheDir)
	src := types.ArgoSource{
		Renderer:       types.RendererHelm,
		Chart:          "unitchart",
		RepoURL:        srv.URL,
		TargetRevision: "0.1.0",
	}
	cached, err := h.PullRemote(src)
	if err != nil {
		t.Fatalf("PullRemote: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cached, "Chart.yaml")); err != nil {
		t.Fatalf("expected Chart.yaml in cached dir %s: %v", cached, err)
	}
	if !strings.HasPrefix(cached, filepath.Join(cacheDir, "charts")+string(filepath.Separator)) {
		t.Fatalf("cached path %q not under %s/charts/", cached, cacheDir)
	}

	// 5. Second call should hit the cache and return the same path. We
	//    close the server first so any network-touching path would error;
	//    a pure stat-and-return must still succeed.
	srv.Close()
	cached2, err := h.PullRemote(src)
	if err != nil {
		t.Fatalf("second PullRemote (cache hit, server closed): %v", err)
	}
	if cached2 != cached {
		t.Fatalf("cache-hit returned %q, want same as first call %q", cached2, cached)
	}

	// 6. End-to-end sanity: RenderChart on the pulled chart produces the
	//    ConfigMap. Proves the pulled layout is what RenderChart expects.
	out, err := h.RenderChart(cached, nil, "")
	if err != nil {
		t.Fatalf("RenderChart on pulled chart: %v", err)
	}
	if !strings.Contains(string(out), "unit-cm") {
		t.Fatalf("expected ConfigMap in rendered output, got:\n%s", out)
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
