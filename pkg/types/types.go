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
	Name             string           `json:"name"`
	CompositeTypeRef GVK              `json:"compositeTypeRef"`
	Mode             string           `json:"mode"` // Pipeline or Resources
	Pipeline         []PipelineStep   `json:"pipeline,omitempty"`
	Resources        []ComposedResource `json:"resources,omitempty"`
	Source           SourceLocation   `json:"source"`
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
	Name       string            `json:"name"`
	Base       ResourceBase      `json:"base"`
	Patches    []PatchInfo       `json:"patches,omitempty"`
}

// ResourceBase is the base resource in a composed resource.
type ResourceBase struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// PatchInfo represents a patch in a Composition.
type PatchInfo struct {
	Type          string `json:"type"` // FromCompositeFieldPath, ToCompositeFieldPath, etc.
	FromFieldPath string `json:"fromFieldPath,omitempty"`
	ToFieldPath   string `json:"toFieldPath,omitempty"`
	Transforms    []TransformInfo `json:"transforms,omitempty"`
}

// TransformInfo represents a transform in a patch.
type TransformInfo struct {
	Type    string `json:"type"` // convert, map, match, math, string
	Convert string `json:"convert,omitempty"` // target type for convert transforms
}

// FunctionInfo represents an installed Crossplane Function.
type FunctionInfo struct {
	Name          string   `json:"name"`
	Package       string   `json:"package"`
	InputVersions []string `json:"inputVersions"`
	Source        SourceLocation `json:"source"`
}

// ProviderInfo represents an installed Crossplane Provider.
type ProviderInfo struct {
	Name    string         `json:"name"`
	Package string         `json:"package"`
	Source  SourceLocation `json:"source"`
}

// ConfigurationInfo represents an installed Crossplane Configuration.
type ConfigurationInfo struct {
	Name    string         `json:"name"`
	Package string         `json:"package"`
	Source  SourceLocation `json:"source"`
}

// ResourceInfo represents a Kubernetes resource (XR, MR, or any other).
type ResourceInfo struct {
	APIVersion  string            `json:"apiVersion"`
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Source      SourceLocation    `json:"source"`
	Raw         map[string]interface{} `json:"-"`
}

// ArgoApplication represents a parsed Argo CD Application.
type ArgoApplication struct {
	Name         string              `json:"name"`
	Namespace    string              `json:"namespace,omitempty"`
	TrackingMode string              `json:"trackingMode"` // annotation or label
	SyncWaves    []SyncWaveEntry     `json:"syncWaves"`
	Source       SourceLocation      `json:"source"`
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

// World is the complete typed representation of a set of manifests.
type World struct {
	CRDs           []CRDInfo          `json:"crds"`
	XRDs           []CRDInfo          `json:"xrds"`
	Compositions   []CompositionInfo  `json:"compositions"`
	Functions      []FunctionInfo     `json:"functions"`
	Providers      []ProviderInfo     `json:"providers"`
	Configurations []ConfigurationInfo `json:"configurations"`
	Resources      []ResourceInfo     `json:"resources"`
	ArgoApps       []ArgoApplication  `json:"argoApps"`
	Schemas        map[string]SchemaInfo `json:"schemas"`
}

// NewWorld creates an empty World.
func NewWorld() *World {
	return &World{
		Schemas: make(map[string]SchemaInfo),
	}
}

// Diagnostic is a single error/warning/info produced by the checker.
type Diagnostic struct {
	Code     string         `json:"code"`     // XPC001, XPC002, ...
	Severity Severity       `json:"severity"`
	Message  string         `json:"message"`
	Source   SourceLocation `json:"source"`
	Detail   string         `json:"detail,omitempty"`
	Fix      string         `json:"fix,omitempty"`
	Related  []SourceLocation `json:"related,omitempty"`
}
