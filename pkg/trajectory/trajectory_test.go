package trajectory

import (
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestSimulate_EmptyWorld(t *testing.T) {
	w := types.NewWorld()
	steps := Simulate(w)
	if len(steps) != 0 {
		t.Errorf("expected 0 steps for empty world, got %d", len(steps))
	}
}

func TestSimulate_WaveOrderingFixture(t *testing.T) {
	docs, err := loader.LoadDirectory("../../testdata/fixtures/wave-ordering")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	w, err := ir.NewBuilder().Build(docs)
	if err != nil {
		t.Fatalf("build IR: %v", err)
	}
	steps := Simulate(w)
	if len(steps) != 1 {
		t.Fatalf("expected 1 step for wave-ordering fixture (single wave 0), got %d", len(steps))
	}
	s := steps[0]
	if s.AppName != "platform" {
		t.Errorf("expected AppName=platform, got %q", s.AppName)
	}
	if s.Wave != 0 {
		t.Errorf("expected Wave=0, got %d", s.Wave)
	}
	if len(s.Delta.Created) == 0 {
		t.Errorf("expected at least one Created key in first wave, got none")
	}
	if len(s.Delta.Deleted) != 0 {
		t.Errorf("expected no Deleted keys in baseline wave-ordering fixture, got %d", len(s.Delta.Deleted))
	}
}

func TestSimulate_MultipleWavesAccumulateState(t *testing.T) {
	w := types.NewWorld()
	w.ArgoApps = append(w.ArgoApps, types.ArgoApplication{
		Name: "app",
		// Destination.Namespace empty → all resources in scope.
		Source: types.SourceLocation{File: "app.yaml", Line: 1},
	})
	w.Resources = []types.ResourceInfo{
		{
			APIVersion: "v1", Kind: "ConfigMap", Name: "cfg", Namespace: "ns",
			Annotations: map[string]string{"argocd.argoproj.io/sync-wave": "0"},
			Source:      types.SourceLocation{File: "cfg.yaml", Line: 1},
		},
		{
			APIVersion: "v1", Kind: "Pod", Name: "web", Namespace: "ns",
			Annotations: map[string]string{"argocd.argoproj.io/sync-wave": "1"},
			Source:      types.SourceLocation{File: "web.yaml", Line: 1},
		},
	}
	steps := Simulate(w)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps (waves 0 and 1), got %d", len(steps))
	}
	if steps[0].Wave != 0 || steps[1].Wave != 1 {
		t.Errorf("expected waves [0,1], got [%d,%d]", steps[0].Wave, steps[1].Wave)
	}
	if len(steps[0].Delta.Created) != 1 || steps[0].Delta.Created[0].Name != "cfg" {
		t.Errorf("expected wave-0 Created=[cfg], got %+v", steps[0].Delta.Created)
	}
	if len(steps[1].Delta.Created) != 1 || steps[1].Delta.Created[0].Name != "web" {
		t.Errorf("expected wave-1 Created=[web], got %+v", steps[1].Delta.Created)
	}
	// Wave 1's cumulative State should include both cfg and web.
	if len(steps[1].State.Resources) != 2 {
		t.Errorf("expected wave-1 cumulative state to include 2 resources, got %d", len(steps[1].State.Resources))
	}
}

func TestSimulate_HookDeletePolicyProducesDeleted(t *testing.T) {
	w := types.NewWorld()
	w.ArgoApps = append(w.ArgoApps, types.ArgoApplication{
		Name:   "app",
		Source: types.SourceLocation{File: "app.yaml", Line: 1},
	})
	w.Resources = []types.ResourceInfo{
		{
			APIVersion: "v1", Kind: "ConfigMap", Name: "tmp", Namespace: "ns",
			Annotations: map[string]string{
				"argocd.argoproj.io/sync-wave":          "0",
				"argocd.argoproj.io/hook":               "Sync",
				"argocd.argoproj.io/hook-delete-policy": "HookSucceeded",
			},
			Source: types.SourceLocation{File: "tmp.yaml", Line: 1},
		},
	}
	steps := Simulate(w)
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if len(steps[0].Delta.Created) != 1 {
		t.Errorf("expected Created=[tmp], got %+v", steps[0].Delta.Created)
	}
	if len(steps[0].Delta.Deleted) != 1 || steps[0].Delta.Deleted[0].Name != "tmp" {
		t.Errorf("expected Deleted=[tmp] from hook-delete-policy, got %+v", steps[0].Delta.Deleted)
	}
	if _, stillInState := steps[0].State.Resources[ResourceKey{
		APIVersion: "v1", Kind: "ConfigMap", Namespace: "ns", Name: "tmp",
	}]; stillInState {
		t.Errorf("expected tmp to be removed from State after hook-deletion")
	}
}

func TestSimulate_ScopeToDestinationNamespace(t *testing.T) {
	w := types.NewWorld()
	w.ArgoApps = append(w.ArgoApps, types.ArgoApplication{
		Name:        "scoped",
		Destination: types.ArgoDestination{Namespace: "allow"},
		Source:      types.SourceLocation{File: "app.yaml", Line: 1},
	})
	w.Resources = []types.ResourceInfo{
		{APIVersion: "v1", Kind: "ConfigMap", Name: "in", Namespace: "allow"},
		{APIVersion: "v1", Kind: "ConfigMap", Name: "out", Namespace: "other"},
		{APIVersion: "v1", Kind: "ConfigMap", Name: "cluster"}, // no namespace → passes
	}
	steps := Simulate(w)
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	for _, k := range steps[0].Delta.Created {
		if k.Name == "out" {
			t.Errorf("resource 'out' in namespace 'other' should not be in scope")
		}
	}
	if len(steps[0].Delta.Created) != 2 {
		t.Errorf("expected scoped + cluster-scoped resource (2), got %d: %+v",
			len(steps[0].Delta.Created), steps[0].Delta.Created)
	}
}
