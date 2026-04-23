// Package types defines shared types used across the xpc tool.
package types

// CostClass classifies the cost of a CRD version conversion.
type CostClass string

const (
	CostClassNone       CostClass = "None"
	CostClassIdentity   CostClass = "Identity"
	CostClassStructural CostClass = "Structural"
	CostClassWebhook    CostClass = "Webhook"
)

// Severity indicates the severity of a diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Well-known Kubernetes kind strings used in switch dispatch and field values.
// Defined here as a single source of truth; Kind-typed fields remain plain string.
const (
	KindPod                         = "Pod"
	KindDeployment                  = "Deployment"
	KindStatefulSet                 = "StatefulSet"
	KindDaemonSet                   = "DaemonSet"
	KindReplicaSet                  = "ReplicaSet"
	KindJob                         = "Job"
	KindCronJob                     = "CronJob"
	KindConfigMap                   = "ConfigMap"
	KindSecret                      = "Secret"
	KindServiceAccount              = "ServiceAccount"
	KindRole                        = "Role"
	KindClusterRole                 = "ClusterRole"
	KindRoleBinding                 = "RoleBinding"
	KindClusterRoleBinding          = "ClusterRoleBinding"
	KindCustomResourceDefinition    = "CustomResourceDefinition"
	KindCompositeResourceDefinition = "CompositeResourceDefinition"
	KindComposition                 = "Composition"
	KindFunction                    = "Function"
	KindProvider                    = "Provider"
	KindConfiguration               = "Configuration"
	KindApplication                 = "Application"
)

// SourceLocation points back to the original file/line that produced a resource.
type SourceLocation struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// CRDVersion represents a single version entry in a CRD.
type CRDVersion struct {
	Name          string `json:"name"`
	Served        bool   `json:"served"`
	Storage       bool   `json:"storage"`
	Referenceable bool   `json:"referenceable,omitempty"` // XRD-specific
	SchemaDigest  string `json:"schemaDigest,omitempty"`
}

// ConversionInfo holds conversion strategy details for a CRD.
type ConversionInfo struct {
	Strategy       string    `json:"strategy"` // None, Webhook
	CostClass      CostClass `json:"costClass"`
	WebhookService string    `json:"webhookService,omitempty"`
}

// CRDInfo represents a parsed CRD or XRD.
type CRDInfo struct {
	Group      string         `json:"group"`
	Kind       string         `json:"kind"`
	Scope      string         `json:"scope"` // Namespaced or Cluster
	Versions   []CRDVersion   `json:"versions"`
	Conversion ConversionInfo `json:"conversion"`
	Source     SourceLocation `json:"source"`
	IsXRD      bool           `json:"isXRD"`
	APIVersion string         `json:"apiVersion,omitempty"` // for XRDs: apiextensions.crossplane.io/v1 or v2
	// OwningApp is the name of the Argo Application that manages this CRD/XRD,
	// determined by path-prefix match against an app's direct-manifest source.
	// Empty means unowned (shared/global CRD not claimed by any modeled
	// Application). Used by per-app rules to avoid cartesian blame across apps.
	OwningApp string `json:"owningApp,omitempty"`
}

// StorageVersion returns the storage version name, or "" if none.
func (c *CRDInfo) StorageVersion() string {
	for _, v := range c.Versions {
		if v.Storage {
			return v.Name
		}
	}
	return ""
}

// ServesVersion returns true if the given version is served.
func (c *CRDInfo) ServesVersion(version string) bool {
	for _, v := range c.Versions {
		if v.Name == version && v.Served {
			return true
		}
	}
	return false
}

// CompositionInfo represents a parsed Crossplane Composition.
type CompositionInfo struct {
	Name             string             `json:"name"`
	CompositeTypeRef GVK                `json:"compositeTypeRef"`
	Mode             string             `json:"mode"` // Pipeline or Resources
	Pipeline         []PipelineStep     `json:"pipeline,omitempty"`
	Resources        []ComposedResource `json:"resources,omitempty"`
	Source           SourceLocation     `json:"source"`
	// OwningApp — see CRDInfo.OwningApp.
	OwningApp string `json:"owningApp,omitempty"`
}

// GVK is a GroupVersionKind tuple.
type GVK struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// PipelineStep represents a step in a Composition pipeline.
type PipelineStep struct {
	Name            string `json:"name"`
	FunctionRef     string `json:"functionRef"`
	InputAPIVersion string `json:"inputAPIVersion,omitempty"`
	InputKind       string `json:"inputKind,omitempty"`
	InputDigest     string `json:"inputDigest,omitempty"`
}

// ComposedResource represents a resource in a legacy (Resources mode) Composition.
type ComposedResource struct {
	Name    string       `json:"name"`
	Base    ResourceBase `json:"base"`
	Patches []PatchInfo  `json:"patches,omitempty"`
}

// ResourceBase is the base resource in a composed resource.
type ResourceBase struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// PatchInfo represents a patch in a Composition.
type PatchInfo struct {
	Type          string          `json:"type"` // FromCompositeFieldPath, ToCompositeFieldPath, etc.
	FromFieldPath string          `json:"fromFieldPath,omitempty"`
	ToFieldPath   string          `json:"toFieldPath,omitempty"`
	Transforms    []TransformInfo `json:"transforms,omitempty"`
}

// TransformInfo represents a transform in a patch.
type TransformInfo struct {
	Type    string `json:"type"`              // convert, map, match, math, string
	Convert string `json:"convert,omitempty"` // target type for convert transforms
}

// FunctionInfo represents an installed Crossplane Function.
type FunctionInfo struct {
	Name          string         `json:"name"`
	Package       string         `json:"package"`
	InputVersions []string       `json:"inputVersions"`
	Source        SourceLocation `json:"source"`
	// OwningApp — see CRDInfo.OwningApp.
	OwningApp string `json:"owningApp,omitempty"`
}

// ProviderInfo represents an installed Crossplane Provider.
type ProviderInfo struct {
	Name        string            `json:"name"`
	Package     string            `json:"package"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Source      SourceLocation    `json:"source"`
	// OwningApp — see CRDInfo.OwningApp.
	OwningApp string `json:"owningApp,omitempty"`
}

// ConfigurationInfo represents an installed Crossplane Configuration.
type ConfigurationInfo struct {
	Name    string         `json:"name"`
	Package string         `json:"package"`
	Source  SourceLocation `json:"source"`
}

// ResourceInfo represents a Kubernetes resource (XR, MR, or any other).
type ResourceInfo struct {
	APIVersion  string                 `json:"apiVersion"`
	Kind        string                 `json:"kind"`
	Name        string                 `json:"name"`
	Namespace   string                 `json:"namespace,omitempty"`
	Annotations map[string]string      `json:"annotations,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
	Source      SourceLocation         `json:"source"`
	Raw         map[string]interface{} `json:"-"`
	// Provenance identifies how this resource entered the World.
	// "direct" (or empty) means it was read from a manifest file on disk.
	// "rendered:helm:<app-name>" means it was produced by rendering a Helm
	// source of that Argo Application. Other future renderers will follow
	// the same "rendered:<kind>:<app-name>" shape.
	Provenance string `json:"provenance,omitempty"`
	// OwningApp is the name of the Argo Application that manages this
	// resource, determined either by path-prefix match against an app's
	// direct-manifest source or by render provenance. Empty means unowned
	// (shared/global resource not claimed by any modeled Application).
	OwningApp string `json:"owningApp,omitempty"`
}

// ArgoApplication represents a parsed Argo CD Application.
type ArgoApplication struct {
	Name         string          `json:"name"`
	Namespace    string          `json:"namespace,omitempty"`
	TrackingMode string          `json:"trackingMode"` // annotation or label
	SyncWaves    []SyncWaveEntry `json:"syncWaves"`
	Source       SourceLocation  `json:"source"`

	// Project is the AppProject this Application belongs to (spec.project).
	// Empty string means "default".
	Project string `json:"project,omitempty"`

	// Sources is the list of sources for multi-source Applications.
	// For single-source Applications, this has one element.
	Sources []ArgoSource `json:"sources,omitempty"`

	// Destination is where this Application deploys to.
	Destination ArgoDestination `json:"destination"`

	// SyncPolicy describes automated sync behavior.
	SyncPolicy ArgoSyncPolicy `json:"syncPolicy"`

	// IgnoreDifferences lists fields to ignore during diff.
	IgnoreDifferences []ArgoIgnoreDiff `json:"ignoreDifferences,omitempty"`

	// Hooks are sync-hook annotations found on managed resources.
	// Populated during rendering or from manifest scan.
	Hooks []ArgoHook `json:"hooks,omitempty"`

	// Finalizers from metadata.finalizers. When this list contains
	// `resources-finalizer.argocd.argoproj.io`, ArgoCD cascades DELETE through
	// every resource the Application owns on Application removal. Combined
	// with spec.syncPolicy.preserveResourcesOnDeletion != true, this is the
	// fg-synapse INC-6 trigger at the Application level. Consumed by R26
	// (plan-level cascade-risk rule).
	Finalizers []string `json:"finalizers,omitempty"`

	// Annotations from metadata.annotations. Consumed by R26's bypass check
	// (xpc.io/allow-delete / policy.facilitygrid.io/allow-delete).
	Annotations map[string]string `json:"annotations,omitempty"`
}

// RendererKind identifies which renderer an Argo source uses.
type RendererKind string

const (
	RendererHelm      RendererKind = "helm"
	RendererKustomize RendererKind = "kustomize"
	RendererDirectory RendererKind = "directory"
	RendererPlugin    RendererKind = "plugin"
)

// ArgoSource represents a single source in an Argo CD Application.
// This is a tagged union: exactly one of Helm, Kustomize, Directory, or Plugin
// should be non-nil, indicated by Renderer.
type ArgoSource struct {
	// RepoURL is the git/Helm/OCI repository URL.
	RepoURL string `json:"repoURL"`
	// Path is the directory within the repo.
	Path string `json:"path,omitempty"`
	// TargetRevision is the git ref, chart version, or OCI tag.
	TargetRevision string `json:"targetRevision,omitempty"`
	// Chart is the Helm chart name (for Helm repo sources).
	Chart string `json:"chart,omitempty"`
	// Ref names this source for Argo multi-source "$<ref>/..." references
	// from sibling sources' Helm valueFiles. Empty on single-source Apps.
	Ref string `json:"ref,omitempty"`
	// Renderer identifies which renderer config is active.
	Renderer RendererKind `json:"renderer"`

	// Helm config (non-nil when Renderer == RendererHelm).
	Helm *ArgoHelmSource `json:"helm,omitempty"`
	// Kustomize config (non-nil when Renderer == RendererKustomize).
	Kustomize *ArgoKustomizeSource `json:"kustomize,omitempty"`
	// Directory config (non-nil when Renderer == RendererDirectory).
	Directory *ArgoDirectorySource `json:"directory,omitempty"`
	// Plugin config (non-nil when Renderer == RendererPlugin).
	Plugin *ArgoPluginSource `json:"plugin,omitempty"`
}

// ArgoHelmSource holds Helm-specific source configuration.
type ArgoHelmSource struct {
	// ValueFiles is a list of values files to use.
	ValueFiles []string `json:"valueFiles,omitempty"`
	// ValuesObject is inline values (from spec.source.helm.valuesObject).
	ValuesObject map[string]interface{} `json:"valuesObject,omitempty"`
	// Values is the raw values string (from spec.source.helm.values).
	Values string `json:"values,omitempty"`
	// ReleaseName overrides the Helm release name.
	ReleaseName string `json:"releaseName,omitempty"`
	// Parameters are individual Helm parameter overrides.
	Parameters []ArgoHelmParam `json:"parameters,omitempty"`
}

// ArgoHelmParam is a single Helm parameter override.
type ArgoHelmParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ArgoKustomizeSource holds Kustomize-specific source configuration.
type ArgoKustomizeSource struct {
	// NamePrefix adds a prefix to all resource names.
	NamePrefix string `json:"namePrefix,omitempty"`
	// NameSuffix adds a suffix to all resource names.
	NameSuffix string `json:"nameSuffix,omitempty"`
	// Images overrides container images.
	Images []string `json:"images,omitempty"`
	// CommonLabels to add to all resources.
	CommonLabels map[string]string `json:"commonLabels,omitempty"`
	// CommonAnnotations to add to all resources.
	CommonAnnotations map[string]string `json:"commonAnnotations,omitempty"`
}

// ArgoDirectorySource holds directory-specific source configuration.
type ArgoDirectorySource struct {
	// Recurse enables recursive directory scanning.
	Recurse bool `json:"recurse,omitempty"`
	// Exclude is a glob pattern to exclude files.
	Exclude string `json:"exclude,omitempty"`
	// Include is a glob pattern to include files.
	Include string `json:"include,omitempty"`
}

// ArgoPluginSource holds plugin-specific source configuration.
type ArgoPluginSource struct {
	// Name is the plugin name.
	Name string `json:"name"`
	// Env are environment variables passed to the plugin.
	Env []ArgoPluginEnv `json:"env,omitempty"`
}

// ArgoPluginEnv is an environment variable for a plugin.
type ArgoPluginEnv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ArgoDestination is where an Application deploys to.
type ArgoDestination struct {
	// Server is the cluster API server URL.
	Server string `json:"server,omitempty"`
	// Name is the cluster name (alternative to Server).
	Name string `json:"name,omitempty"`
	// Namespace is the target namespace.
	Namespace string `json:"namespace,omitempty"`
}

// ArgoSyncPolicy describes the sync policy for an Application.
type ArgoSyncPolicy struct {
	// Automated is non-nil if automated sync is enabled.
	Automated *ArgoAutomatedSync `json:"automated,omitempty"`
	// SyncOptions is the list of sync options as typed fields.
	SyncOptions ArgoSyncOptions `json:"syncOptions"`
	// Retry configures sync retry behavior.
	Retry *ArgoRetryPolicy `json:"retry,omitempty"`
	// PreserveResourcesOnDeletion, when true on an Application's
	// spec.syncPolicy, tells ArgoCD NOT to cascade-delete owned resources
	// when the Application is removed. Pairs with the Finalizers field on
	// ArgoApplication for R26's cascade-risk computation. Independent of
	// the AppSet-level field of the same name (which lives on
	// ArgoAppSetSyncPolicy and gates R24).
	PreserveResourcesOnDeletion bool `json:"preserveResourcesOnDeletion,omitempty"`
}

// ArgoAutomatedSync describes automated sync settings.
type ArgoAutomatedSync struct {
	Prune    bool `json:"prune"`
	SelfHeal bool `json:"selfHeal"`
}

// ArgoSyncOptions holds every sync option as a typed field.
// Each field corresponds to a sync option string like "Replace=true".
type ArgoSyncOptions struct {
	// Replace deletes-then-creates instead of patching.
	Replace bool `json:"replace,omitempty"`
	// ServerSideApply uses server-side apply instead of client-side.
	ServerSideApply bool `json:"serverSideApply,omitempty"`
	// Prune allows deletion of resources absent from source.
	Prune bool `json:"prune,omitempty"`
	// PruneLast orders prunes after all other operations.
	PruneLast bool `json:"pruneLast,omitempty"`
	// CreateNamespace creates the target namespace if missing.
	CreateNamespace bool `json:"createNamespace,omitempty"`
	// ApplyOutOfSyncOnly skips resources whose live state matches.
	ApplyOutOfSyncOnly bool `json:"applyOutOfSyncOnly,omitempty"`
	// Validate enables/disables kubectl validation.
	Validate bool `json:"validate,omitempty"`
	// FailOnSharedResource fails sync if another app manages the same resource.
	FailOnSharedResource bool `json:"failOnSharedResource,omitempty"`
	// RespectIgnoreDifferences uses ignoreDifferences during sync.
	RespectIgnoreDifferences bool `json:"respectIgnoreDifferences,omitempty"`
}

// ArgoRetryPolicy describes sync retry behavior.
type ArgoRetryPolicy struct {
	Limit                 int `json:"limit,omitempty"`
	BackoffDurationSec    int `json:"backoffDurationSec,omitempty"`
	BackoffMaxDurationSec int `json:"backoffMaxDurationSec,omitempty"`
	BackoffFactor         int `json:"backoffFactor,omitempty"`
}

// ArgoIgnoreDiff describes a field to ignore during Argo CD diff.
type ArgoIgnoreDiff struct {
	// Group is the API group (empty matches all).
	Group string `json:"group,omitempty"`
	// Kind is the resource kind (empty matches all).
	Kind string `json:"kind,omitempty"`
	// Name is the resource name (empty matches all).
	Name string `json:"name,omitempty"`
	// Namespace is the resource namespace (empty matches all).
	Namespace string `json:"namespace,omitempty"`
	// JSONPointers are JSON pointer paths to ignore.
	JSONPointers []string `json:"jsonPointers,omitempty"`
	// JQPathExpressions are JQ expressions for fields to ignore.
	JQPathExpressions []string `json:"jqPathExpressions,omitempty"`
	// ManagedFieldsManagers are field managers whose fields to ignore.
	ManagedFieldsManagers []string `json:"managedFieldsManagers,omitempty"`
}

// ArgoHook represents a sync hook annotation on a resource.
type ArgoHook struct {
	// Phase is the hook phase (PreSync, Sync, PostSync, SyncFail, PostDelete).
	Phase string `json:"phase"`
	// DeletePolicy is the hook delete policy (HookSucceeded, HookFailed, BeforeHookCreation).
	DeletePolicy string `json:"deletePolicy,omitempty"`
	// Resource identifies the hook resource.
	Resource ResourceRef `json:"resource"`
	// Wave is the hook's sync wave.
	Wave int `json:"wave"`
}

// ResourceRef is a lightweight reference to a resource by GVK + name + namespace.
type ResourceRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
}

// --- AppProject ---

// ArgoAppProject represents an Argo CD AppProject.
type ArgoAppProject struct {
	Name   string         `json:"name"`
	Source SourceLocation `json:"source"`

	// SourceRepos is the allow-list of repository URLs.
	// Supports wildcards ("*" matches all).
	SourceRepos []string `json:"sourceRepos,omitempty"`

	// Destinations is the allow-list of (server, namespace) pairs.
	Destinations []ArgoProjectDestination `json:"destinations,omitempty"`

	// ClusterResourceWhitelist allows specific cluster-scoped kinds.
	ClusterResourceWhitelist []ArgoGroupKind `json:"clusterResourceWhitelist,omitempty"`
	// ClusterResourceBlacklist denies specific cluster-scoped kinds.
	ClusterResourceBlacklist []ArgoGroupKind `json:"clusterResourceBlacklist,omitempty"`
	// NamespaceResourceWhitelist allows specific namespace-scoped kinds.
	NamespaceResourceWhitelist []ArgoGroupKind `json:"namespaceResourceWhitelist,omitempty"`
	// NamespaceResourceBlacklist denies specific namespace-scoped kinds.
	NamespaceResourceBlacklist []ArgoGroupKind `json:"namespaceResourceBlacklist,omitempty"`

	// SyncWindows restricts when syncs are permitted.
	SyncWindows []ArgoSyncWindow `json:"syncWindows,omitempty"`

	// SignatureKeys are required GPG key IDs for commit verification.
	SignatureKeys []string `json:"signatureKeys,omitempty"`
}

// ArgoProjectDestination is an allowed (server, namespace) pair in an AppProject.
type ArgoProjectDestination struct {
	Server    string `json:"server,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// ArgoGroupKind identifies a resource group + kind for project whitelists.
type ArgoGroupKind struct {
	Group string `json:"group"`
	Kind  string `json:"kind"`
}

// ArgoSyncWindow restricts sync timing.
type ArgoSyncWindow struct {
	// Kind is "allow" or "deny".
	Kind string `json:"kind"`
	// Schedule is a cron expression.
	Schedule string `json:"schedule"`
	// Duration is the window duration (e.g., "1h", "30m").
	Duration string `json:"duration"`
	// Applications is the list of Application patterns this window applies to.
	Applications []string `json:"applications,omitempty"`
	// Namespaces is the list of namespace patterns.
	Namespaces []string `json:"namespaces,omitempty"`
	// Clusters is the list of cluster patterns.
	Clusters []string `json:"clusters,omitempty"`
}

// --- ApplicationSet ---

// ArgoApplicationSet represents an Argo CD ApplicationSet.
type ArgoApplicationSet struct {
	Name   string         `json:"name"`
	Source SourceLocation `json:"source"`

	// Generators produce the parameter sets for templating.
	Generators []ArgoAppSetGenerator `json:"generators"`

	// Template is the Application template to instantiate per generator element.
	Template ArgoAppSetTemplate `json:"template"`
}

// ArgoAppSetGeneratorKind identifies the type of ApplicationSet generator.
type ArgoAppSetGeneratorKind string

const (
	AppSetGenList        ArgoAppSetGeneratorKind = "list"
	AppSetGenCluster     ArgoAppSetGeneratorKind = "cluster"
	AppSetGenGit         ArgoAppSetGeneratorKind = "git"
	AppSetGenMatrix      ArgoAppSetGeneratorKind = "matrix"
	AppSetGenMerge       ArgoAppSetGeneratorKind = "merge"
	AppSetGenSCMProvider ArgoAppSetGeneratorKind = "scmProvider"
	AppSetGenPullRequest ArgoAppSetGeneratorKind = "pullRequest"
)

// ArgoAppSetGenerator is one generator in an ApplicationSet.
// This is a tagged union: Kind identifies which config fields are populated.
type ArgoAppSetGenerator struct {
	// Kind identifies the generator type.
	Kind ArgoAppSetGeneratorKind `json:"kind"`

	// List generator elements (when Kind == "list").
	ListElements []map[string]string `json:"listElements,omitempty"`

	// Cluster generator selector (when Kind == "cluster").
	ClusterSelector map[string]string `json:"clusterSelector,omitempty"`

	// Git generator config (when Kind == "git").
	Git *ArgoAppSetGitGenerator `json:"git,omitempty"`

	// Matrix sub-generators (when Kind == "matrix").
	MatrixGenerators []ArgoAppSetGenerator `json:"matrixGenerators,omitempty"`

	// Merge sub-generators (when Kind == "merge").
	MergeGenerators []ArgoAppSetGenerator `json:"mergeGenerators,omitempty"`
	MergeKeys       []string              `json:"mergeKeys,omitempty"`
}

// ArgoAppSetGitGenerator configures a git-based ApplicationSet generator.
type ArgoAppSetGitGenerator struct {
	RepoURL  string `json:"repoURL"`
	Revision string `json:"revision,omitempty"`
	// Directories to scan for apps.
	Directories []ArgoAppSetGitDir `json:"directories,omitempty"`
	// Files to scan for parameters.
	Files []ArgoAppSetGitFile `json:"files,omitempty"`
}

// ArgoAppSetGitDir is a directory entry in a git generator.
type ArgoAppSetGitDir struct {
	Path    string `json:"path"`
	Exclude bool   `json:"exclude,omitempty"`
}

// ArgoAppSetGitFile is a file entry in a git generator.
type ArgoAppSetGitFile struct {
	Path string `json:"path"`
}

// ArgoAppSetTemplate is the Application template in an ApplicationSet.
type ArgoAppSetTemplate struct {
	// Name is the template for the Application name (may contain {{parameters}}).
	Name string `json:"name,omitempty"`
	// Namespace for the generated Application.
	Namespace string `json:"namespace,omitempty"`
	// Project for the generated Application.
	Project string `json:"project,omitempty"`
	// Source template.
	Source *ArgoSource `json:"source,omitempty"`
	// Sources for multi-source templates.
	Sources []ArgoSource `json:"sources,omitempty"`
	// Destination template.
	Destination ArgoDestination `json:"destination"`
	// SyncPolicy template.
	SyncPolicy ArgoSyncPolicy `json:"syncPolicy"`
}

// SyncWaveEntry represents a resource's sync wave assignment.
type SyncWaveEntry struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Wave int    `json:"wave"`
}

// SchemaInfo holds a content-addressed OpenAPI schema fragment.
type SchemaInfo struct {
	Digest string                 `json:"digest"`
	Schema map[string]interface{} `json:"schema"`
}

// MountRef records that a workload resource (Pod-bearing kind) mounts a
// ConfigMap or Secret as a volume, projected volume, or envFrom.
type MountRef struct {
	OwnerKind       string         `json:"ownerKind"`
	OwnerName       string         `json:"ownerName"`
	OwnerNamespace  string         `json:"ownerNamespace,omitempty"`
	TargetKind      string         `json:"targetKind"` // ConfigMap | Secret
	TargetName      string         `json:"targetName"`
	TargetNamespace string         `json:"targetNamespace,omitempty"`
	MountKind       string         `json:"mountKind"` // volume | envFrom | projected
	Optional        bool           `json:"optional,omitempty"`
	Source          SourceLocation `json:"source"`
}

// SARef records that a workload resource pins to a ServiceAccount.
type SARef struct {
	OwnerKind      string         `json:"ownerKind"`
	OwnerName      string         `json:"ownerName"`
	OwnerNamespace string         `json:"ownerNamespace,omitempty"`
	SAName         string         `json:"saName"`
	SANamespace    string         `json:"saNamespace,omitempty"`
	Source         SourceLocation `json:"source"`
}

// RBACBinding records a (Cluster)RoleBinding subject → role edge.
type RBACBinding struct {
	BindingKind      string         `json:"bindingKind"` // RoleBinding | ClusterRoleBinding
	BindingName      string         `json:"bindingName"`
	BindingNamespace string         `json:"bindingNamespace,omitempty"`
	SubjectKind      string         `json:"subjectKind"` // ServiceAccount | User | Group
	SubjectName      string         `json:"subjectName"`
	SubjectNamespace string         `json:"subjectNamespace,omitempty"`
	RoleKind         string         `json:"roleKind"` // Role | ClusterRole
	RoleName         string         `json:"roleName"`
	Source           SourceLocation `json:"source"`
}

// RBACRule is a single (verbs, resources, apiGroups) entry inside a Role / ClusterRole.
type RBACRule struct {
	OwnerKind      string         `json:"ownerKind"` // Role | ClusterRole
	OwnerName      string         `json:"ownerName"`
	OwnerNamespace string         `json:"ownerNamespace,omitempty"`
	APIGroups      []string       `json:"apiGroups,omitempty"`
	Resources      []string       `json:"resources,omitempty"`
	Verbs          []string       `json:"verbs,omitempty"`
	ResourceNames  []string       `json:"resourceNames,omitempty"`
	Source         SourceLocation `json:"source"`
}

// ImmutableField is one entry in the registry of "field paths that must not change
// after create" per (Group, Kind). Populated from a static table; not extracted from YAML.
type ImmutableField struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	FieldPath string `json:"fieldPath"` // dotted path, e.g. "spec.clusterIP"
	Reason    string `json:"reason"`
}

// ViolationKind classifies a single resource-field validation failure.
type ViolationKind string

const (
	ViolationUnknownField    ViolationKind = "UnknownField"
	ViolationWrongType       ViolationKind = "WrongType"
	ViolationMissingRequired ViolationKind = "MissingRequired"
	ViolationInvalidEnum     ViolationKind = "InvalidEnum"
)

// ResourceFieldFact records one schema-validation violation found while walking
// a concrete manifest against its CRD/XRD schema. Emitted by
// schemas.ValidateManifest and consumed by Shen rule R17
// (XPC.A.resource-field-valid).
type ResourceFieldFact struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Namespace  string         `json:"namespace,omitempty"`
	Name       string         `json:"name"`
	Path       string         `json:"path"` // dotted, array-indexed path e.g. "spec.tags[0]"
	Violation  ViolationKind  `json:"violation"`
	Message    string         `json:"message"`
	Source     SourceLocation `json:"source"`
}

// ValuesIssue records one violation produced by validating a Helm chart's
// merged values against its values.schema.json. Produced by
// renderer.ValidateValues; consumed by Shen rule R19
// (XPC.H.values-well-typed).
type ValuesIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// RenderResult records the outcome of rendering one Argo Application source
// (currently Helm; Kustomize joins in S5). One entry per source attempted,
// whether or not rendering succeeded. Consumed by Shen rules R18
// (XPC.H.helm-renders) and R19 (XPC.H.values-well-typed).
type RenderResult struct {
	// AppName is the Argo Application whose source was rendered.
	AppName string `json:"appName"`
	// ChartPath is the resolved filesystem path that was rendered.
	ChartPath string `json:"chartPath"`
	// Success is true when the renderer produced output without error.
	// A schema-violation in values does NOT flip Success to false — the
	// render may still succeed even if values.schema.json rejects the
	// merged values.
	Success bool `json:"success"`
	// Error is the error string (renderer stderr + exit code, or
	// "helm binary absent" / "timed out"). Empty when Success is true.
	Error string `json:"error,omitempty"`
	// ErrorKind is a machine-readable classifier for Error: one of
	// "helm-absent", "helm-template-failed", "helm-timeout", "other".
	// Lowercase-dashed so Shen patterns can match on it directly
	// (uppercase identifiers are Shen variables).
	ErrorKind string `json:"errorKind,omitempty"`
	// ValuesIssues is the list of values.schema.json violations detected
	// when merging the chart's effective values. May be non-empty even
	// when Success is true.
	ValuesIssues []ValuesIssue `json:"valuesIssues,omitempty"`
	// Source points at the Argo Application file+line that declared the
	// rendered source, so diagnostics point somewhere an MR author can
	// edit.
	Source SourceLocation `json:"source"`
}

// SelectorMapping is one entry in the registry of Crossplane selector fields and the
// concrete resolved paths they populate via late-init. Populated from a static table.
// SelectorPath is the dotted path of the *Selector field in the manifest;
// ResolvedPath is the sibling path Crossplane writes after selector resolution.
// Array-indexed paths use "[]" as a placeholder element, e.g.
// "spec.forProvider.launchTemplate[].idSelector".
type SelectorMapping struct {
	Group        string `json:"group"`
	Kind         string `json:"kind"`
	SelectorPath string `json:"selectorPath"`
	ResolvedPath string `json:"resolvedPath"`
	Reason       string `json:"reason"`
}

// SelectorUsage records that a specific resource has a selector field set,
// along with the resolved path that Crossplane will late-init — and therefore
// that Argo CD will see as a drift unless suppressed via ignoreDifferences.
type SelectorUsage struct {
	// ResourceGroup and ResourceKind identify the resource type.
	ResourceGroup string `json:"resourceGroup"`
	ResourceKind  string `json:"resourceKind"`
	// ResourceName is the metadata.name of the resource.
	ResourceName string `json:"resourceName"`
	// ResourceNamespace is the metadata.namespace (empty for cluster-scoped).
	ResourceNamespace string `json:"resourceNamespace,omitempty"`
	// SelectorPath is the dotted field path of the *Selector field that is set.
	SelectorPath string `json:"selectorPath"`
	// ResolvedPath is the sibling path Crossplane will populate after resolution.
	ResolvedPath string `json:"resolvedPath"`
	// Source points to the manifest file containing this resource.
	Source SourceLocation `json:"source"`
}

// LateInitMapping is one entry in the registry of Crossplane provider fields
// that get late-initialized from observed cloud state (a.k.a. AWS/GCP fills
// them in after Create, and upjet writes them back into spec.forProvider).
// ArgoCD sees the write as drift from the git-declared manifest and shows the
// Application OutOfSync forever unless the ApplicationSet declares an
// ignoreDifferences entry covering the field.
//
// FieldPath is the dotted path of the field that drifts (under the resource,
// typically under spec.forProvider.*). Array-indexed segments use "[]" as a
// placeholder, mirroring SelectorMapping.
//
// FixPattern is metadata for the diagnostic message: "ignoreDifferences",
// "managementPolicies-observe", or "omit-late-initialize". R21 suggests all
// three patterns regardless of which specific MR taught us about this row.
type LateInitMapping struct {
	Group      string `json:"group"`
	Kind       string `json:"kind"`
	FieldPath  string `json:"fieldPath"`
	FixPattern string `json:"fixPattern"`
	Reason     string `json:"reason"`
}

// LateInitUsage records that a specific resource declares a value at a
// late-init field path — and therefore that Argo CD will see drift unless
// suppressed via ignoreDifferences. Populated by extractLateInitUsages by
// joining LateInitMappings against live Resources.
type LateInitUsage struct {
	ResourceGroup     string         `json:"resourceGroup"`
	ResourceKind      string         `json:"resourceKind"`
	ResourceName      string         `json:"resourceName"`
	ResourceNamespace string         `json:"resourceNamespace,omitempty"`
	FieldPath         string         `json:"fieldPath"`
	Source            SourceLocation `json:"source"`
}

// SSAMPConflict records a potential interaction between Argo CD's
// ServerSideApply sync option and a managed resource's managementPolicies
// declaration. R22 (XPC.E.ssa-managementpolicies-*) joins these per
// OwningApp + World.SSAMPMode to decide which of the three sub-codes
// (-observe, -partial, -nondefault) fires.
//
// The Go layer emits one SSAMPConflict per (SSA+managementPolicies) pair
// unconditionally — the Shen rule decides the emission path based on the
// mode fact. That keeps this struct a pure data carrier.
type SSAMPConflict struct {
	// AppName is the owning Argo CD Application for ResourceKind / ResourceName.
	// Populated from ResourceInfo.OwningApp. Empty when the resource is
	// unowned, in which case R22 skips the pairing.
	AppName string `json:"appName"`
	// ServerSideApply mirrors the owning Application's
	// syncPolicy.syncOptions.ServerSideApply at extraction time.
	ServerSideApply bool `json:"serverSideApply"`
	// ManagementPolicies is the list under spec.managementPolicies on the
	// resource. Empty slice means "unset" (Crossplane default = all
	// policies active); a non-nil empty/explicit default slice preserves
	// the author's intent so the kernel can distinguish "default" from
	// "explicit narrow-down".
	ManagementPolicies []string `json:"managementPolicies"`
	// ResourceGroup/ResourceKind/ResourceName/ResourceNamespace identify
	// the managed resource that carries the managementPolicies decl.
	ResourceGroup     string `json:"resourceGroup"`
	ResourceKind      string `json:"resourceKind"`
	ResourceName      string `json:"resourceName"`
	ResourceNamespace string `json:"resourceNamespace,omitempty"`
	// Source points at the resource manifest — where the fix (narrowing
	// managementPolicies, or removing ServerSideApply from the app) needs
	// to be applied.
	Source SourceLocation `json:"source"`
}

// IgnoreDiffEntry is a flattened view of one ignoreDifferences entry on one
// Argo CD Application, expanded to one row per JSONPointer or JQPathExpression.
// When both pointer lists are empty, a single row is emitted with empty strings
// so the kernel can still inspect the group/kind scope.
type IgnoreDiffEntry struct {
	// AppName is the Argo CD Application name.
	AppName string `json:"appName"`
	// Group and Kind narrow which resources this entry applies to.
	Group string `json:"group"`
	Kind  string `json:"kind"`
	// JSONPointer is one element from ignoreDifferences[].jsonPointers.
	JSONPointer string `json:"jsonPointer"`
	// JQPath is one element from ignoreDifferences[].jqPathExpressions.
	JQPath string `json:"jqPath"`
}

// World is the complete typed representation of a set of manifests.
type World struct {
	CRDs           []CRDInfo             `json:"crds"`
	XRDs           []CRDInfo             `json:"xrds"`
	Compositions   []CompositionInfo     `json:"compositions"`
	Functions      []FunctionInfo        `json:"functions"`
	Providers      []ProviderInfo        `json:"providers"`
	Configurations []ConfigurationInfo   `json:"configurations"`
	Resources      []ResourceInfo        `json:"resources"`
	ArgoApps       []ArgoApplication     `json:"argoApps"`
	ArgoProjects   []ArgoAppProject      `json:"argoProjects,omitempty"`
	ArgoAppSets    []ArgoApplicationSet  `json:"argoAppSets,omitempty"`
	Schemas        map[string]SchemaInfo `json:"schemas"`

	MountRefs       []MountRef       `json:"mountRefs,omitempty"`
	SARefs          []SARef          `json:"saRefs,omitempty"`
	RBACBindings    []RBACBinding    `json:"rbacBindings,omitempty"`
	RBACRules       []RBACRule       `json:"rbacRules,omitempty"`
	ImmutableFields []ImmutableField `json:"-"`

	// SelectorMappings is the static registry of Crossplane selector → resolved-path pairs.
	// Populated from the static table in pkg/ir/selector_registry.go; not extracted from YAML.
	SelectorMappings []SelectorMapping `json:"-"`

	// SelectorUsages records every resource that has a selector field set,
	// cross-referenced with the resolved path from SelectorMappings.
	// Populated by EnrichTrajectoryData.
	SelectorUsages []SelectorUsage `json:"-"`

	// LateInitMappings is the static registry of Crossplane fields that
	// providers late-initialize from observed cloud state. Populated from
	// pkg/ir/late_init_registry.go; not extracted from YAML.
	LateInitMappings []LateInitMapping `json:"-"`

	// LateInitUsages records every resource that declares a value at a
	// late-init field path. Populated by EnrichTrajectoryData.
	LateInitUsages []LateInitUsage `json:"-"`

	// ResourceFieldFacts records every schema-validation violation detected
	// while walking a concrete manifest against its CRD/XRD schema. Populated
	// by EnrichFieldValidation via schemas.ValidateManifest and consumed by
	// Shen rule R17 (XPC.A.resource-field-valid).
	ResourceFieldFacts []ResourceFieldFact `json:"-"`

	// RenderResults records the outcome of every Helm (and eventually
	// Kustomize) render attempt. Populated by the IR builder when
	// Builder.SkipRender is false. Consumed by Shen rules R18
	// (XPC.H.helm-renders) and R19 (XPC.H.values-well-typed).
	RenderResults []RenderResult `json:"-"`

	// DeterminismResults records the outcome of double-rendering every
	// renderable source. Populated by pkg/renderer/determinism.go and
	// consumed by Shen rule R20 (XPC.H.render-deterministic).
	DeterminismResults []DeterminismResult `json:"-"`

	// SSAMPConflicts records every (SSA+managementPolicies) pair extracted
	// from managed resources whose owning Application sets
	// syncPolicy.syncOptions.ServerSideApply. Populated by
	// extractSSAMPConflicts and consumed by Shen rule R22 (the
	// XPC.E.ssa-managementpolicies-* family). One row per resource; the
	// kernel picks the sub-code based on SSAMPMode + ManagementPolicies.
	SSAMPConflicts []SSAMPConflict `json:"-"`

	// SSAMPMode is the user-configurable gating mode for R22. One of
	// "observe" (default), "partial", or "any" — progressively broader
	// strictness. Plumbed from --ssa-mp-mode on the CLI through
	// Builder.SSAMPMode; emitted as a single-symbol section on the Shen
	// world so R22 can branch.
	SSAMPMode string `json:"-"`
}

// DeterminismResult records whether rendering the same source twice in a row
// yields byte-identical output. A mismatch is a signal that the renderer's
// inputs are under-specified (e.g. a chart uses `randAlphaNum` or references
// a clock). It is surfaced as a warning, not an error — some non-determinism
// is legitimate and simply wants documenting.
type DeterminismResult struct {
	// AppName is the Argo Application whose source was rendered.
	AppName string `json:"appName"`
	// RendererKind is the renderer this result came from: "helm" or
	// "kustomize". Lowercase so it can go straight into a Shen pattern.
	RendererKind string `json:"rendererKind"`
	// Mismatch is true when the two renders produced different bytes.
	Mismatch bool `json:"mismatch"`
	// DiffSummary is a short, human-readable summary of the first divergence
	// (e.g. "outputs differ: 2048 vs 2055 bytes"). Empty when Mismatch is
	// false.
	DiffSummary string `json:"diffSummary,omitempty"`
	// Source points at the Argo Application file+line.
	Source SourceLocation `json:"source"`
}

// NewWorld creates an empty World.
func NewWorld() *World {
	return &World{
		Schemas: make(map[string]SchemaInfo),
	}
}

// ObligationRef links a diagnostic back to the obligation that produced it.
// This is the provenance trail from the bounded obligation taxonomy.
type ObligationRef struct {
	// ID is the structured obligation ID (e.g., "XPC.B.comp-xrd-ref.billing-api").
	ID string `json:"id"`
	// Category is the obligation category letter (A-L).
	Category string `json:"category"`
	// Generator is the generator name that produced this obligation.
	Generator string `json:"generator"`
}

// Diagnostic is a single error/warning/info produced by the checker.
type Diagnostic struct {
	Code     string           `json:"code"` // XPC001, XPC002, ...
	Severity Severity         `json:"severity"`
	Message  string           `json:"message"`
	Source   SourceLocation   `json:"source"`
	Detail   string           `json:"detail,omitempty"`
	Fix      string           `json:"fix,omitempty"`
	Related  []SourceLocation `json:"related,omitempty"`
	// Obligation links this diagnostic to its obligation provenance.
	// Nil for legacy rule diagnostics not yet ported to the obligation framework.
	Obligation *ObligationRef `json:"obligation,omitempty"`
}
