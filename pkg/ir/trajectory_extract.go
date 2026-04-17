package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

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
