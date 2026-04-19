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

func checkFixture(t *testing.T, world *types.World, cfg Config) []types.Diagnostic {
	t.Helper()
	diags, err := Check(world, cfg)
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	return diags
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
	world := loadFixture(t, "../../testdata/fixtures/webhook-conversion")
	diags := checkFixture(t, world, Config{})
	if xpc001 := findDiagByCode(diags, "XPC001"); len(xpc001) > 0 {
		t.Errorf("expected no XPC001 errors for webhook-conversion fixture, got %d: %v",
			len(xpc001), xpc001)
	}

	world = loadFixture(t, "../../testdata/fixtures/basic")
	diags = checkFixture(t, world, Config{})
	if xpc001 := findDiagByCode(diags, "XPC001"); len(xpc001) > 0 {
		t.Errorf("expected no XPC001 errors for basic fixture, got %d: %v",
			len(xpc001), xpc001)
	}
}

func TestR2_WebhookConversion(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/webhook-conversion")
	diags := checkFixture(t, world, Config{StrictConversions: false})

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
	if d.Obligation == nil || d.Obligation.Generator != "conversion-cost-opt-in" {
		t.Errorf("expected Obligation.Generator=conversion-cost-opt-in, got %+v", d.Obligation)
	}
}

func TestR2_WebhookConversion_StrictMode(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/webhook-conversion")
	diags := checkFixture(t, world, Config{StrictConversions: true})

	xpc002 := findDiagByCode(diags, "XPC002")
	if len(xpc002) != 1 {
		t.Fatalf("expected exactly 1 XPC002 error in strict mode, got %d", len(xpc002))
	}
}

func TestR3_CompositionResolves(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/basic")
	diags := checkFixture(t, world, Config{})
	if xpc003 := findDiagByCode(diags, "XPC003"); len(xpc003) > 0 {
		t.Errorf("expected no XPC003 errors for basic fixture, got %d: %v",
			len(xpc003), xpc003)
	}
}

func TestR4_PipelineFunctions(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/basic")
	diags := checkFixture(t, world, Config{})
	if xpc004 := findDiagByCode(diags, "XPC004"); len(xpc004) > 0 {
		t.Errorf("expected no XPC004 errors for basic fixture, got %d: %v",
			len(xpc004), xpc004)
	}
}

func TestR4_MissingFunction(t *testing.T) {
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

	diags := checkFixture(t, world, Config{})
	xpc004 := findDiagByCode(diags, "XPC004")
	if len(xpc004) != 1 {
		t.Fatalf("expected 1 XPC004 error for missing function, got %d", len(xpc004))
	}
}

func TestR5_PatchTypeMismatch(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/patch-mismatch")
	diags := checkFixture(t, world, Config{})

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
	diags := checkFixture(t, world, Config{})

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

	diags := checkFixture(t, world, Config{})
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

	diags := checkFixture(t, world, Config{})
	if xpc007 := findDiagByCode(diags, "XPC007"); len(xpc007) > 0 {
		t.Errorf("expected no XPC007 warnings for annotation tracking, got %d", len(xpc007))
	}
}

func TestEndToEnd_NoIssues(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/basic")
	diags := checkFixture(t, world, Config{})
	for _, d := range diags {
		if d.Severity == types.SeverityError {
			t.Errorf("unexpected error: %s: %s", d.Code, d.Message)
		}
	}
}

func TestR6c_ProviderWave(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/provider-wave")
	diags := checkFixture(t, world, Config{})

	xpc006 := findDiagByCode(diags, "XPC006")
	if len(xpc006) == 0 {
		t.Fatal("expected at least 1 XPC006 error for provider-wave violation")
	}

	found := false
	for _, d := range xpc006 {
		if d.Severity == types.SeverityError {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error severity on XPC006 from R6c, none found")
	}
}

func TestR12_DanglingMount(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/dangling-mount")
	diags := checkFixture(t, world, Config{})

	xpc012 := findDiagByCode(diags, "XPC012")
	if len(xpc012) != 1 {
		t.Fatalf("expected exactly 1 XPC012 error for dangling mount, got %d: %+v",
			len(xpc012), xpc012)
	}
	if xpc012[0].Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", xpc012[0].Severity)
	}
}

func TestR13_RuleLoaded(t *testing.T) {
	// R13 is framework-only in this phase — Delta.Updated is always empty so
	// it never fires on real input. This test just verifies the rule loads
	// and runs without panicking against a non-trivial fixture.
	world := loadFixture(t, "../../testdata/fixtures/basic")
	diags := checkFixture(t, world, Config{})
	if xpc013 := findDiagByCode(diags, "XPC013"); len(xpc013) > 0 {
		t.Errorf("expected no XPC013 errors in phase 1 (Delta.Updated is empty), got %d", len(xpc013))
	}
}

func TestR14_RbacRegression(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/rbac-regression")
	diags := checkFixture(t, world, Config{})

	xpc014 := findDiagByCode(diags, "XPC014")
	if len(xpc014) == 0 {
		t.Fatalf("expected at least 1 XPC014 error for RBAC regression, got %+v", diags)
	}
	if xpc014[0].Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", xpc014[0].Severity)
	}
}

func TestR15_AppProjectWhitelist(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/appproject-whitelist-miss")
	diags := checkFixture(t, world, Config{})

	got := findDiagByCode(diags, "XPC.D.kind-whitelisted")
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 XPC.D.kind-whitelisted diagnostic, got %d: %+v", len(got), got)
	}
	if got[0].Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", got[0].Severity)
	}
}

func TestEndToEnd_WebhookConversion(t *testing.T) {
	world := loadFixture(t, "../../testdata/fixtures/webhook-conversion")
	diags := checkFixture(t, world, Config{})

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
