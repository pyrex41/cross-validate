package policy

import (
	"slices"
	"testing"
)

func TestDecidableSubsetIncludesINC6Floor(t *testing.T) {
	subset := DecidableSubset()
	for _, code := range []string{
		"XPC.S.crossplane-state-needs-orphan",     // R23
		"XPC.E.appset-finalizer-without-preserve", // R24
		"XPC.E.prod-appset-autosync",              // R25
		"XPC.E.fargate-claim-env-label",           // R29
		"XPC.M.forprovider-canonical-form",        // R31
		"XPC.M.duplicate-env-key",                 // R33
		"XPC.E.ssa-managementpolicies-observe",    // R22
		"XPC.E.ssa-managementpolicies-partial",    // R22
		"XPC.E.ssa-managementpolicies-nondefault", // R22
	} {
		if !slices.Contains(subset, code) {
			t.Errorf("DecidableSubset missing expected single-object code %q", code)
		}
	}
}

func TestDecidableSubsetExcludesTrajectoryAndRefRules(t *testing.T) {
	subset := DecidableSubset()
	// Trajectory / live-diff rules must never be in the single-object subset.
	for _, code := range []string{
		"XPC012",                             // R12 dangling mount (trajectory)
		"XPC014",                             // R14 rbac regression (trajectory)
		"XPC.M.observed-desired-fixed-point", // R32 live observed/desired diff
	} {
		if slices.Contains(subset, code) {
			t.Errorf("DecidableSubset must exclude trajectory/live-diff rule %q", code)
		}
	}
	// Reference-resolution rules must also be excluded from the single-object
	// subset (they need other objects in the repo).
	for _, code := range []string{
		"XPC003",                        // R3 composition->XRD
		"XPC004",                        // R4 pipeline->Function
		"XPC.B.providerconfig-resolves", // R28 providerConfigRef->ProviderConfig
		"XPC007",                        // R7 owner-refs
	} {
		if slices.Contains(subset, code) {
			t.Errorf("DecidableSubset must exclude reference-resolution rule %q", code)
		}
	}
}

func TestAmbientSubsetSupersetOfDecidable(t *testing.T) {
	decidable := DecidableSubset()
	ambient := AmbientSubset()
	for _, code := range decidable {
		if !slices.Contains(ambient, code) {
			t.Errorf("AmbientSubset missing single-object code %q", code)
		}
	}
	if len(ambient) <= len(decidable) {
		t.Errorf("AmbientSubset (%d) should add ambient-tier rules on top of decidable (%d)",
			len(ambient), len(decidable))
	}
	// Ambient subset must still exclude trajectory/live-diff rules.
	if slices.Contains(ambient, "XPC.M.observed-desired-fixed-point") {
		t.Error("AmbientSubset must still exclude R32 (live diff)")
	}
}

func TestControllerSubsetAddsLiveTier(t *testing.T) {
	ambient := AmbientSubset()
	controller := ControllerSubset()

	// ControllerSubset is a superset of AmbientSubset.
	for _, code := range ambient {
		if !slices.Contains(controller, code) {
			t.Errorf("ControllerSubset missing ambient code %q", code)
		}
	}
	// R32 is the live-tier rule the controller unlocks; it must be present
	// here but absent from the admission-facing subsets.
	if !slices.Contains(controller, "XPC.M.observed-desired-fixed-point") {
		t.Error("ControllerSubset must include R32 (live observed/desired diff)")
	}
	if slices.Contains(ambient, "XPC.M.observed-desired-fixed-point") {
		t.Error("AmbientSubset must not include R32")
	}
	if slices.Contains(DecidableSubset(), "XPC.M.observed-desired-fixed-point") {
		t.Error("DecidableSubset must not include R32")
	}
	// Still excludes pure-trajectory rules.
	if slices.Contains(controller, "XPC012") || slices.Contains(controller, "XPC014") {
		t.Error("ControllerSubset must still exclude trajectory rules R12/R14")
	}
}

func TestRegistryNoDuplicateCodes(t *testing.T) {
	seen := map[string]bool{}
	for _, rc := range Registry() {
		if seen[rc.Code] {
			t.Errorf("duplicate code in Registry: %q", rc.Code)
		}
		seen[rc.Code] = true
		if rc.Why == "" {
			t.Errorf("rule %q has empty decidability justification", rc.Code)
		}
	}
}
