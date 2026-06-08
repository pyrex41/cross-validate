package checker

import (
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestR35_MustAdoptExternalName exercises category I Tier-1: a provider-signoz
// Alert (a kind whose provider Create path is broken) with no
// crossplane.io/external-name annotation must fire at error; the adopted form
// (external-name present) and the explicitly-waived form do not.
func TestR35_MustAdoptExternalName(t *testing.T) {
	const code = "XPC.I.must-adopt-external-name"

	world := loadFixture(t, "../../testdata/fixtures/external-name-adopt/positive")
	diags := checkFixture(t, world, Config{})
	got := findDiagByCode(diags, code)
	if len(got) != 1 {
		t.Fatalf("positive: expected 1 %s, got %d: %+v", code, len(got), got)
	}
	if got[0].Severity != types.SeverityError {
		t.Errorf("must-adopt-external-name finding should be error, got %s", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "alert-elasticache-memory-warning") {
		t.Errorf("expected resource name in message, got %q", got[0].Message)
	}
	if !strings.Contains(got[0].Message, "crossplane.io/external-name") {
		t.Errorf("expected the external-name remedy in message, got %q", got[0].Message)
	}

	for _, tc := range []struct{ name, fixture string }{
		// MR abd5aa10ed fix: external-name present → adopt, not create.
		{"adopted", "../../testdata/fixtures/external-name-adopt/adopted"},
		// Explicit opt-out: xpc.io/allow-missing-external-name.
		{"waived", "../../testdata/fixtures/external-name-adopt/waived"},
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

// TestR36_OrphanedSGRef exercises category S Tier-2: a SecurityGroupRule attached
// to a foreign/shared SG but referencing a per-env SG built in the same
// composition (the commit d144aa739b preview SG-orphan shape) must fire at warn;
// the symmetric both-ends-local rule, and the annotated/waived form, do not.
func TestR36_OrphanedSGRef(t *testing.T) {
	const code = "XPC.S.orphaned-sgref"

	world := loadFixture(t, "../../testdata/fixtures/orphan-sgref/positive")
	diags := checkFixture(t, world, Config{})
	got := findDiagByCode(diags, code)
	if len(got) != 1 {
		t.Fatalf("positive: expected 1 %s, got %d: %+v", code, len(got), got)
	}
	if got[0].Severity != types.SeverityWarning {
		t.Errorf("orphaned-sgref finding should be warn, got %s", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "xfargateapp-preview") {
		t.Errorf("expected composition name in message, got %q", got[0].Message)
	}
	if !strings.Contains(got[0].Detail, "sourceSecurityGroupIdSelector") {
		t.Errorf("expected the ref field in detail, got %q", got[0].Detail)
	}

	for _, tc := range []struct{ name, fixture string }{
		// Both ends local (intra-env self rule) → shared lifecycle, nothing dangles.
		{"clean", "../../testdata/fixtures/orphan-sgref/clean"},
		// Explicit opt-out: xpc.io/allow-orphan-sgref on the rule.
		{"waived", "../../testdata/fixtures/orphan-sgref/waived"},
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
