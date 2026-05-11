package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/checker"
	"github.com/pyrex41/cross-validate-/pkg/snapshot"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestMergeSnapshotIntoWorld_NewSlices covers the four slices added when
// snapshots became state-bearing (Resources, ArgoApps, ArgoAppSets,
// ArgoProjects). Manifest-side entries must win on identity-tuple conflicts;
// snapshot-only entries must be appended; manifest-only entries must remain
// untouched. Mirrors the existing CRD/XRD merge convention.
func TestMergeSnapshotIntoWorld_NewSlices(t *testing.T) {
	t.Run("resources: manifest wins on identity conflict, snapshot-only appended, manifest-only preserved", func(t *testing.T) {
		w := types.NewWorld()
		// X: present in both. Manifest source marker should survive.
		w.Resources = append(w.Resources, types.ResourceInfo{
			APIVersion: "example.org/v1",
			Kind:       "Widget",
			Namespace:  "ns1",
			Name:       "foo",
			Provenance: "manifest",
		})
		// Z: only in manifest.
		w.Resources = append(w.Resources, types.ResourceInfo{
			APIVersion: "example.org/v1",
			Kind:       "Widget",
			Namespace:  "ns1",
			Name:       "manifest-only",
			Provenance: "manifest",
		})

		snap := snapshot.New("test")
		// X collision: snapshot version with a different distinguishing field.
		snap.Resources = append(snap.Resources, types.ResourceInfo{
			APIVersion: "example.org/v1",
			Kind:       "Widget",
			Namespace:  "ns1",
			Name:       "foo",
			Provenance: "snapshot",
		})
		// Y: only in snapshot.
		snap.Resources = append(snap.Resources, types.ResourceInfo{
			APIVersion: "example.org/v1",
			Kind:       "Widget",
			Namespace:  "ns1",
			Name:       "snapshot-only",
			Provenance: "snapshot",
		})

		mergeSnapshotIntoWorld(w, snap)

		// Should have exactly 3 Resources: foo (manifest copy), manifest-only,
		// snapshot-only.
		if got, want := len(w.Resources), 3; got != want {
			t.Fatalf("expected %d Resources after merge, got %d: %+v", want, got, w.Resources)
		}

		var foo *types.ResourceInfo
		var sawManifestOnly, sawSnapshotOnly bool
		for i := range w.Resources {
			r := &w.Resources[i]
			switch r.Name {
			case "foo":
				foo = r
			case "manifest-only":
				sawManifestOnly = true
			case "snapshot-only":
				sawSnapshotOnly = true
			}
		}
		if foo == nil {
			t.Fatal("expected merged Resources to contain foo, but it was missing")
		}
		if foo.Provenance != "manifest" {
			t.Errorf("expected manifest-side foo to win on conflict (Provenance=manifest), got %q", foo.Provenance)
		}
		if !sawManifestOnly {
			t.Error("manifest-only resource was lost during merge")
		}
		if !sawSnapshotOnly {
			t.Error("snapshot-only resource was not appended during merge")
		}
	})

	t.Run("argoApps: snapshot-only entry is appended", func(t *testing.T) {
		w := types.NewWorld()
		snap := snapshot.New("test")
		snap.ArgoApps = []types.ArgoApplication{{
			Name:      "app-snap",
			Namespace: "argocd",
		}}

		mergeSnapshotIntoWorld(w, snap)

		if len(w.ArgoApps) != 1 || w.ArgoApps[0].Name != "app-snap" {
			t.Fatalf("expected snapshot ArgoApp to be appended, got %+v", w.ArgoApps)
		}
	})

	t.Run("argoAppSets: snapshot-only entry is appended", func(t *testing.T) {
		w := types.NewWorld()
		snap := snapshot.New("test")
		snap.ArgoAppSets = []types.ArgoApplicationSet{{
			Name: "appset-snap",
			Template: types.ArgoAppSetTemplate{
				Namespace: "argocd",
			},
		}}

		mergeSnapshotIntoWorld(w, snap)

		if len(w.ArgoAppSets) != 1 || w.ArgoAppSets[0].Name != "appset-snap" {
			t.Fatalf("expected snapshot ArgoAppSet to be appended, got %+v", w.ArgoAppSets)
		}
		if w.ArgoAppSets[0].Template.Namespace != "argocd" {
			t.Errorf("expected merged AppSet Template.Namespace to round-trip, got %q",
				w.ArgoAppSets[0].Template.Namespace)
		}
	})

	t.Run("argoProjects: snapshot-only entry is appended", func(t *testing.T) {
		w := types.NewWorld()
		snap := snapshot.New("test")
		snap.ArgoProjects = []types.ArgoAppProject{{
			Name: "proj-snap",
		}}

		mergeSnapshotIntoWorld(w, snap)

		if len(w.ArgoProjects) != 1 || w.ArgoProjects[0].Name != "proj-snap" {
			t.Fatalf("expected snapshot ArgoAppProject to be appended, got %+v", w.ArgoProjects)
		}
	})
}

func TestRunCheck_ProfileRules_WritesProfileSeparately(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "profile.json")
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "appproject-whitelist-absent")

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := runCheck([]string{
		"--format=human",
		"--skip-render",
		"--profile-rules",
		"--profile-out=" + profilePath,
		fixture,
	})
	_ = w.Close()
	os.Stdout = oldStdout
	stdoutBytes, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if code != 0 {
		t.Fatalf("runCheck returned %d, stdout:\n%s", code, string(stdoutBytes))
	}
	stdout := string(stdoutBytes)
	if strings.Contains(stdout, "ruleTimings") || strings.Contains(stdout, "stageTimings") {
		t.Fatalf("profile JSON leaked into stdout:\n%s", stdout)
	}

	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	var payload struct {
		StageTimings []checker.Timing     `json:"stageTimings"`
		RuleTimings  []checker.RuleTiming `json:"ruleTimings"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode profile: %v\n%s", err, data)
	}
	if len(payload.StageTimings) == 0 {
		t.Fatal("expected stage timings in profile")
	}
	if len(payload.RuleTimings) == 0 {
		t.Fatal("expected rule timings in profile")
	}
}
