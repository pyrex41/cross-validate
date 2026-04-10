package checker

import (
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

func loadFixture(t *testing.T, path string) *types.World {
	t.Helper()
	docs, err := loader.LoadDirectory(path)
	if err != nil {
		t.Fatalf("loading %s: %v", path, err)
	}
	builder := ir.NewBuilder()
	world, err := builder.Build(docs)
	if err != nil {
		t.Fatalf("building IR for %s: %v", path, err)
	}
	return world
}

func findDiagByCode(diags []types.Diagnostic, code string) []types.Diagnostic {
	var result []types.Diagnostic
	for _, d := range diags {
		if d.Code == code {
			result = append(result, d)
		}
	}
	return result
}

func TestR1_VersionCoherence(t *testing.T) {
	// Test: CRD with proper versions should not error
	world := loadFixture(t, "../../testdata/fixtures/webhook-conversion")
	diags := checkR1(world)
	if len(diags) > 0 {
		t.Errorf("expected no R1 errors for webhook-conversion fixture, got %d: %v",
			len(diags), diags)
	}

	// Test: XRD with proper versions should not error
	world = loadFixture(t, "../../testdata/fixtures/basic")
	diags = checkR1(world)
	if len(diags) > 0 {
		t.Errorf("expected no R1 errors for basic fixture, got %d: %v",
			len(diags), diags)
	}
}

func TestR2_WebhookConversion(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/webhook-conversion")
	diags := checkR2(world, false)

	xpc002 := findDiagByCode(diags, "XPC002")
	if len(xpc002) != 1 {
		t.Fatalf("expected exactly 1 XPC002 error, got %d", len(xpc002))
	}

	d := xpc002[0]
	if d.Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", d.Severity)
	}
	if d.Source.File != "../../testdata/fixtures/webhook-conversion/bucket.yaml" {
		t.Errorf("expected source file bucket.yaml, got %s", d.Source.File)
	}
	if d.Message != "webhook conversion not acknowledged" {
		t.Errorf("unexpected message: %s", d.Message)
	}
}

func TestR2_WebhookConversion_StrictMode(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/webhook-conversion")
	diags := checkR2(world, true)

	xpc002 := findDiagByCode(diags, "XPC002")
	if len(xpc002) != 1 {
		t.Fatalf("expected exactly 1 XPC002 error in strict mode, got %d", len(xpc002))
	}
}

func TestR3_CompositionResolves(t *testing.T) {
	// Basic fixture has matching XRD and Composition — should pass
	world := loadFixture(t, "../../testdata/fixtures/basic")
	diags := checkR3(world)
	if len(diags) > 0 {
		t.Errorf("expected no R3 errors for basic fixture, got %d: %v",
			len(diags), diags)
	}
}

func TestR4_PipelineFunctions(t *testing.T) {
	// Basic fixture has matching function — should pass
	world := loadFixture(t, "../../testdata/fixtures/basic")
	diags := checkR4(world)
	if len(diags) > 0 {
		t.Errorf("expected no R4 errors for basic fixture, got %d: %v",
			len(diags), diags)
	}
}

func TestR4_MissingFunction(t *testing.T) {
	// Construct a world with a composition referencing a missing function
	world := types.NewWorld()
	world.Compositions = append(world.Compositions, types.CompositionInfo{
		Name:             "test-comp",
		CompositeTypeRef: types.GVK{Group: "example.com", Version: "v1", Kind: "XTest"},
		Mode:             "Pipeline",
		Pipeline: []types.PipelineStep{{
			Name:        "render",
			FunctionRef: "function-does-not-exist",
		}},
		Source: types.SourceLocation{File: "test.yaml", Line: 1},
	})

	diags := checkR4(world)
	xpc004 := findDiagByCode(diags, "XPC004")
	if len(xpc004) != 1 {
		t.Fatalf("expected 1 XPC004 error for missing function, got %d", len(xpc004))
	}
}

func TestR5_PatchTypeMismatch(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/patch-mismatch")
	diags := checkR5(world)

	xpc005 := findDiagByCode(diags, "XPC005")
	if len(xpc005) != 1 {
		t.Fatalf("expected 1 XPC005 error for patch type mismatch, got %d", len(xpc005))
	}

	d := xpc005[0]
	if d.Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", d.Severity)
	}
}

func TestR6_WaveOrdering(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/wave-ordering")
	diags := checkR6(world)

	xpc006 := findDiagByCode(diags, "XPC006")
	if len(xpc006) == 0 {
		t.Fatal("expected at least 1 XPC006 error for wave ordering violation")
	}
}

func TestR7_LabelTracking(t *testing.T) {
	world := types.NewWorld()
	world.ArgoApps = append(world.ArgoApps, types.ArgoApplication{
		Name:         "test-app",
		TrackingMode: "label",
		Source:       types.SourceLocation{File: "app.yaml", Line: 1},
	})
	world.Compositions = append(world.Compositions, types.CompositionInfo{
		Name:   "test-comp",
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	})

	diags := checkR7(world)
	xpc007 := findDiagByCode(diags, "XPC007")
	if len(xpc007) != 1 {
		t.Fatalf("expected 1 XPC007 warning for label tracking, got %d", len(xpc007))
	}
	if xpc007[0].Severity != types.SeverityWarning {
		t.Errorf("expected warning severity, got %s", xpc007[0].Severity)
	}
}

func TestR7_AnnotationTracking_NoWarning(t *testing.T) {
	world := types.NewWorld()
	world.ArgoApps = append(world.ArgoApps, types.ArgoApplication{
		Name:         "test-app",
		TrackingMode: "annotation",
		Source:       types.SourceLocation{File: "app.yaml", Line: 1},
	})
	world.Compositions = append(world.Compositions, types.CompositionInfo{
		Name:   "test-comp",
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	})

	diags := checkR7(world)
	if len(diags) > 0 {
		t.Errorf("expected no warnings for annotation tracking, got %d", len(diags))
	}
}

func TestEndToEnd_NoIssues(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/basic")
	diags, err := Check(world, Config{})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	for _, d := range diags {
		if d.Severity == types.SeverityError {
			t.Errorf("unexpected error: %s: %s", d.Code, d.Message)
		}
	}
}

func TestEndToEnd_WebhookConversion(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/webhook-conversion")
	diags, err := Check(world, Config{})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}

	hasXPC002 := false
	for _, d := range diags {
		if d.Code == "XPC002" {
			hasXPC002 = true
			break
		}
	}
	if !hasXPC002 {
		t.Error("expected XPC002 error for webhook conversion, not found")
	}
}
