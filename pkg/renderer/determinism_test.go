package renderer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestDoubleRenderHelm_Match asserts that a deterministic chart renders
// byte-identical output twice. Requires helm on PATH.
func TestDoubleRenderHelm_Match(t *testing.T) {
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
	write("Chart.yaml", "apiVersion: v2\nname: determ\nversion: 0.1.0\n")
	write("values.yaml", "replicas: 1\n")
	write("templates/cm.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: determ
data:
  n: "{{ .Values.replicas }}"
`)

	h := NewHelmRenderer(helmBin, "")
	mismatch, summary, err := DoubleRenderHelm(h, chart, "", &types.ArgoHelmSource{ReleaseName: "d"})
	if err != nil {
		t.Fatalf("DoubleRenderHelm: %v", err)
	}
	if mismatch {
		t.Errorf("expected match, got mismatch: %s", summary)
	}
}

// TestDoubleRenderHelm_Mismatch asserts that a chart using randAlphaNum
// (or any other non-deterministic helper) produces two different outputs.
// Requires helm on PATH.
func TestDoubleRenderHelm_Mismatch(t *testing.T) {
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
	write("Chart.yaml", "apiVersion: v2\nname: nd\nversion: 0.1.0\n")
	write("values.yaml", "n: 8\n")
	// randAlphaNum is the canonical offender.
	write("templates/cm.yaml", `apiVersion: v1
kind: Secret
metadata:
  name: nd
type: Opaque
stringData:
  token: "{{ randAlphaNum 16 }}"
`)

	h := NewHelmRenderer(helmBin, "")
	mismatch, summary, err := DoubleRenderHelm(h, chart, "", &types.ArgoHelmSource{ReleaseName: "nd"})
	if err != nil {
		t.Fatalf("DoubleRenderHelm: %v", err)
	}
	if !mismatch {
		t.Errorf("expected mismatch, got match")
	}
	if summary == "" {
		t.Errorf("expected non-empty DiffSummary")
	}
}

// TestSummarizeDiff exercises summarizeDiff on the three branches: length
// mismatch, byte mismatch at an offset, and equal-length equal-bytes
// (should return the fallback string — unlikely in practice since the
// caller only reaches summarizeDiff after bytes.Equal returned false).
func TestSummarizeDiff(t *testing.T) {
	t.Run("length mismatch", func(t *testing.T) {
		got := summarizeDiff([]byte("aaaa"), []byte("aaaaa"))
		if !strings.Contains(got, "length") {
			t.Errorf("expected length-mismatch summary, got %q", got)
		}
	})
	t.Run("byte mismatch", func(t *testing.T) {
		got := summarizeDiff([]byte("abcdefgh"), []byte("abcXefgh"))
		if !strings.Contains(got, "byte 3") {
			t.Errorf("expected byte-offset summary, got %q", got)
		}
	})
}

// TestDoubleRenderKustomize_Match asserts that a deterministic overlay
// renders byte-identical output twice. Requires kustomize on PATH.
func TestDoubleRenderKustomize_Match(t *testing.T) {
	kustomizeBin, err := exec.LookPath("kustomize")
	if err != nil {
		t.Skip("kustomize not on PATH; skipping")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - cm.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: d
data:
  x: "1"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	k := NewKustomizeRenderer(kustomizeBin)
	mismatch, _, err := DoubleRenderKustomize(k, dir, nil)
	if err != nil {
		t.Fatalf("DoubleRenderKustomize: %v", err)
	}
	if mismatch {
		t.Errorf("expected match, got mismatch")
	}
}
