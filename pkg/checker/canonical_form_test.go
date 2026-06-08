package checker

import (
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestR31_ForProviderCanonicalForm exercises category M Tier-1 (static): an ECS
// Service whose forProvider.taskDefinition is a bare family ARN (no :revision)
// is the MR !2232 reconcile-storm shape and must fire; the canonical form and
// the managementPolicies-suppressed form must not.
func TestR31_ForProviderCanonicalForm(t *testing.T) {
	const code = "XPC.M.forprovider-canonical-form"

	world := loadFixture(t, "../../testdata/fixtures/canonical-form/positive")
	diags := checkFixture(t, world, Config{})
	got := findDiagByCode(diags, code)
	if len(got) != 1 {
		t.Fatalf("positive: expected 1 %s, got %d: %+v", code, len(got), got)
	}
	if got[0].Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "fg-preview-chart-service") {
		t.Errorf("expected resource name in message, got %q", got[0].Message)
	}

	for _, tc := range []struct{ name, fixture string }{
		{"canonical-ok", "../../testdata/fixtures/canonical-form/canonical-ok"},
		{"mgmtpolicies-ok", "../../testdata/fixtures/canonical-form/mgmtpolicies-ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := loadFixture(t, tc.fixture)
			diags := checkFixture(t, world, Config{})
			if got := findDiagByCode(diags, code); len(got) != 0 {
				t.Fatalf("%s: expected 0 %s, got %d: %+v", tc.name, code, len(got), got)
			}
		})
	}
}

// TestR31_CompositionTemplate exercises category M Tier-2 (heuristic): the
// MR !2232 shape lives in an unrendered go-templating Composition, invisible to
// the resource-walk tier. The hardcoded bare-family taskDefinition must fire
// (warn); the fix's computed-from-observed form must not.
func TestR31_CompositionTemplate(t *testing.T) {
	const code = "XPC.M.forprovider-canonical-form"

	world := loadFixture(t, "../../testdata/fixtures/canonical-form-template/positive")
	diags := checkFixture(t, world, Config{})
	got := findDiagByCode(diags, code)
	if len(got) != 1 {
		t.Fatalf("positive: expected 1 %s, got %d: %+v", code, len(got), got)
	}
	if got[0].Severity != types.SeverityWarning {
		t.Errorf("template finding should be warn, got %s", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "xfargateservice-preview") {
		t.Errorf("expected composition name in message, got %q", got[0].Message)
	}

	// Unguarded bare-family literal reachable through a conditional on an
	// unrelated var (no $taskDefArn/atProvider/default guard) — Shape A hidden
	// in a {{ if }}. Closing the "any {{ RHS is canonical" blind spot means this
	// now fires.
	t.Run("unguarded-conditional", func(t *testing.T) {
		world := loadFixture(t, "../../testdata/fixtures/canonical-form-template/unguarded-conditional")
		diags := checkFixture(t, world, Config{})
		got := findDiagByCode(diags, code)
		if len(got) != 1 {
			t.Fatalf("unguarded-conditional: expected 1 %s, got %d: %+v", code, len(got), got)
		}
		if got[0].Severity != types.SeverityWarning {
			t.Errorf("template finding should be warn, got %s", got[0].Severity)
		}
	})

	// Negative cases — Shape B/C must NOT fire. Pins Option 1: a guarded
	// one-shot seed converges (transient blip, not a permanent storm), so M
	// stays silent and the validated MR !2232 fix is not regressed.
	for _, tc := range []struct{ name, fixture string }{
		// $taskDefArn | default (printf bare) — the !2232 worker/service fix.
		{"fixed-default-pipe", "../../testdata/fixtures/canonical-form-template/fixed"},
		// {{ if $taskDefArn }}…{{ else }}bare{{ end }} — the app-prod shape.
		{"guarded-ifelse", "../../testdata/fixtures/canonical-form-template/guarded-ifelse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := loadFixture(t, tc.fixture)
			diags := checkFixture(t, world, Config{})
			if got := findDiagByCode(diags, code); len(got) != 0 {
				t.Fatalf("%s: expected 0 %s, got %d: %+v", tc.name, code, len(got), got)
			}
		})
	}
}

// TestR32_ObservedDesiredFixedPoint exercises category M Tier-3 (dynamic): a
// status-bearing Service whose forProvider/atProvider diverge. A registered
// field (taskDefinition) is conclusive (error); an unregistered field
// (cluster) is the warn-level long tail.
func TestR32_ObservedDesiredFixedPoint(t *testing.T) {
	const code = "XPC.M.observed-desired-fixed-point"

	t.Run("registered-error", func(t *testing.T) {
		world := loadFixture(t, "../../testdata/fixtures/fixed-point/registered")
		diags := checkFixture(t, world, Config{})
		got := findDiagByCode(diags, code)
		if len(got) != 1 {
			t.Fatalf("expected 1 %s, got %d: %+v", code, len(got), got)
		}
		if got[0].Severity != types.SeverityError {
			t.Errorf("registered divergence should be error, got %s", got[0].Severity)
		}
		if !strings.Contains(got[0].Message, "taskDefinition") {
			t.Errorf("expected field path in message, got %q", got[0].Message)
		}
		if !strings.Contains(got[0].Detail, ":42") {
			t.Errorf("expected observed value in detail, got %q", got[0].Detail)
		}
	})

	t.Run("unregistered-warn", func(t *testing.T) {
		world := loadFixture(t, "../../testdata/fixtures/fixed-point/unregistered")
		diags := checkFixture(t, world, Config{})
		got := findDiagByCode(diags, code)
		if len(got) != 1 {
			t.Fatalf("expected 1 %s, got %d: %+v", code, len(got), got)
		}
		if got[0].Severity != types.SeverityWarning {
			t.Errorf("unregistered divergence should be warn, got %s", got[0].Severity)
		}
	})
}

// TestR33_DuplicateEnvKey exercises category M Tier-2: a composition that emits
// the same ECS env var twice (global + conditional override) fires; the
// single-entry form, with a like-named container and a secret, does not.
func TestR33_DuplicateEnvKey(t *testing.T) {
	const code = "XPC.M.duplicate-env-key"

	world := loadFixture(t, "../../testdata/fixtures/duplicate-env/positive")
	diags := checkFixture(t, world, Config{})
	got := findDiagByCode(diags, code)
	if len(got) != 1 {
		t.Fatalf("positive: expected 1 %s, got %d: %+v", code, len(got), got)
	}
	if got[0].Severity != types.SeverityWarning {
		t.Errorf("duplicate-env finding should be warn, got %s", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "OTEL_PHP_AUTOLOAD_ENABLED") {
		t.Errorf("expected env name in message, got %q", got[0].Message)
	}

	t.Run("clean", func(t *testing.T) {
		world := loadFixture(t, "../../testdata/fixtures/duplicate-env/clean")
		diags := checkFixture(t, world, Config{})
		if got := findDiagByCode(diags, code); len(got) != 0 {
			t.Fatalf("clean: expected 0 %s, got %d: %+v", code, len(got), got)
		}
	})
}

// TestR34_ComputedBlockAlias exercises category M Tier-2: an elbv2
// LBListenerRule forward action written with the simple targetGroupArnSelector
// alias and no canonical forward{} block (the MR !2336 storm shape) fires; the
// fixed form with a forward{} block, and a redirect rule, do not.
func TestR34_ComputedBlockAlias(t *testing.T) {
	const code = "XPC.M.computed-block-alias"

	world := loadFixture(t, "../../testdata/fixtures/computed-block-alias/positive")
	diags := checkFixture(t, world, Config{})
	got := findDiagByCode(diags, code)
	if len(got) != 1 {
		t.Fatalf("positive: expected 1 %s, got %d: %+v", code, len(got), got)
	}
	if got[0].Severity != types.SeverityWarning {
		t.Errorf("computed-block-alias finding should be warn, got %s", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "xfargateapp-preview") {
		t.Errorf("expected composition name in message, got %q", got[0].Message)
	}
	if !strings.Contains(got[0].Message, "targetGroupArnSelector") {
		t.Errorf("expected alias field in message, got %q", got[0].Message)
	}

	for _, tc := range []struct{ name, fixture string }{
		// MR !2336 fix: canonical forward{} block present → converges.
		{"fixed", "../../testdata/fixtures/computed-block-alias/fixed"},
		// A redirect action has no computed forward block → not this storm.
		{"redirect", "../../testdata/fixtures/computed-block-alias/redirect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := loadFixture(t, tc.fixture)
			diags := checkFixture(t, world, Config{})
			if got := findDiagByCode(diags, code); len(got) != 0 {
				t.Fatalf("%s: expected 0 %s, got %d: %+v", tc.name, code, len(got), got)
			}
		})
	}
}
