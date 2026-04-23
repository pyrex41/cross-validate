// Package plan implements xpc's variant-diff capability. A Plan captures the
// resource-identity delta between two git refs (or worktree paths) of a repo
// containing xpc-checkable manifests, along with the static diagnostics from
// each side. Consumed by `xpc plan --base --head` and by R26 / R-future
// variant-aware rules.
//
// The substrate is files + process-local execution. No live cluster, no
// daemon. A Plan is produced by checking out two worktrees, running the
// existing `checker.Check` against each, and diffing the rendered World.
package plan

import "github.com/pyrex41/cross-validate-/pkg/types"

// ResourceIdentity is the primary key used to decide whether two
// ResourceInfos from the base and head variants refer to the same resource.
// The five-tuple disambiguates same-named resources across different
// Applications, which is the common case in multi-app repos.
type ResourceIdentity struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	// AppName is the Argo Application that declares this resource, if any.
	// Empty when the resource is not owned by an Application (top-level
	// manifests). Required to disambiguate two apps that happen to own a
	// same-named resource.
	AppName string
}

// ResourceChange records a single added/removed/modified entry in the
// delta. For Added, BaseSource is empty; for Removed, HeadSource is empty.
// For Modified, both are populated and refer to the same identity.
type ResourceChange struct {
	Identity ResourceIdentity
	// BaseSource is the source location of the resource on the base side.
	// Empty for Added (there is no base counterpart).
	BaseSource types.SourceLocation
	// HeadSource is the source location on the head side. Empty for Removed.
	HeadSource types.SourceLocation
	// BaseRaw / HeadRaw carry the parsed YAML maps for rules that need to
	// inspect specific fields on one side of the delta (e.g. R26 reads
	// `spec.deletionPolicy` on BaseRaw to decide whether a removal is
	// destructive). Empty when the corresponding side is absent.
	BaseRaw map[string]interface{}
	HeadRaw map[string]interface{}
}

// ResourceDelta is the full set-difference between two Worlds.
type ResourceDelta struct {
	Added    []ResourceChange
	Removed  []ResourceChange
	Modified []ResourceChange
}

// VariantResult captures everything xpc knows about one side of a plan.
type VariantResult struct {
	// Ref is the user-supplied base/head argument — a git ref, a worktree
	// path, or a directory. Recorded verbatim for reporting.
	Ref string
	// ResolvedDir is the actual filesystem directory xpc checked. For git
	// refs this is the temporary worktree; for directory arguments, the
	// same value as Ref.
	ResolvedDir string
	// World is the IR produced for this variant. Shared pointer — callers
	// should treat as read-only after Plan returns.
	World *types.World
	// Diagnostics are the per-tip check results for this variant.
	Diagnostics []types.Diagnostic
}

// Plan is the output of a two-ref run. Delta describes the resource-level
// difference; Diagnostics contains any plan-time diagnostics (R26 and the
// upcoming trajectory-aware rules), separate from the per-variant static
// diagnostics which live on Base/Head.
type Plan struct {
	Base        VariantResult
	Head        VariantResult
	Delta       ResourceDelta
	Diagnostics []types.Diagnostic
}
