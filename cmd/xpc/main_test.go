package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// captureRunCheck runs runCheck with the given args, capturing stdout, and
// returns (exitCode, stdout). Mirrors the os.Pipe pattern used elsewhere here.
func captureRunCheck(t *testing.T, args []string) (int, string) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := runCheck(args)
	_ = w.Close()
	os.Stdout = oldStdout
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return code, string(out)
}

// ecsServiceStormResource is a live ECS Service with a forProvider/atProvider
// divergence on the registered taskDefinition leaf — the R31/R32 (category M)
// reconcile-storm fingerprint. Returns a ResourceInfo whose Raw mirrors a
// cluster read-back.
func ecsServiceStormResource() types.ResourceInfo {
	return types.ResourceInfo{
		APIVersion: "ecs.aws.upbound.io/v1beta1",
		Kind:       "Service",
		Name:       "fg-preview-chart-service",
		Namespace:  "crossplane-system",
		Source:     types.SourceLocation{File: "live://Service/fg-preview-chart-service", Line: 1},
		Raw: map[string]interface{}{
			"apiVersion": "ecs.aws.upbound.io/v1beta1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "fg-preview-chart-service",
				"namespace": "crossplane-system",
			},
			"spec": map[string]interface{}{
				"forProvider": map[string]interface{}{
					"region":         "us-east-2",
					"taskDefinition": "arn:aws:ecs:us-east-2:362836553677:task-definition/fg-preview-chart-service",
					"launchType":     "FARGATE",
				},
			},
			"status": map[string]interface{}{
				"atProvider": map[string]interface{}{
					"taskDefinition": "arn:aws:ecs:us-east-2:362836553677:task-definition/fg-preview-chart-service:42",
				},
			},
		},
	}
}

// rdsClusterDeleteRiskResource is a live state-bearing RDS Cluster with no
// deletionPolicy: Orphan — the R23 (category S) crossplane-state-needs-orphan
// fingerprint. Used as the non-M finding the --category=M filter must exclude.
func rdsClusterDeleteRiskResource() types.ResourceInfo {
	return types.ResourceInfo{
		APIVersion: "rds.aws.upbound.io/v1beta1",
		Kind:       "Cluster",
		Name:       "aurora-prod-cluster",
		Namespace:  "crossplane-system",
		Source:     types.SourceLocation{File: "live://Cluster/aurora-prod-cluster", Line: 1},
		Raw: map[string]interface{}{
			"apiVersion": "rds.aws.upbound.io/v1beta1",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      "aurora-prod-cluster",
				"namespace": "crossplane-system",
			},
			"spec": map[string]interface{}{
				"forProvider": map[string]interface{}{
					"region":       "us-east-1",
					"engine":       "aurora-postgresql",
					"databaseName": "app",
				},
			},
		},
	}
}

// writeSnapshotFile saves a snapshot carrying the given resources to a temp
// .xpcsnap file with a future timestamp (so it is never flagged stale) and
// returns its path.
func writeSnapshotFile(t *testing.T, resources ...types.ResourceInfo) string {
	t.Helper()
	snap := snapshot.New("test-cluster")
	snap.Timestamp = time.Now().Add(24 * time.Hour)
	snap.Resources = append(snap.Resources, resources...)
	path := filepath.Join(t.TempDir(), "live.xpcsnap")
	if err := snap.Save(path); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	return path
}

// TestRunCheck_SnapshotOnly_FiresR32 covers Change 1: a check whose ONLY input
// is a live-cluster snapshot (no path arg) runs the rules over the merged world
// and fires R32 (XPC.M.observed-desired-fixed-point) from the snapshot's
// forProvider/atProvider divergence. This is the in-cluster audit shape.
func TestRunCheck_SnapshotOnly_FiresR32(t *testing.T) {
	snapPath := writeSnapshotFile(t, ecsServiceStormResource())

	// No path argument — the snapshot is the sole source of the world.
	code, stdout := captureRunCheck(t, []string{
		"--snapshot=" + snapPath,
		"--skip-render",
		"--format=agent",
	})

	if !strings.Contains(stdout, "XPC.M.observed-desired-fixed-point") {
		t.Fatalf("expected R32 to fire from snapshot-only run, stdout:\n%s", stdout)
	}
	if code != 1 {
		t.Fatalf("expected exit 1 (R32 is an error), got %d\n%s", code, stdout)
	}
}

// TestRunCheck_SnapshotOnly_NoSnapshotStillErrors pins that the "no YAML
// documents found" gate still fires when there is genuinely nothing to check
// (no docs and no snapshot). Uses an empty temp dir as the path.
func TestRunCheck_SnapshotOnly_NoSnapshotStillErrors(t *testing.T) {
	emptyDir := t.TempDir()
	code, _ := captureRunCheck(t, []string{"--skip-render", emptyDir})
	if code != 1 {
		t.Fatalf("expected exit 1 for empty input with no snapshot, got %d", code)
	}
}

// TestRunCheck_CategoryM_ExcludesNonM covers Change 2: --category=M restricts
// the run to category-M rules, so a non-M finding (R23 / category S) present in
// the same snapshot is excluded while the M findings remain.
func TestRunCheck_CategoryM_ExcludesNonM(t *testing.T) {
	snapPath := writeSnapshotFile(t,
		ecsServiceStormResource(),      // M: R31 + R32
		rdsClusterDeleteRiskResource(), // S: R23
	)

	// Unfiltered: both the M finding and the S finding must be present (setup
	// guard — otherwise the exclusion assertion below is vacuous).
	_, full := captureRunCheck(t, []string{"--snapshot=" + snapPath, "--skip-render", "--format=agent"})
	if !strings.Contains(full, "XPC.M.observed-desired-fixed-point") {
		t.Fatalf("setup: unfiltered run must contain the M finding, stdout:\n%s", full)
	}
	if !strings.Contains(full, "XPC.S.crossplane-state-needs-orphan") {
		t.Fatalf("setup: unfiltered run must contain the non-M (S) finding, stdout:\n%s", full)
	}

	// --category=M: keep M findings, drop the S finding.
	_, filtered := captureRunCheck(t, []string{
		"--snapshot=" + snapPath, "--skip-render", "--format=agent", "--category=M",
	})
	if !strings.Contains(filtered, "XPC.M.observed-desired-fixed-point") {
		t.Errorf("--category=M must keep the M finding, stdout:\n%s", filtered)
	}
	if strings.Contains(filtered, "XPC.S.crossplane-state-needs-orphan") {
		t.Errorf("--category=M must exclude the non-M (S) finding, stdout:\n%s", filtered)
	}
}

// TestRunCheck_Rules_NarrowsToSingleCode covers the --rules flag: a single full
// diagnostic code restricts the run to exactly that rule, even excluding other
// rules in the same category.
func TestRunCheck_Rules_NarrowsToSingleCode(t *testing.T) {
	snapPath := writeSnapshotFile(t, ecsServiceStormResource())
	_, out := captureRunCheck(t, []string{
		"--snapshot=" + snapPath, "--skip-render", "--format=agent",
		"--rules=XPC.M.observed-desired-fixed-point",
	})
	if !strings.Contains(out, "XPC.M.observed-desired-fixed-point") {
		t.Errorf("--rules must keep the requested code, stdout:\n%s", out)
	}
	if strings.Contains(out, "XPC.M.forprovider-canonical-form") {
		t.Errorf("--rules=XPC.M.observed-desired-fixed-point must exclude the sibling M rule R31, stdout:\n%s", out)
	}
}

// TestRunCheck_Category_UnknownLetterErrors pins that a typo'd category fails
// loudly rather than silently checking nothing.
func TestRunCheck_Category_UnknownLetterErrors(t *testing.T) {
	snapPath := writeSnapshotFile(t, ecsServiceStormResource())
	code, _ := captureRunCheck(t, []string{
		"--snapshot=" + snapPath, "--skip-render", "--category=Z",
	})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown --category=Z, got %d", code)
	}
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
