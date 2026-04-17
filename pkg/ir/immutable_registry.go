package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// ImmutableFieldRegistry returns the static catalog of immutable field paths.
// Expand by appending to this slice — one entry per (Group, Kind, FieldPath).
func ImmutableFieldRegistry() []types.ImmutableField {
	return []types.ImmutableField{
		{Group: "", Kind: "Service", FieldPath: "spec.clusterIP",
			Reason: "Service ClusterIP is immutable after create; changing it requires recreate"},
		{Group: "", Kind: "Service", FieldPath: "spec.type",
			Reason: "Service type changes from/to ExternalName are not allowed in-place"},
		{Group: "", Kind: "PersistentVolumeClaim", FieldPath: "spec.storageClassName",
			Reason: "PVC StorageClassName is immutable after create"},
		{Group: "", Kind: "PersistentVolumeClaim", FieldPath: "spec.accessModes",
			Reason: "PVC AccessModes are immutable after create"},
		{Group: "batch", Kind: "Job", FieldPath: "spec.selector",
			Reason: "Job Selector is immutable after create"},
		{Group: "batch", Kind: "Job", FieldPath: "spec.template",
			Reason: "Job Template is immutable after create"},
		{Group: "apps", Kind: "StatefulSet", FieldPath: "spec.serviceName",
			Reason: "StatefulSet ServiceName is immutable after create"},
		{Group: "apps", Kind: "StatefulSet", FieldPath: "spec.volumeClaimTemplates",
			Reason: "StatefulSet VolumeClaimTemplates are immutable after create"},
	}
}
