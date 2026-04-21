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
// w.LateInitMappings against each resource's Raw map. Same array-path deferral
// as extractSelectorUsages: entries containing "[]" are skipped on this pass.
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
			if strings.Contains(m.FieldPath, "[]") {
				continue
			}
			if walkScalarPath(res.Raw, m.FieldPath) {
				w.LateInitUsages = append(w.LateInitUsages, types.LateInitUsage{
					ResourceGroup:     resGroup,
					ResourceKind:      res.Kind,
					ResourceName:      res.Name,
					ResourceNamespace: res.Namespace,
					FieldPath:         m.FieldPath,
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

// walkScalarPath walks a dotted path (no "[]" segments) in a raw map and
// reports whether the field exists and is non-nil.
// e.g. "spec.forProvider.vpcZoneIdentifierSelector" → true when present.
func walkScalarPath(raw map[string]interface{}, path string) bool {
	if raw == nil || path == "" {
		return false
	}
	parts := strings.SplitN(path, ".", 2)
	key := parts[0]
	val, ok := raw[key]
	if !ok || val == nil {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	next, ok := val.(map[string]interface{})
	if !ok {
		return false
	}
	return walkScalarPath(next, parts[1])
}

// extractSelectorUsages populates w.SelectorUsages by consulting
// w.SelectorMappings against each resource's Raw map.
// Array-indexed paths (containing "[]") are skipped with a TODO: the entries
// are present in the registry for completeness but require element-wise
// walking which is deferred to a follow-up pass.
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
			// Skip array-indexed paths on first pass; their resolution
			// requires iterating slice elements which is not yet implemented.
			// TODO: implement array-path walking (spec step 3 note).
			if strings.Contains(m.SelectorPath, "[]") {
				continue
			}
			if walkScalarPath(res.Raw, m.SelectorPath) {
				w.SelectorUsages = append(w.SelectorUsages, types.SelectorUsage{
					ResourceGroup:     resGroup,
					ResourceKind:      res.Kind,
					ResourceName:      res.Name,
					ResourceNamespace: res.Namespace,
					SelectorPath:      m.SelectorPath,
					ResolvedPath:      m.ResolvedPath,
					Source:            res.Source,
				})
			}
		}
	}
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
