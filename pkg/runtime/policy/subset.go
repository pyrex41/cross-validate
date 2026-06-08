// Package policy is the runtime-policy companion to the xpc static analyzer.
// It reuses the same loader -> IR builder -> Shen checker pipeline, but
// restricts evaluation to the subset of kernel rules that are *sound* to run
// against a single Kubernetes object at admission time — i.e. rules whose
// verdict on one object never depends on simulating a deployment trajectory
// or on joining against other objects in the repo/cluster.
//
// subset.go is the decidability core: it classifies every kernel rule code
// (enumerated from kernel/check.shen) into one of three tiers and exposes the
// admission-safe subsets.
package policy

// Tier classifies a kernel rule by what it needs in order to reach a sound
// verdict at single-object admission time.
type Tier int

const (
	// TierSingleObject rules decide entirely from the bytes of the object
	// under admission. No trajectory simulation, no cross-object reference
	// join, no live-cluster diff. Sound and bounded on a single object.
	TierSingleObject Tier = iota

	// TierAmbient rules are self-contained per object but need a small,
	// read-only set of cached cluster facts (CRDs/XRDs/Compositions/
	// Functions/Providers — the "type environment") to resolve references.
	// Sound only when an ambient World snapshot is supplied.
	TierAmbient

	// TierExcluded rules are NOT runtime-safe for single-object admission:
	// they require trajectory simulation (apply-order state evolution),
	// live observed/desired diffs, or unbounded cross-repo reference joins.
	TierExcluded
)

// RuleClass is one kernel rule code together with its tier and a one-line
// decidability justification.
type RuleClass struct {
	Code string
	Tier Tier
	Why  string
}

// Registry classifies every kernel rule code found in kernel/check.shen.
// Codes are quoted exactly as the kernel emits them.
//
// Be honest: the reference-resolution rules (R3/R4/R6/R7/R28) and the
// trajectory/live-diff rules (R12/R14/R32) are TierExcluded for single-object
// admission — they fundamentally need other objects or a simulated apply.
func Registry() []RuleClass {
	return []RuleClass{
		// ---- TierSingleObject: sound on one object's bytes ----

		// R23: reads spec.deletionPolicy of one state-bearing managed
		// resource; (Group,Kind) membership + a string compare. Terminating,
		// no joins. INC-6 static floor.
		{Code: "XPC.S.crossplane-state-needs-orphan", Tier: TierSingleObject,
			Why: "single managed resource: deletionPolicy != Orphan on a state-bearing (Group,Kind); pure field read, no joins"},

		// R24: reads one ApplicationSet's template finalizers +
		// preserveResourcesOnDeletion. Self-contained on that AppSet object.
		{Code: "XPC.E.appset-finalizer-without-preserve", Tier: TierSingleObject,
			Why: "single ApplicationSet: cascading finalizer present without preserveResourcesOnDeletion; fields of the same object"},

		// R25: one ApplicationSet's name matched against prod patterns + its
		// template syncPolicy.automated. Bounded substring + field read.
		{Code: "XPC.E.prod-appset-autosync", Tier: TierSingleObject,
			Why: "single ApplicationSet: prod name pattern + automated sync enabled; bounded pattern match on one object"},

		// R29: one claim object's environment label key/value vs a fixed
		// allowed enum. Label read on the object itself.
		{Code: "XPC.E.fargate-claim-env-label", Tier: TierSingleObject,
			Why: "single claim object: required env label present and in enum; label read on the object, no references"},

		// R31: one managed resource's registered forProvider field vs its
		// canonical form. Static registry lookup + literal compare on the
		// object (no live observed state — that is R32).
		{Code: "XPC.M.forprovider-canonical-form", Tier: TierSingleObject,
			Why: "single managed resource: registered forProvider field set to non-canonical literal; static registry + field compare"},

		// R33: duplicate env-var names within one Composition's templated
		// containerDefinitions. Counting within a single object's body.
		{Code: "XPC.M.duplicate-env-key", Tier: TierSingleObject,
			Why: "single Composition: duplicate ECS env name within its own template body; intra-object counting"},

		// R22: SSA managementPolicies safety. Each emitted code reasons about
		// one managed resource's managementPolicies value; no trajectory.
		{Code: "XPC.E.ssa-managementpolicies-observe", Tier: TierSingleObject,
			Why: "single managed resource: managementPolicies == [Observe] under SSA; value classification on the object"},
		{Code: "XPC.E.ssa-managementpolicies-partial", Tier: TierSingleObject,
			Why: "single managed resource: partial managementPolicies under SSA; value classification on the object"},
		{Code: "XPC.E.ssa-managementpolicies-nondefault", Tier: TierSingleObject,
			Why: "single managed resource: non-default managementPolicies under SSA; value classification on the object"},

		// R17: schema validation of one manifest against its own CRD/XRD
		// schema. Bounded walk of the object; needs the schema, but the
		// schema travels with the object's own GVK.
		{Code: "XPC.A.resource-field-valid", Tier: TierSingleObject,
			Why: "single manifest: field validity against its own kind's schema; bounded structural walk of the object"},

		// ---- TierAmbient: per-object but needs cached type environment ----

		// R1: apiVersion validity needs the CRD/XRD set to know which
		// versions exist. Per object, but resolves against ambient schemas.
		{Code: "XPC001", Tier: TierAmbient,
			Why: "object apiVersion checked against the ambient CRD/XRD version set; needs cached type environment"},

		// R2: webhook-conversion safety needs the CRD's conversion strategy
		// from the ambient CRD set.
		{Code: "XPC002", Tier: TierAmbient,
			Why: "object's conversion safety depends on its CRD's conversion strategy held in the ambient CRD set"},

		// R8: v1/v2 machinery for a converted object needs the CRD's served
		// versions from the ambient set.
		{Code: "XPC008", Tier: TierAmbient,
			Why: "object's storage/served version machinery resolved against the ambient CRD set"},

		// R11: API deprecation needs the ambient CRD/provider catalog to know
		// which APIs are deprecated. Verdict is per object.
		{Code: "XPC011", Tier: TierAmbient,
			Why: "object's apiVersion checked for deprecation against the ambient CRD/provider catalog"},

		// R15: AppProject whitelist — a single resource's GVK vs its
		// project's whitelist; needs the ambient AppProject set.
		{Code: "XPC.D.kind-whitelisted", Tier: TierAmbient,
			Why: "object GVK checked against its AppProject whitelist; needs the ambient AppProject set"},

		// R16: selector field needs an ignoreDifferences entry; that entry
		// lives on the owning Application in the ambient set.
		{Code: "XPC.E.selector-needs-ignore-diff", Tier: TierAmbient,
			Why: "object's selector field requires an ignoreDifferences entry held on the ambient owning Application"},

		// R21: late-init field needs an ignoreDifferences entry from the
		// ambient owning Application — same shape as R16.
		{Code: "XPC.E.late-init-needs-ignore-diff", Tier: TierAmbient,
			Why: "object's late-init field requires an ignoreDifferences entry held on the ambient owning Application"},

		// R30: ExternalSecret store name must be in the ambient allowed-store
		// set; per object, but the allowlist is ambient config.
		{Code: "XPC.K.externalsecret-store", Tier: TierAmbient,
			Why: "object's secretStoreRef.name checked against the ambient allowed-store set"},

		// ---- TierExcluded: trajectory / live-diff / unbounded ref joins ----

		// R3: composition must resolve to an XRD that may live anywhere in
		// the repo — unbounded reference join, not single-object.
		{Code: "XPC003", Tier: TierExcluded,
			Why: "composition->XRD reference resolution across the repo; not a single-object decision"},
		// R4: pipeline functions must resolve to Function objects elsewhere.
		{Code: "XPC004", Tier: TierExcluded,
			Why: "composition pipeline->Function reference resolution across the repo"},
		// R5: patch typecheck joins composite XRD schema with composed CRD
		// schema across objects.
		{Code: "XPC005", Tier: TierExcluded,
			Why: "patch typecheck joins XRD and composed-CRD schemas across distinct objects"},
		// R6 / R6c: sync-wave ordering reasons over the whole apply set.
		{Code: "XPC006", Tier: TierExcluded,
			Why: "sync-wave ordering is a property of the whole apply set, not one object"},
		// R7: owner-refs cross-reference Applications and Compositions.
		{Code: "XPC007", Tier: TierExcluded,
			Why: "owner-ref consistency joins Applications and Compositions across objects"},
		// R9: bootstrap ordering reasons over composition+resource fleet.
		{Code: "XPC009", Tier: TierExcluded,
			Why: "bootstrap ordering is a fleet-level property across compositions and resources"},
		// R10: secret-taint propagates across resolved patches/objects.
		{Code: "XPC010", Tier: TierExcluded,
			Why: "secret taint propagates across resolved patches spanning multiple objects"},
		// R12: dangling-mount needs trajectory state to know if the mount
		// target is live at apply time.
		{Code: "XPC012", Tier: TierExcluded,
			Why: "dangling mount requires trajectory simulation of apply-order liveness; not single-object"},
		// R14: RBAC regression needs trajectory state across bindings/roles.
		{Code: "XPC014", Tier: TierExcluded,
			Why: "RBAC regression requires trajectory simulation across bindings and roles"},
		// R18/R19/R20: Helm/Kustomize render rules — runtime has no helm/
		// kustomize and renders nothing (SkipRender=true).
		{Code: "XPC.H.helm-renders", Tier: TierExcluded,
			Why: "needs Helm rendering; runtime builds with SkipRender=true and never renders"},
		{Code: "XPC.H.values-well-typed", Tier: TierExcluded,
			Why: "needs Helm rendering; runtime builds with SkipRender=true and never renders"},
		{Code: "XPC.H.render-deterministic", Tier: TierExcluded,
			Why: "needs repeated Helm/Kustomize renders; runtime never renders"},
		// R28: providerConfigRef must resolve to a ProviderConfig declared
		// elsewhere — unbounded reference join.
		{Code: "XPC.B.providerconfig-resolves", Tier: TierExcluded,
			Why: "providerConfigRef->ProviderConfig reference resolution across the repo"},
		// R32: observed/desired fixed point requires LIVE status (atProvider)
		// — a runtime admission request carries no observed state.
		{Code: "XPC.M.observed-desired-fixed-point", Tier: TierExcluded,
			Why: "requires live observed (atProvider) status diff against desired; not available at admission"},
	}
}

// DecidableSubset returns the codes whose Tier == TierSingleObject. This is
// the default admission subset: sound to evaluate on a single object with no
// ambient World. Includes the INC-6 floor (R23/R24/R25) plus R29/R31/R33/R22
// and R17, all confirmed self-contained from their kernel .shen.
func DecidableSubset() []string {
	return codesForTiers(TierSingleObject)
}

// AmbientSubset returns TierSingleObject + TierAmbient codes, for use when a
// cached ambient World (type environment) is supplied so ambient rules can
// resolve their references soundly.
func AmbientSubset() []string {
	return codesForTiers(TierSingleObject, TierAmbient)
}

func codesForTiers(tiers ...Tier) []string {
	want := make(map[Tier]bool, len(tiers))
	for _, t := range tiers {
		want[t] = true
	}
	var out []string
	for _, rc := range Registry() {
		if want[rc.Tier] {
			out = append(out, rc.Code)
		}
	}
	return out
}
