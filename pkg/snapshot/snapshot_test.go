package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestNewSnapshot(t *testing.T) {
	s := New("test-cluster")
	if s.ClusterName != "test-cluster" {
		t.Errorf("expected cluster name test-cluster, got %s", s.ClusterName)
	}
	if s.Version != 1 {
		t.Errorf("expected version 1, got %d", s.Version)
	}
	if s.Schemas == nil {
		t.Error("expected non-nil schemas map")
	}
}

func TestComputeDigest(t *testing.T) {
	s := New("test")
	s.CRDs = []types.CRDInfo{{
		Group: "example.com",
		Kind:  "Foo",
		Versions: []types.CRDVersion{{
			Name:    "v1",
			Served:  true,
			Storage: true,
		}},
	}}

	d1 := s.ComputeDigest()
	if d1 == "" {
		t.Error("expected non-empty digest")
	}

	// Same content should produce same digest
	d2 := s.ComputeDigest()
	if d1 != d2 {
		t.Errorf("expected stable digest, got %s then %s", d1, d2)
	}

	// Different content should produce different digest
	s.CRDs = append(s.CRDs, types.CRDInfo{
		Group: "example.com",
		Kind:  "Bar",
	})
	d3 := s.ComputeDigest()
	if d1 == d3 {
		t.Error("expected different digest for different content")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.xpcsnap")

	s := New("test")
	s.CRDs = []types.CRDInfo{{
		Group: "example.com",
		Kind:  "Foo",
		Versions: []types.CRDVersion{{
			Name:    "v1",
			Served:  true,
			Storage: true,
		}},
	}}
	s.ComputeDigest()

	if err := s.Save(path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.Digest != s.Digest {
		t.Errorf("digest mismatch: %s vs %s", loaded.Digest, s.Digest)
	}
	if loaded.ClusterName != s.ClusterName {
		t.Errorf("cluster name mismatch: %s vs %s", loaded.ClusterName, s.ClusterName)
	}
	if len(loaded.CRDs) != len(s.CRDs) {
		t.Errorf("CRD count mismatch: %d vs %d", len(loaded.CRDs), len(s.CRDs))
	}
}

func TestVerify(t *testing.T) {
	s := New("test")
	s.CRDs = []types.CRDInfo{{Group: "example.com", Kind: "Foo"}}
	s.ComputeDigest()

	if !s.Verify() {
		t.Error("expected verification to pass")
	}

	// Tamper with data
	s.CRDs = append(s.CRDs, types.CRDInfo{Group: "example.com", Kind: "Bar"})
	savedDigest := s.Digest
	if s.Verify() && s.Digest == savedDigest {
		t.Error("expected verification to detect tampering")
	}
}

func TestIsStale(t *testing.T) {
	s := New("test")
	s.Timestamp = time.Now().Add(-20 * time.Minute)

	if !s.IsStale(DefaultTTL) {
		t.Error("expected 20-minute-old snapshot to be stale")
	}

	s.Timestamp = time.Now().Add(-5 * time.Minute)
	if s.IsStale(DefaultTTL) {
		t.Error("expected 5-minute-old snapshot to be fresh")
	}
}

func TestDiff(t *testing.T) {
	a := New("cluster-a")
	a.CRDs = []types.CRDInfo{{
		Group: "example.com",
		Kind:  "Foo",
		Versions: []types.CRDVersion{{Name: "v1", Served: true, Storage: true}},
	}}
	a.ComputeDigest()

	b := New("cluster-b")
	b.CRDs = []types.CRDInfo{
		{
			Group: "example.com",
			Kind:  "Foo",
			Versions: []types.CRDVersion{
				{Name: "v1", Served: true, Storage: false},
				{Name: "v2", Served: true, Storage: true},
			},
		},
		{
			Group: "example.com",
			Kind:  "Bar",
		},
	}
	b.ComputeDigest()

	diff := Diff(a, b)
	if diff == "" {
		t.Error("expected non-empty diff")
	}
}

func TestFromWorld(t *testing.T) {
	w := types.NewWorld()
	w.CRDs = []types.CRDInfo{{Group: "example.com", Kind: "Foo"}}
	w.Providers = []types.ProviderInfo{{Name: "provider-aws", Package: "xpkg:v1"}}
	w.Functions = []types.FunctionInfo{{Name: "function-pt", Package: "xpkg:v1"}}

	s := FromWorld(w, "test")
	if len(s.CRDs) != 1 {
		t.Errorf("expected 1 CRD, got %d", len(s.CRDs))
	}
	if len(s.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(s.Providers))
	}
	if len(s.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(s.Functions))
	}
	if s.Digest == "" {
		t.Error("expected non-empty digest")
	}
}

func TestToWorld(t *testing.T) {
	s := New("test")
	s.CRDs = []types.CRDInfo{{Group: "example.com", Kind: "Foo"}}
	s.XRDs = []types.CRDInfo{{Group: "example.com", Kind: "XFoo", IsXRD: true}}
	s.Providers = []ProviderStatus{{
		ProviderInfo: types.ProviderInfo{Name: "prov"},
		Healthy:      true,
	}}

	w := s.ToWorld()
	if len(w.CRDs) != 1 {
		t.Errorf("expected 1 CRD, got %d", len(w.CRDs))
	}
	if len(w.XRDs) != 1 {
		t.Errorf("expected 1 XRD, got %d", len(w.XRDs))
	}
	if len(w.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(w.Providers))
	}
}

func TestLoadNonexistent(t *testing.T) {
	_, err := Load("/nonexistent/path")
	if err == nil {
		t.Error("expected error loading nonexistent file")
	}
}

func TestSaveToInvalidPath(t *testing.T) {
	s := New("test")
	err := s.Save("/nonexistent/dir/file.json")
	if err == nil {
		t.Error("expected error saving to invalid path")
	}
}

func TestDiffSameSnapshot(t *testing.T) {
	s := New("test")
	s.CRDs = []types.CRDInfo{{Group: "example.com", Kind: "Foo"}}
	s.ComputeDigest()

	diff := Diff(s, s)
	if diff == "" {
		t.Error("expected non-empty diff output even for same snapshot")
	}
}

func TestSaveCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.xpcsnap")

	s := New("test")
	s.ComputeDigest()

	if err := s.Save(path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to exist after save")
	}
}
