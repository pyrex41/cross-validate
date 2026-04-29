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
	if s.Verify() {
		t.Error("expected verification to detect tampering")
	}
	if s.Digest != savedDigest {
		t.Errorf("Verify mutated Digest: got %s want %s", s.Digest, savedDigest)
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
		Group:    "example.com",
		Kind:     "Foo",
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

func TestFromWorldWithResources(t *testing.T) {
	w := types.NewWorld()
	w.Resources = []types.ResourceInfo{
		{
			APIVersion: "example.com/v1",
			Kind:       "Foo",
			Namespace:  "ns-a",
			Name:       "foo-1",
		},
		{
			APIVersion: "other.example.com/v1",
			Kind:       "Bar",
			Namespace:  "",
			Name:       "bar-cluster",
		},
	}
	w.ArgoApps = []types.ArgoApplication{{
		Name:         "app-a",
		Namespace:    "argocd",
		TrackingMode: "annotation",
	}}
	w.ArgoAppSets = []types.ArgoApplicationSet{{
		Name: "appset-a",
		Template: types.ArgoAppSetTemplate{
			Name:      "child-{{name}}",
			Namespace: "argocd",
		},
	}}
	w.ArgoProjects = []types.ArgoAppProject{{
		Name: "proj-a",
	}}

	s := FromWorldWithOptions(w, "test", FromWorldOptions{IncludeResources: true})

	if len(s.Resources) != 2 {
		t.Fatalf("expected 2 resources on snapshot, got %d", len(s.Resources))
	}
	if len(s.ArgoApps) != 1 || s.ArgoApps[0].Name != "app-a" {
		t.Fatalf("argo apps not carried: %+v", s.ArgoApps)
	}
	if len(s.ArgoAppSets) != 1 || s.ArgoAppSets[0].Name != "appset-a" {
		t.Fatalf("argo appsets not carried: %+v", s.ArgoAppSets)
	}
	if len(s.ArgoProjects) != 1 || s.ArgoProjects[0].Name != "proj-a" {
		t.Fatalf("argo projects not carried: %+v", s.ArgoProjects)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "round-trip.xpcsnap")
	if err := s.Save(path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	w2 := loaded.ToWorld()
	if len(w2.Resources) != len(w.Resources) {
		t.Errorf("resources count mismatch after round-trip: got %d want %d",
			len(w2.Resources), len(w.Resources))
	}
	for i := range w.Resources {
		got, want := w2.Resources[i], w.Resources[i]
		if got.APIVersion != want.APIVersion ||
			got.Kind != want.Kind ||
			got.Namespace != want.Namespace ||
			got.Name != want.Name {
			t.Errorf("resource[%d] identity mismatch: got %+v want %+v", i, got, want)
		}
	}
	if len(w2.ArgoApps) != 1 || w2.ArgoApps[0].Name != "app-a" {
		t.Errorf("argo apps not round-tripped: %+v", w2.ArgoApps)
	}
	if len(w2.ArgoAppSets) != 1 || w2.ArgoAppSets[0].Name != "appset-a" {
		t.Errorf("argo appsets not round-tripped: %+v", w2.ArgoAppSets)
	}
	if w2.ArgoAppSets[0].Template.Namespace != "argocd" {
		t.Errorf("appset template namespace not round-tripped: %q",
			w2.ArgoAppSets[0].Template.Namespace)
	}
	if len(w2.ArgoProjects) != 1 || w2.ArgoProjects[0].Name != "proj-a" {
		t.Errorf("argo projects not round-tripped: %+v", w2.ArgoProjects)
	}
}

func TestComputeDigestStable_NoResources(t *testing.T) {
	build := func() *types.World {
		w := types.NewWorld()
		w.CRDs = []types.CRDInfo{{
			Group: "example.com",
			Kind:  "Foo",
			Versions: []types.CRDVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
			}},
		}}
		w.Providers = []types.ProviderInfo{{Name: "provider-aws", Package: "xpkg:v1"}}
		return w
	}

	w1 := build()
	s1 := FromWorld(w1, "test")
	d1 := s1.Digest
	if d1 == "" {
		t.Fatal("expected non-empty digest from FromWorld")
	}

	w2 := build()
	s2 := FromWorldWithOptions(w2, "test", FromWorldOptions{IncludeResources: true})
	d2 := s2.Digest

	if d1 != d2 {
		t.Errorf("digest changed when IncludeResources=true on a World with no live state:\n  legacy:  %s\n  with-opt: %s", d1, d2)
	}

	if s2.Resources != nil {
		t.Errorf("expected Resources to remain nil when copied from empty World, got %v", s2.Resources)
	}
	if s2.ArgoApps != nil {
		t.Errorf("expected ArgoApps to remain nil, got %v", s2.ArgoApps)
	}
	if s2.ArgoAppSets != nil {
		t.Errorf("expected ArgoAppSets to remain nil, got %v", s2.ArgoAppSets)
	}
	if s2.ArgoProjects != nil {
		t.Errorf("expected ArgoProjects to remain nil, got %v", s2.ArgoProjects)
	}
}

func TestLoadOldFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.xpcsnap")

	// Hand-crafted JSON simulating a snapshot written before this change:
	// no resources / argo_apps / argo_app_sets / argo_projects keys at all.
	body := `{
  "version": 1,
  "digest": "sha256:deadbeef",
  "timestamp": "2026-01-01T00:00:00Z",
  "clusterName": "legacy",
  "crds": [
    {
      "group": "example.com",
      "kind": "Foo",
      "scope": "Namespaced",
      "versions": [{"name": "v1", "served": true, "storage": true}],
      "conversion": {"strategy": "None", "costClass": "None"},
      "source": {"file": "", "line": 0, "column": 0},
      "isXRD": false
    }
  ],
  "xrds": [],
  "providers": [],
  "functions": [],
  "configurations": [],
  "compositions": []
}`

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write old-format snapshot: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load on legacy snapshot failed: %v", err)
	}

	if loaded.ClusterName != "legacy" {
		t.Errorf("clusterName: got %q want %q", loaded.ClusterName, "legacy")
	}
	if len(loaded.CRDs) != 1 {
		t.Errorf("CRDs: got %d want 1", len(loaded.CRDs))
	}
	if loaded.Resources != nil {
		t.Errorf("expected Resources nil on legacy snapshot, got %v", loaded.Resources)
	}
	if loaded.ArgoApps != nil {
		t.Errorf("expected ArgoApps nil on legacy snapshot, got %v", loaded.ArgoApps)
	}
	if loaded.ArgoAppSets != nil {
		t.Errorf("expected ArgoAppSets nil on legacy snapshot, got %v", loaded.ArgoAppSets)
	}
	if loaded.ArgoProjects != nil {
		t.Errorf("expected ArgoProjects nil on legacy snapshot, got %v", loaded.ArgoProjects)
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
