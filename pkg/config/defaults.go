package config

// Built-in defaults for the load-bearing config knobs. These mirror the
// hardcoded literals scattered across the codebase BEFORE xpc.yaml landed.
// The mirroring is asserted by TestDefault_Matches_Builtin in config_test.go,
// which compares each slice against the actual hardcoded site (kernel
// substring constants for prod-patterns, pkg/ir/trajectory_extract.go for the
// allow-delete keys, kernel/r23 for the alb-logs carve-out).
//
// Editing any value here requires editing the matching hardcoded site (or
// vice versa) — the test is the safety net.
var (
	defaultProdAppSetNameSubstrings = []string{"-prod", "prod-"}

	defaultAllowDeletePrimary = "xpc.io/allow-delete"
	// defaultAllowDeleteAliases is empty: the binary ships only the
	// xpc.io/-branded primary. Operators who want to recognize an
	// org-specific alias (e.g. "policy.<vendor>/allow-delete") register
	// it through xpc.yaml's bypass-annotations.allow-delete.aliases
	// section. See docs/xpc-yaml.md for the recipe.
	defaultAllowDeleteAliases []string

	defaultAllowImmutableChangePrimary = "xpc.io/allow-immutable-change"
	defaultAllowImmutableChangeAliases []string

	defaultCrossplaneStateNeedsOrphanCarveouts = []string{"alb-logs"}
)

// Default returns a Config populated with the same values the binary used
// before xpc.yaml landed. Resolving an empty Config{} or a Default() against
// any of the helpers below produces identical output, so the loader can
// short-circuit "no file present" by returning Default() and the rest of the
// pipeline stays oblivious.
//
// Version is set to 1 so a default-loaded Config round-trips through any
// future version-aware codepath without spurious "missing version" errors.
func Default() *Config {
	return &Config{
		Version: 1,
		ProdPatterns: ProdPatternsConfig{
			AppSetNameSubstrings: append([]string(nil), defaultProdAppSetNameSubstrings...),
		},
		BypassAnnotations: BypassAnnotationsConfig{
			AllowDelete: BypassKeyConfig{
				Primary: defaultAllowDeletePrimary,
				Aliases: append([]string(nil), defaultAllowDeleteAliases...),
			},
			AllowImmutableChange: BypassKeyConfig{
				Primary: defaultAllowImmutableChangePrimary,
				Aliases: append([]string(nil), defaultAllowImmutableChangeAliases...),
			},
		},
		NameCarveouts: NameCarveoutsConfig{
			CrossplaneStateNeedsOrphan: append([]string(nil),
				defaultCrossplaneStateNeedsOrphanCarveouts...),
		},
	}
}
