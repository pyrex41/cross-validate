package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// ExternalNameAdoptRegistry returns the static catalog of Crossplane managed
// resource kinds whose provider Create path is broken / non-idempotent (or which
// target a singleton external object that already exists), so the resource MUST
// carry a `crossplane.io/external-name` annotation to ADOPT the existing external
// object instead of attempting a (failing or duplicating) Create. Each row is the
// seed for category I (Provider-capability obligations) rule R35
// (XPC.I.must-adopt-external-name).
//
// Why this is a provider-capability obligation (category I), not convergence (M):
//
//   - Category M (R31/R32/R34) is a STEADY-STATE problem: the resource reconciles
//     and creates fine, but desired never equals the cloud's read-back, so upjet
//     re-Updates forever. The fight is upjet-vs-cloud at steady state.
//   - This rule is a CREATE-PATH problem: the resource never reaches steady state
//     at all. The provider's external Create either errors (the SigNoz provider's
//     create path returns HTML against the custom build → `invalid character '<'`)
//     or duplicates a singleton that already exists. The remedy is to skip Create
//     entirely by adopting the existing external object via external-name. This is
//     a capability gap in the installed provider for this kind, which is exactly
//     what category I scopes.
//
// Detection is a pure presence check: a resource whose (group, kind) matches a row
// and which lacks (a non-empty) `crossplane.io/external-name` fires at error — the
// failure is definite and registry-confirmed (the provider WILL fail or duplicate
// Create for this kind), so it is not a heuristic. The escape hatch is the
// `xpc.io/allow-missing-external-name` annotation (for the rare case where a fresh
// external object really should be created, e.g. the provider create path has been
// fixed but the registry row not yet retired) plus the standard .xpc-waivers.yaml.
//
// Append-only; anchor each Reason to the MR that adopted via external-name.
func ExternalNameAdoptRegistry() []types.ExternalNameAdoptMapping {
	return []types.ExternalNameAdoptMapping{
		// ── alert.signoz.crossplane.io / Alert ─────────────────────────────
		// abd5aa10ed (INC-8): provider-signoz v0.3.0's create/delete path is
		// broken against the custom signoz:v0.110.1-header-auth build — it returns
		// an HTML error page, so the SDK unmarshal fails with
		// `invalid character '<' looking for beginning of value` and the Alert MR
		// never reconciles (observe/update work fine). The ElastiCache alert rules
		// were created out-of-band via the SigNoz API and adopted by adding
		// `crossplane.io/external-name: <rule-id>` so Crossplane observes instead
		// of creating. Retire this row once the provider create path is fixed.
		{
			Group:  "alert.signoz.crossplane.io",
			Kind:   "Alert",
			Reason: "provider-signoz Alert create path is broken against the custom signoz build (returns HTML → \"invalid character '<'\"); the rule must be adopted via crossplane.io/external-name (created out-of-band via the SigNoz API), not created; MR abd5aa10ed / INC-8",
		},
	}
}
