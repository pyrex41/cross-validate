// Package obligation implements the bounded obligation taxonomy for xpc.
//
// Every check in xpc is an obligation: a claim about the input that must be
// discharged (proven or falsified). Obligations are produced by generators,
// organized into 12 categories (see docs/obligations.md), and discharged by
// the runner.
//
// This package replaces the ad-hoc rule functions in pkg/checker/rules.go
// with a structured, provenance-tracked framework.
package obligation

import (
	"crypto/sha256"
	"fmt"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Category identifies one of the 12 obligation categories.
type Category string

const (
	CatSchema          Category = "A" // Schema obligations
	CatReference       Category = "B" // Reference-resolution obligations
	CatVersionCoherence Category = "C" // Version-coherence obligations
	CatAppProject      Category = "D" // AppProject-constraint obligations
	CatSyncOption      Category = "E" // Sync-option interaction obligations
	CatTrajectory      Category = "F" // Trajectory-invariant obligations
	CatCrossApp        Category = "G" // Cross-Application obligations
	CatRendering       Category = "H" // Rendering obligations
	CatProvider        Category = "I" // Provider-capability obligations
	CatConversionCost  Category = "J" // Conversion-cost obligations
	CatSecretFlow      Category = "K" // Secret-flow obligations
	CatDeprecation     Category = "L" // Deprecation/calendar obligations
)

// Status is the result of discharging an obligation.
type Status string

const (
	Satisfied Status = "satisfied" // Obligation holds; no issue.
	Violated  Status = "violated"  // Obligation fails; diagnostic emitted.
	Unknown   Status = "unknown"   // Cannot determine (missing data).
)

// Context carries cluster-level information needed by generators.
// During Phase 0 this wraps the World; it will grow to include snapshot
// data, installed CRDs, and controller versions.
type Context struct {
	World *types.World
	// StrictConversions refuses webhook conversions entirely.
	StrictConversions bool
}

// Provenance records where an obligation came from.
type Provenance struct {
	// Generator is the name of the generator that produced this obligation.
	Generator string `json:"generator"`
	// Category is the obligation category.
	Category Category `json:"category"`
	// InputHash is a content hash of the input that triggered this obligation.
	InputHash string `json:"inputHash,omitempty"`
}

// ObligationRef is an alias for types.ObligationRef, which is the minimal
// reference stored on a Diagnostic to link it back to its obligation.

// Obligation is a single claim about the input that must be discharged.
type Obligation struct {
	// ID is the structured obligation ID.
	// Format: XPC.<Category>.<Generator>.<Instance>
	ID string

	// Category is the obligation category.
	Category Category

	// Subject is the resource this obligation is about.
	Subject types.SourceLocation

	// Claim is a human-readable statement of what must hold.
	Claim string

	// Provenance records where this obligation came from.
	Provenance Provenance

	// LegacyCode is the XPC001-XPC011 alias, if this obligation absorbs
	// a legacy rule. Empty for new obligations.
	LegacyCode string

	// Discharge is the function that proves or falsifies this obligation.
	// It returns the result and an optional diagnostic (populated on Violated).
	Discharge func(ctx *Context) Result
}

// Result is the outcome of discharging an obligation.
type Result struct {
	// Status indicates whether the obligation was satisfied or violated.
	Status Status
	// Diag is populated when Status is Violated.
	Diag *types.Diagnostic
}

// Ref returns the ObligationRef for this obligation.
func (o *Obligation) Ref() types.ObligationRef {
	return types.ObligationRef{
		ID:        o.ID,
		Category:  string(o.Category),
		Generator: o.Provenance.Generator,
	}
}

// Generator produces obligations for a specific category.
// Each generator is deterministic: same (Context, World) -> same obligations.
type Generator interface {
	// Name returns the generator's short name (e.g., "comp-xrd-ref").
	Name() string

	// Category returns the obligation category this generator belongs to.
	Category() Category

	// Description returns a human-readable description for `xpc explain`.
	Description() string

	// Generate enumerates all obligations for the given input.
	// Must be deterministic and exhaustive over its category scope.
	Generate(ctx *Context) []Obligation
}

// MakeID constructs a structured obligation ID.
func MakeID(cat Category, generator, instance string) string {
	if instance == "" {
		return fmt.Sprintf("XPC.%s.%s", cat, generator)
	}
	return fmt.Sprintf("XPC.%s.%s.%s", cat, generator, instance)
}

// ContentHash computes a short SHA256 hash of arbitrary content for provenance.
func ContentHash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", sum[:8])
}
