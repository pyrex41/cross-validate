package ir

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/loader"
)

// writeFile creates path (and parents) with contents.
func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSourcePathCwd_RepoRootResolution is the regression test for the
// 6b0323d6fe / INC-2734 follow-on miss: Argo resolves a same-repo
// source.path against the REPO ROOT, but xpc joined it to the Application
// file's directory. In the fg-manifold layout (apps under
// deploy/.../applicationsets/, charts under lib/charts/) every chart path
// resolved to a nonexistent directory, so R18 reported "repo not found"
// noise instead of the chart's real template error — and on a healthy tree
// it would have been a false positive.
func TestSourcePathCwd_RepoRootResolution(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Chart at the repo root, app file two levels down — fg-manifold's shape.
	writeFile(t, filepath.Join(root, "lib/charts/dummy/Chart.yaml"),
		"apiVersion: v2\nname: dummy\nversion: 0.1.0\n")
	writeFile(t, filepath.Join(root, "lib/charts/dummy/values.yaml"), "name: x\n")
	writeFile(t, filepath.Join(root, "lib/charts/dummy/templates/cm.yaml"),
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Values.name }}\n")
	appFile := filepath.Join(root, "deploy/ops/applicationsets/app.yaml")
	writeFile(t, appFile, `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: repo-root-chart
  namespace: argocd
spec:
  project: default
  destination:
    server: https://kubernetes.default.svc
    namespace: ns
  sources:
    - repoURL: https://example.com/repo.git
      targetRevision: main
      ref: values
    - repoURL: https://example.com/repo.git
      targetRevision: main
      path: lib/charts/dummy
      helm:
        valueFiles:
          - $values/deploy/ops/app-values.yaml
`)
	writeFile(t, filepath.Join(root, "deploy/ops/app-values.yaml"), "name: from-values\n")

	t.Run("sourcePathCwd prefers appdir, falls back to repo root", func(t *testing.T) {
		appDir := filepath.Dir(appFile)
		if got := sourcePathCwd(appFile, "lib/charts/dummy"); got != root {
			t.Errorf("repo-root chart: got %q, want %q", got, root)
		}
		// `./` exists relative to the app file — must stay app-dir-relative
		// (fixtures and Argo's own `path: ./` convention).
		if got := sourcePathCwd(appFile, "./"); got != appDir {
			t.Errorf("./: got %q, want app dir %q", got, appDir)
		}
		// Nonexistent everywhere — keep the app dir and let the render error.
		if got := sourcePathCwd(appFile, "no/such/chart"); got != appDir {
			t.Errorf("missing: got %q, want app dir %q", got, appDir)
		}
	})

	t.Run("R18 renders a repo-root chart", func(t *testing.T) {
		helmBin, err := exec.LookPath("helm")
		if err != nil {
			t.Skip("helm not on PATH")
		}
		docs, err := loader.LoadDirectory(filepath.Join(root, "deploy"))
		if err != nil {
			t.Fatal(err)
		}
		b := NewBuilder()
		b.HelmBin = helmBin
		world, err := b.Build(docs)
		if err != nil {
			t.Fatal(err)
		}
		if len(world.RenderResults) != 1 {
			t.Fatalf("expected 1 RenderResult, got %d", len(world.RenderResults))
		}
		r := world.RenderResults[0]
		if !r.Success {
			t.Errorf("render should succeed against the repo-root chart, got error: %s", r.Error)
		}
	})
}
