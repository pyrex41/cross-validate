package plan

import (
	"path/filepath"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/snapshot"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestResolveVariant_DispatchesOnExtension exercises the three on-disk
// resolution branches: an existing directory, an existing `.xpcsnap` file,
// and a path that exists but is neither (treated as the negative case via a
// non-repo temp dir to keep the git-ref fallback from succeeding).
func TestResolveVariant_DispatchesOnExtension(t *testing.T) {
	tmp := t.TempDir()

	// Build a tiny World and write a valid .xpcsnap into the temp dir.
	w := types.NewWorld()
	w.Resources = []types.ResourceInfo{{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Namespace:  "ns",
		Name:       "n",
	}}
	snap := snapshot.FromWorldWithOptions(w, "test", snapshot.FromWorldOptions{IncludeResources: true})
	snapPath := filepath.Join(tmp, "snap.xpcsnap")
	if err := snap.Save(snapPath); err != nil {
		t.Fatalf("Save snapshot: %v", err)
	}

	tests := []struct {
		name     string
		ref      string
		path     string
		wantKind string
		wantErr  bool
	}{
		{
			name:     "directory",
			ref:      tmp,
			path:     tmp,
			wantKind: "directory",
		},
		{
			name:     "xpcsnap_file",
			ref:      snapPath,
			path:     tmp,
			wantKind: "snapshot",
		},
		{
			name:    "missing_path_no_git_repo",
			ref:     filepath.Join(tmp, "does-not-exist"),
			path:    tmp, // tmp is not under a git repo, so worktree fallback fails
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, cleanup, err := resolveVariant(tc.ref, tc.path, "base")
			if cleanup != nil {
				t.Cleanup(cleanup)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got VariantSource=%+v", src)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveVariant: %v", err)
			}
			if src.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", src.Kind, tc.wantKind)
			}
			if src.Path == "" {
				t.Errorf("Path should be non-empty for kind=%s", src.Kind)
			}
			if cleanup != nil {
				t.Errorf("expected nil cleanup for on-disk source, got non-nil")
			}
		})
	}
}

// TestRunVariantFromSnapshot_HappyPath builds a World with one Resource,
// captures it with IncludeResources=true, and confirms runVariantFromSnapshot
// reconstructs the World and emits no XPC.P.snapshot-incomplete diagnostic.
func TestRunVariantFromSnapshot_HappyPath(t *testing.T) {
	w := types.NewWorld()
	w.Resources = []types.ResourceInfo{{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Namespace:  "ns",
		Name:       "n",
	}}
	snap := snapshot.FromWorldWithOptions(w, "test", snapshot.FromWorldOptions{IncludeResources: true})
	snapPath := filepath.Join(t.TempDir(), "snap.xpcsnap")
	if err := snap.Save(snapPath); err != nil {
		t.Fatalf("Save snapshot: %v", err)
	}

	res, err := runVariantFromSnapshot(Config{}, "snap-ref", snapPath)
	if err != nil {
		t.Fatalf("runVariantFromSnapshot: %v", err)
	}
	if res.Ref != "snap-ref" {
		t.Errorf("Ref = %q, want snap-ref", res.Ref)
	}
	if res.ResolvedDir != snapPath {
		t.Errorf("ResolvedDir = %q, want %q", res.ResolvedDir, snapPath)
	}
	if res.World == nil {
		t.Fatalf("World is nil")
	}
	if got := len(res.World.Resources); got != 1 {
		t.Fatalf("expected 1 resource in World, got %d", got)
	}
	if res.World.Resources[0].Name != "n" {
		t.Errorf("resource Name = %q, want n", res.World.Resources[0].Name)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "XPC.P.snapshot-incomplete" {
			t.Errorf("did not expect XPC.P.snapshot-incomplete on resource-bearing snapshot, got %+v", d)
		}
	}
}

// TestRunVariantFromSnapshot_EmitsIncomplete confirms a snapshot captured
// via the 2-arg FromWorld shim (no live state) yields exactly one info-level
// XPC.P.snapshot-incomplete diagnostic.
func TestRunVariantFromSnapshot_EmitsIncomplete(t *testing.T) {
	w := types.NewWorld()
	w.Resources = []types.ResourceInfo{{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Namespace:  "ns",
		Name:       "ignored",
	}}
	// FromWorld is FromWorldWithOptions with IncludeResources=false, so the
	// saved snap will carry empty Resources/ArgoApps/AppSets/Projects.
	snap := snapshot.FromWorld(w, "test")
	snapPath := filepath.Join(t.TempDir(), "snap.xpcsnap")
	if err := snap.Save(snapPath); err != nil {
		t.Fatalf("Save snapshot: %v", err)
	}

	res, err := runVariantFromSnapshot(Config{}, "snap-ref", snapPath)
	if err != nil {
		t.Fatalf("runVariantFromSnapshot: %v", err)
	}

	var hits []types.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == "XPC.P.snapshot-incomplete" {
			hits = append(hits, d)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly one XPC.P.snapshot-incomplete, got %d (all diags: %+v)", len(hits), res.Diagnostics)
	}
	if hits[0].Severity != types.SeverityInfo {
		t.Errorf("severity = %q, want info", hits[0].Severity)
	}
}
