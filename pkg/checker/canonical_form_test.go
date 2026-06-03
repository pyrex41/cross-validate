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

	t.Run("fixed", func(t *testing.T) {
		world := loadFixture(t, "../../testdata/fixtures/canonical-form-template/fixed")
		diags := checkFixture(t, world, Config{})
		if got := findDiagByCode(diags, code); len(got) != 0 {
			t.Fatalf("fixed: expected 0 %s, got %d: %+v", code, len(got), got)
		}
	})
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
