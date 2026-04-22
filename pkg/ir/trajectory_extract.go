package ir

import (
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// EnrichTrajectoryData extracts cross-resource references (mount refs,
// ServiceAccount refs, RBAC bindings and rules) from already-parsed
// resources in the World. All extraction reads ResourceInfo.Raw — the
// YAML escape hatch — so the loader does not need to change.
//
// Also populates the static immutable-field registry for downstream
// consumers that need to reason about immutability.
func EnrichTrajectoryData(w *types.World) {
	if w == nil {
		return
	}
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

	w.ImmutableFields = ImmutableFieldRegistry()
	w.SelectorMappings = SelectorRegistry()
	extractSelectorUsages(w)
	w.LateInitMappings = LateInitRegistry()
	extractLateInitUsages(w)
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
