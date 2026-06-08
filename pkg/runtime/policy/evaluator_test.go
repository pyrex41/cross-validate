package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// dummyAmbient is a minimal ambient World used to exercise mergeAmbient.
var dummyAmbient = types.World{
	CRDs: []types.CRDInfo{{Group: "example.com", Kind: "Widget"}},
}

// kernelDir locates the repo's kernel/ directory by walking upward from the
// test's working directory (the package dir), so the embedded-vs-disk kernel
// resolution is deterministic in tests.
func kernelDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		cand := filepath.Join(dir, "kernel", "check.shen")
		if _, err := os.Stat(cand); err == nil {
			return filepath.Join(dir, "kernel")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate kernel/ above %s", dir)
		}
		dir = parent
	}
}

// fixture reads a fixture file relative to testdata/fixtures/.
func fixture(t *testing.T, rel string) []byte {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	var repoRoot string
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			repoRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root above working dir")
		}
		dir = parent
	}
	path := filepath.Join(repoRoot, "testdata", "fixtures", rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
}

func TestEvaluate(t *testing.T) {
	kernel := kernelDir(t)

	cases := []struct {
		name        string
		fixture     string
		ref         ObjectRef
		wantAllowed bool
		wantCode    string // expected diagnostic code (empty = none required)
	}{
		{
			name:    "R23 missing deletionPolicy Orphan blocks",
			fixture: "crossplane-state-needs-orphan/positive/cluster.yaml",
			ref: ObjectRef{
				Group: "rds.aws.upbound.io", Version: "v1beta1", Kind: "Cluster",
				Name: "aurora-prod-cluster", Namespace: "crossplane-system",
			},
			wantAllowed: false,
			wantCode:    "XPC.S.crossplane-state-needs-orphan",
		},
		{
			name:    "R23 with deletionPolicy Orphan is allowed",
			fixture: "crossplane-state-needs-orphan/orphan-ok/cluster.yaml",
			ref: ObjectRef{
				Group: "rds.aws.upbound.io", Version: "v1beta1", Kind: "Cluster",
				Name: "aurora-prod-cluster", Namespace: "crossplane-system",
			},
			wantAllowed: true,
		},
		{
			name:    "R24 appset finalizer without preserve blocks",
			fixture: "appset-finalizer-without-preserve/appset.yaml",
			ref: ObjectRef{
				Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationSet",
				Name: "crossplane-platform-aws-prod", Namespace: "argocd",
			},
			wantAllowed: false,
			wantCode:    "XPC.E.appset-finalizer-without-preserve",
		},
		{
			name:    "R24 appset finalizer with preserve is allowed",
			fixture: "appset-finalizer-with-preserve/appset.yaml",
			ref: ObjectRef{
				Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationSet",
				Name: "crossplane-platform-aws-prod", Namespace: "argocd",
			},
			wantAllowed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New(kernel, nil) // nil -> DecidableSubset
			raw := fixture(t, tc.fixture)
			dec, err := e.Evaluate(raw, tc.ref, nil)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if dec.Allowed != tc.wantAllowed {
				t.Fatalf("Allowed = %v, want %v (errors=%d warnings=%d diags=%+v)",
					dec.Allowed, tc.wantAllowed, dec.Errors, dec.Warnings, dec.Diagnostics)
			}
			if dec.Ref != tc.ref {
				t.Errorf("Ref not echoed: got %+v want %+v", dec.Ref, tc.ref)
			}
			if dec.EvalNanos <= 0 {
				t.Errorf("EvalNanos not recorded: %d", dec.EvalNanos)
			}
			if tc.wantCode != "" {
				found := false
				for _, d := range dec.Diagnostics {
					if d.Code == tc.wantCode {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected diagnostic code %q, got %+v", tc.wantCode, dec.Diagnostics)
				}
			}
		})
	}
}

func TestEvaluateDefaultSubset(t *testing.T) {
	e := New("", nil)
	if len(e.Subset) == 0 {
		t.Fatal("nil subset should default to DecidableSubset(), got empty")
	}
	want := DecidableSubset()
	if len(e.Subset) != len(want) {
		t.Fatalf("default subset size = %d, want %d", len(e.Subset), len(want))
	}
}

func TestMergeAmbientKeepsObjectResources(t *testing.T) {
	// A built world with one resource; merging ambient CRDs must not drop it.
	raw := fixture(t, "crossplane-state-needs-orphan/positive/cluster.yaml")
	e := New(kernelDir(t), nil)
	// Evaluate with an ambient world carrying a dummy CRD; the object's own
	// R23 verdict must be unchanged.
	dec, err := e.Evaluate(raw, ObjectRef{Kind: "Cluster", Name: "aurora-prod-cluster"}, &dummyAmbient)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec.Allowed {
		t.Fatalf("expected block with ambient merge, got allowed; diags=%+v", dec.Diagnostics)
	}
}
