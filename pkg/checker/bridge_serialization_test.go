package checker

import (
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/trajectory"
	"github.com/pyrex41/cross-validate-/pkg/types"
	"github.com/tiancaiamao/shen-go/kl"
)

func TestBridge_TrajectorySerialization(t *testing.T) {
	w := types.NewWorld()
	w.ImmutableFields = []types.ImmutableField{{
		Group: "", Kind: "Service", FieldPath: "spec.clusterIP", Reason: "immutable",
	}}
	w.MountRefs = []types.MountRef{{
		OwnerKind: "Pod", OwnerName: "web", OwnerNamespace: "ns",
		TargetKind: "ConfigMap", TargetName: "cfg", TargetNamespace: "ns",
		MountKind: "volume", Optional: false,
		Source: types.SourceLocation{File: "pod.yaml", Line: 1},
	}}
	w.SARefs = []types.SARef{{
		OwnerKind: "Pod", OwnerName: "web", OwnerNamespace: "ns",
		SAName: "web-sa", SANamespace: "ns",
		Source: types.SourceLocation{File: "pod.yaml", Line: 1},
	}}
	w.RBACBindings = []types.RBACBinding{{
		BindingKind: "RoleBinding", BindingName: "rb", BindingNamespace: "ns",
		SubjectKind: "ServiceAccount", SubjectName: "web-sa", SubjectNamespace: "ns",
		RoleKind: "Role", RoleName: "r",
		Source: types.SourceLocation{File: "rbac.yaml", Line: 1},
	}}
	w.RBACRules = []types.RBACRule{{
		OwnerKind: "Role", OwnerName: "r", OwnerNamespace: "ns",
		APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get"},
		Source: types.SourceLocation{File: "rbac.yaml", Line: 1},
	}}

	steps := []trajectory.Step{{
		AppName: "app", Wave: 0,
		Delta: trajectory.Delta{
			Created: []trajectory.ResourceKey{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "ns", Name: "cfg"}},
			Deleted: []trajectory.ResourceKey{{APIVersion: "v1", Kind: "Secret", Namespace: "ns", Name: "old"}},
		},
		State: trajectory.State{Resources: map[trajectory.ResourceKey]types.ResourceInfo{
			{APIVersion: "v1", Kind: "ConfigMap", Namespace: "ns", Name: "cfg"}: {Name: "cfg"},
		}},
	}}

	obj := worldToShenObj(w, steps)
	ser := kl.ObjString(obj)

	for _, expected := range []string{
		"mount-refs", "mount-ref-fact",
		"sa-refs", "sa-ref-fact",
		"rbac-bindings", "rbac-binding-fact",
		"rbac-rules", "rbac-rule-fact",
		"immutable-fields", "immutable-field-fact",
		"trajectory", "step", "delta",
		"created", "deleted", "state",
		"resource-key",
	} {
		if !strings.Contains(ser, expected) {
			t.Errorf("serialized world missing expected token %q\nserialization: %s",
				expected, ser)
		}
	}
}
