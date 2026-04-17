package ir

import (
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestEnrichTrajectoryData_EmptyWorld(t *testing.T) {
	w := types.NewWorld()
	EnrichTrajectoryData(w)
	if len(w.MountRefs) != 0 {
		t.Errorf("expected no MountRefs, got %d", len(w.MountRefs))
	}
	if len(w.SARefs) != 0 {
		t.Errorf("expected no SARefs, got %d", len(w.SARefs))
	}
	if len(w.RBACBindings) != 0 {
		t.Errorf("expected no RBACBindings, got %d", len(w.RBACBindings))
	}
	if len(w.RBACRules) != 0 {
		t.Errorf("expected no RBACRules, got %d", len(w.RBACRules))
	}
	if len(w.ImmutableFields) == 0 {
		t.Errorf("expected ImmutableFields to be populated from the registry")
	}
}

func TestEnrichTrajectoryData_PodVolumes(t *testing.T) {
	w := types.NewWorld()
	w.Resources = append(w.Resources, types.ResourceInfo{
		APIVersion: "v1", Kind: "Pod", Name: "web", Namespace: "app",
		Source: types.SourceLocation{File: "pod.yaml", Line: 1},
		Raw: map[string]interface{}{
			"spec": map[string]interface{}{
				"serviceAccountName": "web-sa",
				"volumes": []interface{}{
					map[string]interface{}{
						"name": "cfg",
						"configMap": map[string]interface{}{
							"name":     "web-config",
							"optional": false,
						},
					},
					map[string]interface{}{
						"name": "tls",
						"secret": map[string]interface{}{
							"secretName": "web-tls",
							"optional":   true,
						},
					},
				},
			},
		},
	})
	EnrichTrajectoryData(w)

	if len(w.MountRefs) != 2 {
		t.Fatalf("expected 2 MountRefs, got %d: %+v", len(w.MountRefs), w.MountRefs)
	}
	if len(w.SARefs) != 1 || w.SARefs[0].SAName != "web-sa" {
		t.Errorf("expected 1 SARef to web-sa, got %+v", w.SARefs)
	}
	cm := w.MountRefs[0]
	if cm.TargetKind != "ConfigMap" || cm.TargetName != "web-config" || cm.MountKind != "volume" || cm.Optional {
		t.Errorf("unexpected ConfigMap mount: %+v", cm)
	}
	sec := w.MountRefs[1]
	if sec.TargetKind != "Secret" || sec.TargetName != "web-tls" || sec.MountKind != "volume" || !sec.Optional {
		t.Errorf("unexpected Secret mount: %+v", sec)
	}
	if cm.TargetNamespace != "app" {
		t.Errorf("expected TargetNamespace to default to OwnerNamespace 'app', got %q", cm.TargetNamespace)
	}
}

func TestEnrichTrajectoryData_DeploymentEnvFrom(t *testing.T) {
	w := types.NewWorld()
	w.Resources = append(w.Resources, types.ResourceInfo{
		APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Namespace: "app",
		Source: types.SourceLocation{File: "api.yaml", Line: 1},
		Raw: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name": "api",
								"envFrom": []interface{}{
									map[string]interface{}{
										"configMapRef": map[string]interface{}{
											"name": "api-env",
										},
									},
									map[string]interface{}{
										"secretRef": map[string]interface{}{
											"name":     "api-secrets",
											"optional": true,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	EnrichTrajectoryData(w)

	if len(w.MountRefs) != 2 {
		t.Fatalf("expected 2 MountRefs, got %d: %+v", len(w.MountRefs), w.MountRefs)
	}
	cm := w.MountRefs[0]
	if cm.MountKind != "envFrom" || cm.TargetKind != "ConfigMap" || cm.TargetName != "api-env" {
		t.Errorf("unexpected envFrom configmap mount: %+v", cm)
	}
	sec := w.MountRefs[1]
	if sec.MountKind != "envFrom" || sec.TargetKind != "Secret" || sec.TargetName != "api-secrets" || !sec.Optional {
		t.Errorf("unexpected envFrom secret mount: %+v", sec)
	}
}

func TestEnrichTrajectoryData_CronJob(t *testing.T) {
	w := types.NewWorld()
	w.Resources = append(w.Resources, types.ResourceInfo{
		APIVersion: "batch/v1", Kind: "CronJob", Name: "nightly", Namespace: "ops",
		Source: types.SourceLocation{File: "cron.yaml", Line: 1},
		Raw: map[string]interface{}{
			"spec": map[string]interface{}{
				"jobTemplate": map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"serviceAccountName": "nightly-sa",
								"volumes": []interface{}{
									map[string]interface{}{
										"name": "script",
										"configMap": map[string]interface{}{
											"name": "nightly-script",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	EnrichTrajectoryData(w)

	if len(w.MountRefs) != 1 || w.MountRefs[0].TargetName != "nightly-script" {
		t.Errorf("expected one nightly-script mount, got %+v", w.MountRefs)
	}
	if len(w.SARefs) != 1 || w.SARefs[0].SAName != "nightly-sa" {
		t.Errorf("expected one nightly-sa SARef, got %+v", w.SARefs)
	}
}

func TestEnrichTrajectoryData_ProjectedVolume(t *testing.T) {
	w := types.NewWorld()
	w.Resources = append(w.Resources, types.ResourceInfo{
		APIVersion: "v1", Kind: "Pod", Name: "web", Namespace: "app",
		Source: types.SourceLocation{File: "pod.yaml", Line: 1},
		Raw: map[string]interface{}{
			"spec": map[string]interface{}{
				"volumes": []interface{}{
					map[string]interface{}{
						"name": "all-config",
						"projected": map[string]interface{}{
							"sources": []interface{}{
								map[string]interface{}{
									"configMap": map[string]interface{}{"name": "a"},
								},
								map[string]interface{}{
									"secret": map[string]interface{}{"name": "b"},
								},
							},
						},
					},
				},
			},
		},
	})
	EnrichTrajectoryData(w)

	if len(w.MountRefs) != 2 {
		t.Fatalf("expected 2 projected mount refs, got %d", len(w.MountRefs))
	}
	if w.MountRefs[0].MountKind != "projected" || w.MountRefs[1].MountKind != "projected" {
		t.Errorf("expected all mounts to be projected, got %+v", w.MountRefs)
	}
}

func TestEnrichTrajectoryData_RBACBinding(t *testing.T) {
	w := types.NewWorld()
	w.Resources = append(w.Resources, types.ResourceInfo{
		APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding",
		Name: "web-binding", Namespace: "app",
		Source: types.SourceLocation{File: "rbac.yaml", Line: 1},
		Raw: map[string]interface{}{
			"roleRef": map[string]interface{}{
				"kind": "Role",
				"name": "web-role",
			},
			"subjects": []interface{}{
				map[string]interface{}{
					"kind":      "ServiceAccount",
					"name":      "web-sa",
					"namespace": "app",
				},
				map[string]interface{}{
					"kind": "User",
					"name": "alice",
				},
			},
		},
	})
	EnrichTrajectoryData(w)

	if len(w.RBACBindings) != 2 {
		t.Fatalf("expected 2 bindings (one per subject), got %d", len(w.RBACBindings))
	}
	if w.RBACBindings[0].SubjectKind != "ServiceAccount" || w.RBACBindings[0].RoleName != "web-role" {
		t.Errorf("unexpected first binding: %+v", w.RBACBindings[0])
	}
}

func TestEnrichTrajectoryData_RBACRules(t *testing.T) {
	w := types.NewWorld()
	w.Resources = append(w.Resources, types.ResourceInfo{
		APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole",
		Name: "web-role",
		Source: types.SourceLocation{File: "rbac.yaml", Line: 1},
		Raw: map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"apiGroups": []interface{}{""},
					"resources": []interface{}{"configmaps", "secrets"},
					"verbs":     []interface{}{"get", "list"},
				},
			},
		},
	})
	EnrichTrajectoryData(w)

	if len(w.RBACRules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(w.RBACRules))
	}
	r := w.RBACRules[0]
	if r.OwnerKind != "ClusterRole" || len(r.Verbs) != 2 || r.Verbs[0] != "get" {
		t.Errorf("unexpected rule: %+v", r)
	}
}
