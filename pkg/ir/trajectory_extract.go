package ir

import (
	"strconv"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/config"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// EnrichTrajectoryData extracts cross-resource references (mount refs,
// ServiceAccount refs, RBAC bindings and rules) from already-parsed
// resources in the World. All extraction reads ResourceInfo.Raw — the
// YAML escape hatch — so the loader does not need to change.
//
// Knob-shaped fields the World expects (ProdPatterns, NameCarveouts,
// BypassKeys, ImmutableFields) are resolved against built-in defaults if a
// caller skipped Builder.Build (e.g. unit tests that construct a World
// inline). In normal operation Builder.Build pre-populates them from the
// loaded *config.Config.
func EnrichTrajectoryData(w *types.World) {
	if w == nil {
		return
	}
	ensureKnobDefaults(w)

	for _, res := range w.Resources {
		switch res.Kind {
		case types.KindPod:
			extractFromPodSpec(w, res, getMap(res.Raw, "spec"))
		case types.KindDeployment, types.KindStatefulSet, types.KindDaemonSet, types.KindReplicaSet, types.KindJob:
			spec := getMap(res.Raw, "spec")
			template := getMap(spec, "template")
			extractFromPodSpec(w, res, getMap(template, "spec"))
		case types.KindCronJob:
			spec := getMap(res.Raw, "spec")
			jobTemplate := getMap(spec, "jobTemplate")
			jobSpec := getMap(jobTemplate, "spec")
			template := getMap(jobSpec, "template")
			extractFromPodSpec(w, res, getMap(template, "spec"))
		case types.KindRoleBinding, types.KindClusterRoleBinding:
			extractRBACBinding(w, res)
		case types.KindRole, types.KindClusterRole:
			extractRBACRules(w, res)
		}
	}

	w.SelectorMappings = SelectorRegistry()
	extractSelectorUsages(w)
	w.LateInitMappings = LateInitRegistry()
	extractLateInitUsages(w)
	w.CanonicalFormMappings = CanonicalFormRegistry()
	extractCanonicalFormUsages(w)
	extractFixedPointUsages(w)
	extractExternalNameAdoptFindings(w)
	extractSSAMPConflicts(w)
	extractCPDeletionPolicyFacts(w)
}

// externalNameAnnotation is the Crossplane annotation that adopts an existing
// external object (the provider observes/updates it instead of creating).
const externalNameAnnotation = "crossplane.io/external-name"

// allowMissingExternalNameAnnotation is the explicit opt-out for R35: a resource
// annotated with this key (truthy) is exempt from the must-adopt check — used
// when a fresh external object really should be created (e.g. the provider create
// path has been fixed but the registry row is not yet retired). Mirrors the
// bypass-annotation pattern used by R23/R31.
const allowMissingExternalNameAnnotation = "xpc.io/allow-missing-external-name"

// extractExternalNameAdoptFindings populates w.ExternalNameAdoptFindings
// (category I, Tier-1 resource walk) by joining ExternalNameAdoptRegistry against
// every concrete managed resource in the World. A resource whose (group, kind) is
// registered as must-adopt and which lacks a non-empty crossplane.io/external-name
// annotation (and is not bypassed) is a finding: the provider's Create path is
// broken / non-idempotent for this kind, so without the annotation it never
// reconciles cleanly. Error severity — the failure is definite and
// registry-confirmed, so the kernel does not soften it.
func extractExternalNameAdoptFindings(w *types.World) {
	type gk struct{ group, kind string }
	index := make(map[gk]types.ExternalNameAdoptMapping)
	for _, m := range ExternalNameAdoptRegistry() {
		index[gk{m.Group, m.Kind}] = m
	}
	if len(index) == 0 {
		return
	}
	for _, res := range w.Resources {
		// A nameless doc (e.g. a flat claim shape inside a Helm values file) is
		// not a real subject — only a committed/rendered manifest with a name.
		if res.Name == "" {
			continue
		}
		resGroup := groupFromAPIVersion(res.APIVersion)
		m, ok := index[gk{resGroup, res.Kind}]
		if !ok {
			continue
		}
		if annotationTruthy(res.Annotations, allowMissingExternalNameAnnotation) {
			continue
		}
		if strings.TrimSpace(res.Annotations[externalNameAnnotation]) != "" {
			// Adopted — the provider observes the existing object. PASS.
			continue
		}
		w.ExternalNameAdoptFindings = append(w.ExternalNameAdoptFindings, types.ExternalNameAdoptFinding{
			Group:     resGroup,
			Kind:      res.Kind,
			Name:      res.Name,
			Namespace: res.Namespace,
			Reason:    m.Reason,
			Source:    res.Source,
		})
	}
}

// ensureKnobDefaults populates the user-extensible knob fields on w if a
// caller (typically a test) constructed the World without going through
// Builder.Build. Idempotent: nil-checks each slice individually.
func ensureKnobDefaults(w *types.World) {
	if w.ImmutableFields == nil {
		w.ImmutableFields = config.ResolveImmutableFields(nil, ImmutableFieldRegistry())
	}
	if w.StateBearingKinds == nil {
		w.StateBearingKinds = config.ResolveStateBearingKinds(nil, StateBearingKindsRegistry())
	}
	if w.EnvLabelKey == "" {
		w.EnvLabelKey, w.EnvLabelClaimKinds, w.EnvLabelAllowedValues = config.ResolveEnvLabel(nil)
	}
	if w.ProdPatterns == nil {
		w.ProdPatterns = config.ResolveProdPatterns(nil)
	}
	if w.BypassKeys.AllowDelete == nil {
		w.BypassKeys.AllowDelete = config.ResolveAllowDeleteKeys(nil)
	}
	if w.BypassKeys.AllowImmutableChange == nil {
		w.BypassKeys.AllowImmutableChange = config.ResolveAllowImmutableChangeKeys(nil)
	}
	if w.NameCarveouts == nil {
		w.NameCarveouts = map[string][]string{
			"crossplane-state-needs-orphan": config.ResolveCrossplaneStateNeedsOrphanCarveouts(nil),
		}
	} else if _, ok := w.NameCarveouts["crossplane-state-needs-orphan"]; !ok {
		w.NameCarveouts["crossplane-state-needs-orphan"] = config.ResolveCrossplaneStateNeedsOrphanCarveouts(nil)
	}
}

// extractCPDeletionPolicyFacts walks every resource whose (Group, Kind) is in
// the state-bearing allowlist (see StateBearingKindsRegistry) and emits one
// fact per match. The Shen rule R23 decides whether to fire based on the fact
// contents — this function emits unconditionally so the kernel has full
// visibility into the invariant.
func extractCPDeletionPolicyFacts(w *types.World) {
	if w == nil || len(w.Resources) == 0 {
		return
	}
	allow := make(map[string]bool, 16)
	// Prefer the resolved overlay on the World (populated by Builder.Build
	// from xpc.yaml). Fall back to the raw registry only when the World
	// was constructed inline without going through the builder — in normal
	// runs ensureKnobDefaults will already have set this slice above.
	source := w.StateBearingKinds
	if source == nil {
		source = StateBearingKindsRegistry()
	}
	for _, gk := range source {
		allow[gk.Group+"/"+gk.Kind] = true
	}
	for _, res := range w.Resources {
		group := groupFromAPIVersion(res.APIVersion)
		if !allow[group+"/"+res.Kind] {
			continue
		}
		policy := ""
		if spec, ok := res.Raw["spec"].(map[string]interface{}); ok {
			if dp, ok := spec["deletionPolicy"].(string); ok {
				policy = dp
			}
		}
		bypass := w.BypassKeys.Has(res.Annotations, types.BypassAllowDelete)
		w.CPDeletionPolicyFacts = append(w.CPDeletionPolicyFacts, types.CPDeletionPolicyFact{
			Group:          group,
			Kind:           res.Kind,
			Name:           res.Name,
			Namespace:      res.Namespace,
			DeletionPolicy: policy,
			Bypass:         bypass,
			Source:         res.Source,
		})
	}
}

// extractLateInitUsages populates w.LateInitUsages by consulting
// w.LateInitMappings against each resource's Raw map. Array-indexed paths
// (containing "[]" or "[*]") are expanded via WalkPath, so every matching
// array element produces its own usage row.
func extractLateInitUsages(w *types.World) {
	type gk struct{ group, kind string }
	index := make(map[gk][]types.LateInitMapping)
	for _, m := range w.LateInitMappings {
		key := gk{m.Group, m.Kind}
		index[key] = append(index[key], m)
	}

	for _, res := range w.Resources {
		resGroup := groupFromAPIVersion(res.APIVersion)
		key := gk{resGroup, res.Kind}
		mappings, ok := index[key]
		if !ok {
			continue
		}
		for _, m := range mappings {
			for _, hit := range WalkPath(res.Raw, m.FieldPath) {
				// For scalar paths, hit.Path is the same as m.FieldPath; for
				// array-indexed paths it is the concrete rendered form (e.g.
				// "spec.forProvider.launchTemplate[0].id"). Use the concrete
				// form so each array element surfaces as its own usage row
				// rather than collapsing into duplicates.
				w.LateInitUsages = append(w.LateInitUsages, types.LateInitUsage{
					ResourceGroup:     resGroup,
					ResourceKind:      res.Kind,
					ResourceName:      res.Name,
					ResourceNamespace: res.Namespace,
					FieldPath:         hit.Path,
					Source:            res.Source,
				})
			}
		}
	}
}

// allowNonCanonicalAnnotation is the explicit opt-out for R31/R32: a resource
// annotated with this key (truthy value) is exempt from the canonical-form and
// fixed-point checks. Mirrors the bypass-annotation pattern used by R23.
const allowNonCanonicalAnnotation = "xpc.io/allow-noncanonical"

// extractCanonicalFormUsages populates w.CanonicalFormUsages (Tier-1, static)
// by joining w.CanonicalFormMappings against each resource's Raw map. For each
// registered (Group, Kind, FieldPath) it reads the scalar value, classifies it
// against the mapping's Detector, and records the managementPolicies / bypass
// verdicts the kernel needs. Works on any concrete managed resource present in
// the World — a raw committed MR or one produced by Helm/Crossplane render.
func extractCanonicalFormUsages(w *types.World) {
	type gk struct{ group, kind string }
	index := make(map[gk][]types.CanonicalFormMapping)
	for _, m := range w.CanonicalFormMappings {
		index[gk{m.Group, m.Kind}] = append(index[gk{m.Group, m.Kind}], m)
	}

	for _, res := range w.Resources {
		resGroup := groupFromAPIVersion(res.APIVersion)
		mappings, ok := index[gk{resGroup, res.Kind}]
		if !ok {
			continue
		}
		updDisabled := managementUpdateDisabled(getMap(res.Raw, "spec"))
		bypass := annotationTruthy(res.Annotations, allowNonCanonicalAnnotation)
		for _, m := range mappings {
			for _, hit := range WalkPath(res.Raw, m.FieldPath) {
				val, ok := scalarString(hit.Value)
				if !ok || val == "" {
					// Field absent or non-scalar (e.g. a value xpc cannot read
					// because it is still a Composition template). Emit no usage
					// — Tier-2 handles the unrendered-template case.
					continue
				}
				w.CanonicalFormUsages = append(w.CanonicalFormUsages, types.CanonicalFormUsage{
					ResourceGroup:     resGroup,
					ResourceKind:      res.Kind,
					ResourceName:      res.Name,
					ResourceNamespace: res.Namespace,
					FieldPath:         hit.Path,
					Value:             val,
					Canonical:         m.Canonical,
					Reason:            m.Reason,
					NonCanonical:      failsCanonicalDetector(m.Detector, val),
					UpdateDisabled:    updDisabled,
					Bypass:            bypass,
					Source:            res.Source,
				})
			}
		}
	}
}

// extractFixedPointUsages populates w.FixedPointUsages (Tier-3, dynamic) by
// comparing each managed resource's spec.forProvider leaves against the
// matching status.atProvider leaves. A scalar string leaf present in both but
// holding different values is the reconcile-storm fingerprint: desired never
// converges to observed. Only fires on status-bearing resources (a
// --from-cluster snapshot merged into the World); on disk manifests
// status.atProvider is absent and this produces nothing.
//
// A leaf whose (Group, Kind, leaf) is in the canonical-form registry is marked
// Registered: a known-non-convergent field, conclusive from a single snapshot
// (the kernel escalates it to error). Unregistered divergences are the
// high-recall long tail the registry does not know about (warn — confirm with
// a second snapshot, since a single snapshot cannot distinguish a storm from a
// resource mid-update).
func extractFixedPointUsages(w *types.World) {
	type gkl struct{ group, kind, leaf string }
	registered := make(map[gkl]bool)
	for _, m := range w.CanonicalFormMappings {
		registered[gkl{m.Group, m.Kind, leafSeg(m.FieldPath)}] = true
	}

	for _, res := range w.Resources {
		spec := getMap(res.Raw, "spec")
		forProvider := getMap(spec, "forProvider")
		status := getMap(res.Raw, "status")
		atProvider := getMap(status, "atProvider")
		if len(forProvider) == 0 || len(atProvider) == 0 {
			continue
		}
		resGroup := groupFromAPIVersion(res.APIVersion)
		updDisabled := managementUpdateDisabled(spec)
		bypass := annotationTruthy(res.Annotations, allowNonCanonicalAnnotation)
		if bypass {
			continue
		}

		desired := map[string]string{}
		flattenStringLeaves("", forProvider, desired)
		observed := map[string]string{}
		flattenStringLeaves("", atProvider, observed)

		for leaf, dv := range desired {
			ov, ok := observed[leaf]
			if !ok || dv == ov {
				continue
			}
			w.FixedPointUsages = append(w.FixedPointUsages, types.FixedPointUsage{
				ResourceGroup:     resGroup,
				ResourceKind:      res.Kind,
				ResourceName:      res.Name,
				ResourceNamespace: res.Namespace,
				FieldPath:         "spec.forProvider." + leaf,
				Desired:           dv,
				Observed:          ov,
				Registered:        registered[gkl{resGroup, res.Kind, lastDotted(leaf)}],
				UpdateDisabled:    updDisabled,
				Source:            res.Source,
			})
		}
	}
}

// failsCanonicalDetector reports whether value violates the named detector.
func failsCanonicalDetector(detector, value string) bool {
	switch detector {
	case "arn-requires-revision":
		// The identifying segment (after the last "/", or the whole scalar) must
		// carry a ":revision" suffix. "arn:aws:ecs:..." has colons in the
		// service prefix, so only the post-"/" segment is examined.
		seg := value
		if i := strings.LastIndex(value, "/"); i >= 0 {
			seg = value[i+1:]
		}
		return !strings.Contains(seg, ":")
	default:
		return false
	}
}

// managementUpdateDisabled reports whether spec.managementPolicies is present
// and omits both "*" and "Update". When so, upjet never calls the external
// Update, so a non-canonical forProvider value cannot drive a storm. An absent
// managementPolicies field defaults to ["*"] (full management) → not disabled.
func managementUpdateDisabled(spec map[string]interface{}) bool {
	raw, ok := spec["managementPolicies"]
	if !ok {
		return false
	}
	list, ok := raw.([]interface{})
	if !ok {
		return false
	}
	for _, p := range list {
		s, _ := p.(string)
		if s == "*" || s == "Update" {
			return false
		}
	}
	return true
}

// annotationTruthy reports whether annotations[key] is set to a truthy value.
func annotationTruthy(annotations map[string]string, key string) bool {
	v, ok := annotations[key]
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

// scalarString returns the string form of v when v is a scalar string (the
// only shape normalization bites in practice); ("", false) otherwise.
func scalarString(v interface{}) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// flattenStringLeaves recursively walks a decoded map and records every scalar
// string leaf under its dotted path (arrays use [i] indices). Non-string
// scalars and container nodes are skipped — see extractFixedPointUsages.
func flattenStringLeaves(prefix string, m map[string]interface{}, out map[string]string) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch t := v.(type) {
		case string:
			out[path] = t
		case map[string]interface{}:
			flattenStringLeaves(path, t, out)
		case []interface{}:
			for i, e := range t {
				ip := path + "[" + strconv.Itoa(i) + "]"
				if sub, ok := e.(map[string]interface{}); ok {
					flattenStringLeaves(ip, sub, out)
				} else if s, ok := e.(string); ok {
					out[ip] = s
				}
			}
		}
	}
}

// leafSeg / lastDotted return the last dotted segment of a path. leafSeg
// strips array indices from its input first ("a.b[0].c" → "c").
func leafSeg(path string) string { return lastDotted(path) }

func lastDotted(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// groupFromAPIVersion extracts the API group from an APIVersion string.
// "autoscaling.aws.upbound.io/v1beta1" → "autoscaling.aws.upbound.io".
// "v1" (core group) → "".
func groupFromAPIVersion(apiVersion string) string {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// extractSelectorUsages populates w.SelectorUsages by consulting
// w.SelectorMappings against each resource's Raw map. Both scalar and
// array-indexed paths (templates containing "[]" or "[*]") are handled via
// WalkPath — each array element that declares the selector surfaces its own
// SelectorUsage, with the ResolvedPath specialized to the same concrete index.
func extractSelectorUsages(w *types.World) {
	// Build a lookup index: (group, kind) → []SelectorMapping for fast access.
	type gk struct{ group, kind string }
	index := make(map[gk][]types.SelectorMapping)
	for _, m := range w.SelectorMappings {
		key := gk{m.Group, m.Kind}
		index[key] = append(index[key], m)
	}

	for _, res := range w.Resources {
		resGroup := groupFromAPIVersion(res.APIVersion)
		key := gk{resGroup, res.Kind}
		mappings, ok := index[key]
		if !ok {
			continue
		}
		for _, m := range mappings {
			// Walk the SelectorPath template. For scalar paths this produces
			// 0 or 1 hit; for array-indexed templates one hit per element that
			// declares the selector. We intentionally do not also gate on the
			// ResolvedPath existing — the whole point of R16 is that the
			// resolved path is typically absent pre-reconcile (Crossplane
			// populates it later) and Argo then fights it.
			hits := WalkPath(res.Raw, m.SelectorPath)
			for _, hit := range hits {
				// Specialize the ResolvedPath template with the same index
				// signature as the SelectorPath hit, so a
				// "launchTemplate[*].idSelector" hit at index 0 produces a
				// usage whose ResolvedPath is "launchTemplate[0].id".
				resolved := specializeIndices(m.ResolvedPath, hit.Path, m.SelectorPath)
				w.SelectorUsages = append(w.SelectorUsages, types.SelectorUsage{
					ResourceGroup:     resGroup,
					ResourceKind:      res.Kind,
					ResourceName:      res.Name,
					ResourceNamespace: res.Namespace,
					SelectorPath:      hit.Path,
					ResolvedPath:      resolved,
					Source:            res.Source,
				})
			}
		}
	}
}

// specializeIndices substitutes the wildcard bracket placeholders in
// resolvedTemplate with the concrete "[N]" indices taken from concreteSelector.
// selectorTemplate is used to identify which bracket positions are wildcards
// (as opposed to explicit indices, which should be preserved verbatim).
//
// For paths with no wildcards, resolvedTemplate is returned unchanged.
func specializeIndices(resolvedTemplate, concreteSelector, selectorTemplate string) string {
	if !strings.Contains(resolvedTemplate, "[]") && !strings.Contains(resolvedTemplate, "[*]") {
		return resolvedTemplate
	}
	// Extract the concrete index sequence from the rendered selector path —
	// anything between "[" and "]" becomes the next replacement token.
	indices := extractBracketContents(concreteSelector)
	if len(indices) == 0 {
		return resolvedTemplate
	}
	return substituteWildcards(resolvedTemplate, indices)
}

// extractBracketContents returns the content of every "[...]" pair in s,
// in left-to-right order. For "xs[0].ys[1]" it returns ["0", "1"].
func extractBracketContents(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "[")
		if i < 0 {
			return out
		}
		j := strings.Index(s[i:], "]")
		if j < 0 {
			return out
		}
		out = append(out, s[i+1:i+j])
		s = s[i+j+1:]
	}
}

// substituteWildcards walks template and replaces each wildcard bracket
// ("[]" or "[*]") with "[<index>]" pulled in order from indices. Explicit
// numeric brackets in the template are left untouched.
func substituteWildcards(template string, indices []string) string {
	var b strings.Builder
	var cur int
	for i := 0; i < len(template); {
		if template[i] != '[' {
			b.WriteByte(template[i])
			i++
			continue
		}
		end := strings.Index(template[i:], "]")
		if end < 0 {
			// Malformed — bail and emit the remainder verbatim.
			b.WriteString(template[i:])
			break
		}
		inside := template[i+1 : i+end]
		if (inside == "" || inside == "*") && cur < len(indices) {
			b.WriteByte('[')
			b.WriteString(indices[cur])
			b.WriteByte(']')
			cur++
		} else {
			b.WriteString(template[i : i+end+1])
		}
		i += end + 1
	}
	return b.String()
}

// extractFromPodSpec walks a PodSpec-shaped map and emits MountRefs for each
// ConfigMap/Secret reference (volumes, projected volumes, envFrom) and one
// SARef for the pod's ServiceAccount.
func extractFromPodSpec(w *types.World, owner types.ResourceInfo, podSpec map[string]interface{}) {
	if podSpec == nil {
		return
	}

	// serviceAccount is the deprecated alias for serviceAccountName; honor either.
	saName, _ := podSpec["serviceAccountName"].(string)
	if saName == "" {
		saName, _ = podSpec["serviceAccount"].(string)
	}
	if saName != "" {
		w.SARefs = append(w.SARefs, types.SARef{
			OwnerKind:      owner.Kind,
			OwnerName:      owner.Name,
			OwnerNamespace: owner.Namespace,
			SAName:         saName,
			SANamespace:    owner.Namespace,
			Source:         owner.Source,
		})
	}

	for _, v := range getSlice(podSpec, "volumes") {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if cm := getMap(vm, "configMap"); cm != nil {
			optional, _ := cm["optional"].(bool)
			name, _ := cm["name"].(string)
			if name != "" {
				w.MountRefs = append(w.MountRefs, types.MountRef{
					OwnerKind:       owner.Kind,
					OwnerName:       owner.Name,
					OwnerNamespace:  owner.Namespace,
					TargetKind:      types.KindConfigMap,
					TargetName:      name,
					TargetNamespace: owner.Namespace,
					MountKind:       "volume",
					Optional:        optional,
					Source:          owner.Source,
				})
			}
		}
		if sec := getMap(vm, "secret"); sec != nil {
			optional, _ := sec["optional"].(bool)
			name, _ := sec["secretName"].(string)
			if name != "" {
				w.MountRefs = append(w.MountRefs, types.MountRef{
					OwnerKind:       owner.Kind,
					OwnerName:       owner.Name,
					OwnerNamespace:  owner.Namespace,
					TargetKind:      types.KindSecret,
					TargetName:      name,
					TargetNamespace: owner.Namespace,
					MountKind:       "volume",
					Optional:        optional,
					Source:          owner.Source,
				})
			}
		}
		if proj := getMap(vm, "projected"); proj != nil {
			for _, s := range getSlice(proj, "sources") {
				sm, ok := s.(map[string]interface{})
				if !ok {
					continue
				}
				if cm := getMap(sm, "configMap"); cm != nil {
					optional, _ := cm["optional"].(bool)
					name, _ := cm["name"].(string)
					if name != "" {
						w.MountRefs = append(w.MountRefs, types.MountRef{
							OwnerKind:       owner.Kind,
							OwnerName:       owner.Name,
							OwnerNamespace:  owner.Namespace,
							TargetKind:      types.KindConfigMap,
							TargetName:      name,
							TargetNamespace: owner.Namespace,
							MountKind:       "projected",
							Optional:        optional,
							Source:          owner.Source,
						})
					}
				}
				if sec := getMap(sm, "secret"); sec != nil {
					optional, _ := sec["optional"].(bool)
					name, _ := sec["name"].(string)
					if name != "" {
						w.MountRefs = append(w.MountRefs, types.MountRef{
							OwnerKind:       owner.Kind,
							OwnerName:       owner.Name,
							OwnerNamespace:  owner.Namespace,
							TargetKind:      types.KindSecret,
							TargetName:      name,
							TargetNamespace: owner.Namespace,
							MountKind:       "projected",
							Optional:        optional,
							Source:          owner.Source,
						})
					}
				}
			}
		}
	}

	for _, containerKey := range []string{"containers", "initContainers"} {
		for _, c := range getSlice(podSpec, containerKey) {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			for _, ef := range getSlice(cm, "envFrom") {
				em, ok := ef.(map[string]interface{})
				if !ok {
					continue
				}
				if cfg := getMap(em, "configMapRef"); cfg != nil {
					optional, _ := cfg["optional"].(bool)
					name, _ := cfg["name"].(string)
					if name != "" {
						w.MountRefs = append(w.MountRefs, types.MountRef{
							OwnerKind:       owner.Kind,
							OwnerName:       owner.Name,
							OwnerNamespace:  owner.Namespace,
							TargetKind:      types.KindConfigMap,
							TargetName:      name,
							TargetNamespace: owner.Namespace,
							MountKind:       "envFrom",
							Optional:        optional,
							Source:          owner.Source,
						})
					}
				}
				if sec := getMap(em, "secretRef"); sec != nil {
					optional, _ := sec["optional"].(bool)
					name, _ := sec["name"].(string)
					if name != "" {
						w.MountRefs = append(w.MountRefs, types.MountRef{
							OwnerKind:       owner.Kind,
							OwnerName:       owner.Name,
							OwnerNamespace:  owner.Namespace,
							TargetKind:      types.KindSecret,
							TargetName:      name,
							TargetNamespace: owner.Namespace,
							MountKind:       "envFrom",
							Optional:        optional,
							Source:          owner.Source,
						})
					}
				}
			}
		}
	}
}

// extractRBACBinding emits one RBACBinding per (subject, roleRef) pair found
// on a RoleBinding or ClusterRoleBinding.
func extractRBACBinding(w *types.World, res types.ResourceInfo) {
	roleRef := getMap(res.Raw, "roleRef")
	if roleRef == nil {
		return
	}
	roleKind, _ := roleRef["kind"].(string)
	roleName, _ := roleRef["name"].(string)
	if roleKind == "" || roleName == "" {
		return
	}
	// Resolve the Role's namespace per Kubernetes RBAC semantics:
	//   - ClusterRoleBinding → RoleRef must target a ClusterRole (cluster-
	//     scoped) → empty namespace.
	//   - RoleBinding + RoleRef.kind=Role → Role lives in the binding's
	//     own namespace.
	//   - RoleBinding + RoleRef.kind=ClusterRole → ClusterRole is still
	//     cluster-scoped → empty namespace.
	roleNs := ""
	if res.Kind == "RoleBinding" && roleKind == "Role" {
		roleNs = res.Namespace
	}
	for _, s := range getSlice(res.Raw, "subjects") {
		sm, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		subjKind, _ := sm["kind"].(string)
		subjName, _ := sm["name"].(string)
		subjNs, _ := sm["namespace"].(string)
		if subjKind == "" || subjName == "" {
			continue
		}
		w.RBACBindings = append(w.RBACBindings, types.RBACBinding{
			BindingKind:      res.Kind,
			BindingName:      res.Name,
			BindingNamespace: res.Namespace,
			SubjectKind:      subjKind,
			SubjectName:      subjName,
			SubjectNamespace: subjNs,
			RoleKind:         roleKind,
			RoleName:         roleName,
			RoleNamespace:    roleNs,
			Source:           res.Source,
		})
	}
}

// extractRBACRules emits one RBACRule per entry in a Role or ClusterRole's
// rules list.
func extractRBACRules(w *types.World, res types.ResourceInfo) {
	for _, r := range getSlice(res.Raw, "rules") {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		rule := types.RBACRule{
			OwnerKind:      res.Kind,
			OwnerName:      res.Name,
			OwnerNamespace: res.Namespace,
			APIGroups:      getStringSlice(rm, "apiGroups"),
			Resources:      getStringSlice(rm, "resources"),
			Verbs:          getStringSlice(rm, "verbs"),
			ResourceNames:  getStringSlice(rm, "resourceNames"),
			Source:         res.Source,
		}
		w.RBACRules = append(w.RBACRules, rule)
	}
}

// extractSSAMPConflicts walks every resource whose owning Argo Application
// sets syncPolicy.syncOptions.ServerSideApply and records one SSAMPConflict
// per (resource, managementPolicies) pair. Every SSA+managementPolicies
// combination is emitted unconditionally — the Shen rule R22 inspects the
// World.SSAMPMode fact to decide which sub-code fires for each row.
//
// Resources whose OwningApp is empty (unclaimed by any Application) are
// skipped: without a join key to SSA we cannot make a safe statement.
// Resources whose owning Application has SSA=false are also skipped —
// there is no SSA/managementPolicies interaction to report.
//
// Kept as a purely additive tail of this file to avoid merge conflict with
// parallel array-path walker work.
func extractSSAMPConflicts(w *types.World) {
	if w == nil || len(w.Resources) == 0 || len(w.ArgoApps) == 0 {
		return
	}

	// Build a one-shot lookup from Application name to ServerSideApply.
	ssaByApp := make(map[string]bool, len(w.ArgoApps))
	for _, app := range w.ArgoApps {
		ssaByApp[app.Name] = app.SyncPolicy.SyncOptions.ServerSideApply
	}

	for _, res := range w.Resources {
		if res.OwningApp == "" {
			continue
		}
		ssa, known := ssaByApp[res.OwningApp]
		if !known || !ssa {
			// Only flag rows whose owning app opts into SSA. Resources
			// owned by non-SSA apps have no interaction to report.
			continue
		}
		policies := readManagementPolicies(res.Raw)
		if policies == nil {
			// No managementPolicies declared — the default is "all
			// policies active", which by construction cannot conflict
			// with SSA in ways R22 catches. Skip.
			continue
		}
		w.SSAMPConflicts = append(w.SSAMPConflicts, types.SSAMPConflict{
			AppName:            res.OwningApp,
			ServerSideApply:    true,
			ManagementPolicies: policies,
			ResourceGroup:      groupFromAPIVersion(res.APIVersion),
			ResourceKind:       res.Kind,
			ResourceName:       res.Name,
			ResourceNamespace:  res.Namespace,
			Source:             res.Source,
		})
	}
}

// readManagementPolicies returns spec.managementPolicies as a []string if
// present and list-shaped, or nil if absent / malformed. A non-nil empty
// slice means the resource declared an explicit empty list — R22 treats
// this as "Observe"-equivalent (Crossplane does nothing).
func readManagementPolicies(raw map[string]interface{}) []string {
	if raw == nil {
		return nil
	}
	spec, ok := raw["spec"].(map[string]interface{})
	if !ok {
		return nil
	}
	mp, ok := spec["managementPolicies"]
	if !ok {
		return nil
	}
	items, ok := mp.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
