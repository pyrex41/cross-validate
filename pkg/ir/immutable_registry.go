package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// ImmutableFieldRegistry returns the static catalog of immutable field paths.
// Expand by appending to this slice — one entry per (Group, Kind, FieldPath).
//
// Scope: scalar-leaf paths only. Array-indexed paths (e.g. `spec.xs[0].k`)
// and whole-object-block paths (e.g. `spec.forProvider.settings`) are NOT
// supported by this registry — the consumer (R27) uses ReadPath for single
// lookups against a raw map, which walks named keys and returns values
// verbatim. Composite / indexed immutability is a separate problem deferred
// to P5: a group-level diff-match rule would replace "one field at a time"
// with "any change anywhere under this prefix is breaking".
//
// Each entry carries a prose Reason used in the Detail string of the
// XPC.P.immutable-change diagnostic so the operator can see *why* the field
// is registered, not just *that* it is.
func ImmutableFieldRegistry() []types.ImmutableField {
	return []types.ImmutableField{
		// Core K8s.
		{Group: "", Kind: "Service", FieldPath: "spec.clusterIP",
			Reason: "Service ClusterIP is immutable after create; changing it requires recreate"},
		{Group: "", Kind: "Service", FieldPath: "spec.type",
			Reason: "Service type changes from/to ExternalName are not allowed in-place"},
		{Group: "", Kind: "PersistentVolumeClaim", FieldPath: "spec.storageClassName",
			Reason: "PVC StorageClassName is immutable after create"},
		{Group: "", Kind: "PersistentVolumeClaim", FieldPath: "spec.accessModes",
			Reason: "PVC AccessModes are immutable after create"},
		{Group: "batch", Kind: types.KindJob, FieldPath: "spec.selector",
			Reason: "Job Selector is immutable after create"},
		{Group: "batch", Kind: types.KindJob, FieldPath: "spec.template",
			Reason: "Job Template is immutable after create"},
		{Group: "apps", Kind: types.KindStatefulSet, FieldPath: "spec.serviceName",
			Reason: "StatefulSet ServiceName is immutable after create"},
		{Group: "apps", Kind: types.KindStatefulSet, FieldPath: "spec.volumeClaimTemplates",
			Reason: "StatefulSet VolumeClaimTemplates are immutable after create"},

		// Crossplane — RDS Aurora.
		{Group: "rds.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.engine",
			Reason: "RDS Cluster engine is immutable after create; changing it requires DeleteCluster + CreateCluster (data loss)"},
		{Group: "rds.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.engineMode",
			Reason: "RDS Cluster engineMode (provisioned/serverless) is immutable after create"},
		{Group: "rds.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.dbClusterIdentifier",
			Reason: "RDS Cluster dbClusterIdentifier is the external name; changing it detaches the CR from the live cluster"},
		{Group: "rds.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.masterUsername",
			Reason: "RDS Cluster masterUsername is immutable after create"},
		{Group: "rds.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.storageEncrypted",
			Reason: "RDS Cluster storageEncrypted is immutable after create; toggling requires full recreate"},
		{Group: "rds.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.kmsKeyId",
			Reason: "RDS Cluster kmsKeyId is immutable after create; re-keying requires snapshot + restore"},
		{Group: "rds.aws.upbound.io", Kind: "ClusterInstance", FieldPath: "spec.forProvider.clusterIdentifier",
			Reason: "RDS ClusterInstance clusterIdentifier pins the instance to its cluster; changing it is a destructive re-parent"},
		{Group: "rds.aws.upbound.io", Kind: "ClusterInstance", FieldPath: "spec.forProvider.engine",
			Reason: "RDS ClusterInstance engine must match its Cluster; changing it requires recreate"},

		// Crossplane — DocDB (mirrors RDS shape).
		{Group: "docdb.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.engine",
			Reason: "DocDB Cluster engine is immutable after create; changing it requires DeleteCluster + CreateCluster (data loss)"},
		{Group: "docdb.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.engineMode",
			Reason: "DocDB Cluster engineMode is immutable after create"},
		{Group: "docdb.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.dbClusterIdentifier",
			Reason: "DocDB Cluster dbClusterIdentifier is the external name; changing it detaches the CR from the live cluster"},
		{Group: "docdb.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.masterUsername",
			Reason: "DocDB Cluster masterUsername is immutable after create"},
		{Group: "docdb.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.storageEncrypted",
			Reason: "DocDB Cluster storageEncrypted is immutable after create; toggling requires full recreate"},
		{Group: "docdb.aws.upbound.io", Kind: "Cluster", FieldPath: "spec.forProvider.kmsKeyId",
			Reason: "DocDB Cluster kmsKeyId is immutable after create"},
		{Group: "docdb.aws.upbound.io", Kind: "ClusterInstance", FieldPath: "spec.forProvider.clusterIdentifier",
			Reason: "DocDB ClusterInstance clusterIdentifier pins the instance to its cluster; changing it is a destructive re-parent"},
		{Group: "docdb.aws.upbound.io", Kind: "ClusterInstance", FieldPath: "spec.forProvider.engine",
			Reason: "DocDB ClusterInstance engine must match its Cluster; changing it requires recreate"},

		// Crossplane — S3.
		{Group: "s3.aws.upbound.io", Kind: "Bucket", FieldPath: "spec.forProvider.bucket",
			Reason: "S3 Bucket name is globally unique and immutable after create; changing it means DeleteBucket + CreateBucket"},
		{Group: "s3.aws.upbound.io", Kind: "Bucket", FieldPath: "spec.forProvider.region",
			Reason: "S3 Bucket region is immutable after create; a region change is a destructive rehost"},
		{Group: "s3.aws.upbound.io", Kind: "Bucket", FieldPath: "spec.forProvider.objectLockEnabled",
			Reason: "S3 Bucket objectLockEnabled can only be set at create time; flipping it requires bucket recreate"},

		// Crossplane — KMS.
		{Group: "kms.aws.upbound.io", Kind: "Key", FieldPath: "spec.forProvider.keyUsage",
			Reason: "KMS Key keyUsage (ENCRYPT_DECRYPT vs SIGN_VERIFY) is immutable after create"},
		{Group: "kms.aws.upbound.io", Kind: "Key", FieldPath: "spec.forProvider.customerMasterKeySpec",
			Reason: "KMS Key customerMasterKeySpec is immutable after create; re-speccing requires a new key"},

		// Crossplane — EC2 VPC.
		{Group: "ec2.aws.upbound.io", Kind: "VPC", FieldPath: "spec.forProvider.cidrBlock",
			Reason: "VPC cidrBlock is the network identity and is immutable after create"},
		{Group: "ec2.aws.upbound.io", Kind: "VPC", FieldPath: "spec.forProvider.instanceTenancy",
			Reason: "VPC instanceTenancy is immutable after create (default vs dedicated)"},

		// Crossplane — mysql.sql.
		{Group: "mysql.sql.crossplane.io", Kind: "Database", FieldPath: "spec.forProvider.name",
			Reason: "mysql Database name is the external identity; changing it drops the database and creates a fresh empty one"},
	}
}
