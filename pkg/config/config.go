// Package config defines the user-facing xpc.yaml schema and the loader that
// turns it into a typed Config. The file is optional: every section is
// resolved against built-in defaults so an absent / empty xpc.yaml produces
// behaviour bit-identical to the compile-time hardcoded path.
//
// Per design (thoughts/shared/design/xpc-yaml-config.md), the schema covers
// three knobs:
//
//   - prod-patterns:        substring matchers for R25's ApplicationSet name
//     classification. Replace-semantics if the block is non-empty.
//   - immutable-fields:     overlay over R27's hardcoded registry. Append by
//     default; opt out of a built-in via suppress: true.
//   - bypass-annotations:   per-rule primary + aliases for the load-bearing
//     bypass keys. Primary replaces the built-in; aliases are additive.
//   - name-carveouts:       per-rule substring carve-outs (e.g. R23's
//     "alb-logs"). Purely additive over the built-ins.
//
// The kernel never sees the bypass-annotations payload — the Go IR builder
// pre-collapses the matched annotation into a `bypass-yes`/`bypass-no` symbol
// before serialization. See pkg/ir/trajectory_extract.go for that path.
package config

// Config is the deserialized form of xpc.yaml after loader-level normalization
// (defaults, version-check, unknown-key handling). Every section is optional
// at the YAML level; the corresponding Go field is the zero value when absent.
type Config struct {
	// Version is the xpc.yaml schema version. Required for any non-empty
	// file. The loader rejects values it doesn't recognize so a future
	// schema-breaking change has a clean escape hatch.
	Version int `yaml:"version"`

	// ProdPatterns configures R25's prod-classification substring matcher.
	ProdPatterns ProdPatternsConfig `yaml:"prod-patterns"`

	// ImmutableFields is the user overlay over the built-in immutable-field
	// registry consumed by R27. Each entry either appends a new (gvk, path)
	// tuple or — when Suppress is true — removes the matching built-in.
	ImmutableFields []ImmutableFieldEntry `yaml:"immutable-fields"`

	// StateBearingKinds is the user overlay over the built-in state-bearing
	// kind allowlist consumed by R23 / R26. The block carries two parallel
	// lists: append (extra kinds the operator wants R23 to police) and
	// suppress (built-in kinds that the operator wants R23 to ignore).
	// Mirrors the immutable-fields shape but uses (group, kind) tuples
	// instead of (gvk, path) since R23 doesn't reason about field paths.
	StateBearingKinds StateBearingKindsConfig `yaml:"state-bearing-kinds"`

	// BypassAnnotations configures the annotation keys that silence each
	// rule's bypass-aware path. Per-rule primary + aliases.
	BypassAnnotations BypassAnnotationsConfig `yaml:"bypass-annotations"`

	// NameCarveouts configures rule-specific name substrings that exempt a
	// resource from a rule. Purely additive over the built-in carve-outs.
	NameCarveouts NameCarveoutsConfig `yaml:"name-carveouts"`
}

// ProdPatternsConfig configures R25's prod-name classification.
type ProdPatternsConfig struct {
	// AppSetNameSubstrings, if non-empty, REPLACES the built-in
	// {"-prod", "prod-"} list. To extend rather than replace, the user
	// includes the defaults explicitly.
	AppSetNameSubstrings []string `yaml:"appset-name-substrings"`
}

// ImmutableFieldEntry is one line of the user-supplied immutable-fields
// overlay. GVK is in "group/version/Kind" form (the version is parsed but
// not currently used by R27 — only Group + Kind are matched).
type ImmutableFieldEntry struct {
	// GVK is the "group/version/Kind" identifier. The empty group is
	// written as "/version/Kind" or just "version/Kind"; both are accepted.
	GVK string `yaml:"gvk"`
	// Paths is one or more dotted scalar-leaf field paths. Block / array
	// paths are out of scope for the registry — they would silently never
	// fire — so the loader rejects entries whose path looks block-shaped.
	Paths []string `yaml:"paths"`
	// Reason is a human prose blurb surfaced in the diagnostic detail. May
	// be empty for suppression entries.
	Reason string `yaml:"reason"`
	// Suppress, when true, removes the matching built-in (Group, Kind,
	// FieldPath) tuples instead of appending them.
	Suppress bool `yaml:"suppress"`
}

// StateBearingKindsConfig holds the user overlay over the built-in
// state-bearing kind allowlist (see pkg/ir/state_bearing_registry.go). Both
// lists are optional and resolved against the built-in defaults via
// ResolveStateBearingKinds. Empty / nil block means "use defaults verbatim".
//
// Suppress is applied first (so an entry that's both appended and suppressed
// ends up suppressed); Append second. De-dupe is by (group, kind) — case
// sensitive.
type StateBearingKindsConfig struct {
	// Append lists extra (group, kind) tuples that should join the
	// state-bearing allowlist. Useful for operators with their own
	// in-house CRDs whose deletion drops external state.
	Append []StateBearingKindEntry `yaml:"append"`

	// Suppress lists (group, kind) tuples that should be removed from the
	// built-in allowlist. Useful when an operator handles a particular
	// kind through a different mechanism (e.g. an org-specific
	// admission policy) and doesn't want xpc to police it.
	Suppress []StateBearingKindEntry `yaml:"suppress"`
}

// StateBearingKindEntry is one (group, kind) pair on the state-bearing
// overlay. Mirrors the shape of types.ArgoGroupKind so the resolver can
// emit the same value type the registry uses.
type StateBearingKindEntry struct {
	Group string `yaml:"group"`
	Kind  string `yaml:"kind"`
}

// BypassAnnotationsConfig is a per-rule (primary, aliases) table.
type BypassAnnotationsConfig struct {
	AllowDelete          BypassKeyConfig `yaml:"allow-delete"`
	AllowImmutableChange BypassKeyConfig `yaml:"allow-immutable-change"`
}

// BypassKeyConfig captures a single logical bypass — the primary annotation
// key plus zero-or-more aliases. Setting Primary to a non-empty string
// REPLACES the built-in primary; aliases are always additive.
type BypassKeyConfig struct {
	Primary string   `yaml:"primary"`
	Aliases []string `yaml:"aliases"`
}

// NameCarveoutsConfig holds rule-specific name substrings that exempt a
// resource from the named rule. Each list is additive over the built-in
// carve-outs documented at the rule.
type NameCarveoutsConfig struct {
	// CrossplaneStateNeedsOrphan is R23's name carve-out list. Built-in
	// default is {"alb-logs"} (ALB access-log buckets are separately
	// managed and intentionally destroyable).
	CrossplaneStateNeedsOrphan []string `yaml:"crossplane-state-needs-orphan"`
}
