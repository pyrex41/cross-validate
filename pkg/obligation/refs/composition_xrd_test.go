package refs

import (
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestCompXRDRef_NoCompositions(t *testing.T) {
	ctx := &obligation.Context{World: types.NewWorld()}
	gen := CompXRDRef{}
	obs := gen.Generate(ctx)
	if len(obs) != 0 {
		t.Errorf("expected 0 obligations for empty world, got %d", len(obs))
	}
}

func TestCompXRDRef_ValidRef(t *testing.T) {
	w := types.NewWorld()
	w.XRDs = []types.CRDInfo{{
		Group: "example.org",
		Kind:  "XDatabase",
		Versions: []types.CRDVersion{{
			Name:          "v1alpha1",
			Served:        true,
			Referenceable: true,
		}},
		Source: types.SourceLocation{File: "xrd.yaml", Line: 1},
	}}
	w.Compositions = []types.CompositionInfo{{
		Name: "xdatabase-default",
		CompositeTypeRef: types.GVK{
			Group:   "example.org",
			Version: "v1alpha1",
			Kind:    "XDatabase",
		},
		Mode:   "Pipeline",
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	}}

	ctx := &obligation.Context{World: w}
	gen := CompXRDRef{}
	obs := gen.Generate(ctx)

	if len(obs) != 1 {
		t.Fatalf("expected 1 obligation, got %d", len(obs))
	}

	result := obs[0].Discharge(ctx)
	if result.Status != obligation.Satisfied {
		t.Errorf("expected Satisfied, got %s", result.Status)
	}
	if result.Diag != nil {
		t.Errorf("expected no diagnostic, got: %s", result.Diag.Message)
	}
}

func TestCompXRDRef_MissingXRD(t *testing.T) {
	w := types.NewWorld()
	w.Compositions = []types.CompositionInfo{{
		Name: "xdatabase-default",
		CompositeTypeRef: types.GVK{
			Group:   "example.org",
			Version: "v1alpha1",
			Kind:    "XDatabase",
		},
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	}}

	ctx := &obligation.Context{World: w}
	gen := CompXRDRef{}
	obs := gen.Generate(ctx)

	if len(obs) != 1 {
		t.Fatalf("expected 1 obligation, got %d", len(obs))
	}

	result := obs[0].Discharge(ctx)
	if result.Status != obligation.Violated {
		t.Errorf("expected Violated, got %s", result.Status)
	}
	if result.Diag == nil {
		t.Fatal("expected diagnostic")
	}
	if result.Diag.Code != "XPC003" {
		t.Errorf("expected XPC003, got %s", result.Diag.Code)
	}
}

func TestCompXRDRef_UnreferenceableVersion(t *testing.T) {
	w := types.NewWorld()
	w.XRDs = []types.CRDInfo{{
		Group: "example.org",
		Kind:  "XDatabase",
		Versions: []types.CRDVersion{{
			Name:          "v1alpha1",
			Served:        true,
			Referenceable: false, // not referenceable
		}},
		Source: types.SourceLocation{File: "xrd.yaml", Line: 1},
	}}
	w.Compositions = []types.CompositionInfo{{
		Name: "xdatabase-default",
		CompositeTypeRef: types.GVK{
			Group:   "example.org",
			Version: "v1alpha1",
			Kind:    "XDatabase",
		},
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	}}

	ctx := &obligation.Context{World: w}
	gen := CompXRDRef{}
	obs := gen.Generate(ctx)

	if len(obs) != 1 {
		t.Fatalf("expected 1 obligation, got %d", len(obs))
	}

	result := obs[0].Discharge(ctx)
	if result.Status != obligation.Violated {
		t.Errorf("expected Violated, got %s", result.Status)
	}
	if result.Diag == nil {
		t.Fatal("expected diagnostic")
	}
	if result.Diag.Code != "XPC003" {
		t.Errorf("expected XPC003, got %s", result.Diag.Code)
	}
}

func TestCompXRDRef_ObligationID(t *testing.T) {
	w := types.NewWorld()
	w.XRDs = []types.CRDInfo{{
		Group: "example.org",
		Kind:  "XDatabase",
		Versions: []types.CRDVersion{{
			Name:          "v1alpha1",
			Served:        true,
			Referenceable: true,
		}},
	}}
	w.Compositions = []types.CompositionInfo{{
		Name: "xdatabase-default",
		CompositeTypeRef: types.GVK{
			Group:   "example.org",
			Version: "v1alpha1",
			Kind:    "XDatabase",
		},
	}}

	ctx := &obligation.Context{World: w}
	gen := CompXRDRef{}
	obs := gen.Generate(ctx)

	if len(obs) != 1 {
		t.Fatalf("expected 1 obligation, got %d", len(obs))
	}

	expected := "XPC.B.comp-xrd-ref.xdatabase-default"
	if obs[0].ID != expected {
		t.Errorf("expected obligation ID %q, got %q", expected, obs[0].ID)
	}
	if obs[0].LegacyCode != "XPC003" {
		t.Errorf("expected legacy code XPC003, got %q", obs[0].LegacyCode)
	}
	if obs[0].Category != obligation.CatReference {
		t.Errorf("expected category B, got %s", obs[0].Category)
	}
}

func TestCompXRDRef_RunIntegration(t *testing.T) {
	// Test through the full obligation.Run path
	w := types.NewWorld()
	w.Compositions = []types.CompositionInfo{{
		Name: "missing-ref",
		CompositeTypeRef: types.GVK{
			Group:   "example.org",
			Version: "v1alpha1",
			Kind:    "XMissing",
		},
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	}}

	reg := obligation.NewRegistry()
	reg.Register(CompXRDRef{})

	ctx := &obligation.Context{World: w}
	result := obligation.Run(reg, ctx)

	if result.TotalObligations != 1 {
		t.Errorf("expected 1 total obligation, got %d", result.TotalObligations)
	}
	if result.Violated != 1 {
		t.Errorf("expected 1 violated, got %d", result.Violated)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(result.Diagnostics))
	}
	diag := result.Diagnostics[0]
	if diag.Code != "XPC003" {
		t.Errorf("expected XPC003, got %s", diag.Code)
	}
	if diag.Obligation == nil {
		t.Fatal("expected Obligation ref on diagnostic")
	}
	if diag.Obligation.Generator != "comp-xrd-ref" {
		t.Errorf("expected generator comp-xrd-ref, got %s", diag.Obligation.Generator)
	}
	if diag.Obligation.Category != "B" {
		t.Errorf("expected category B, got %s", diag.Obligation.Category)
	}
}
