package config

import (
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Resolution functions take a (possibly nil) *Config and a set of built-in
// values, and return the merged result. Each function documents its
// merge-semantics — replace vs. overlay vs. additive — to match the design
// in thoughts/shared/design/xpc-yaml-config.md §2.

// ResolveProdPatterns returns the substring list R25 should classify against.
// Replace-semantics: if the user supplies a non-empty list it wins outright.
// nil cfg or empty list falls back to the built-in defaults.
func ResolveProdPatterns(cfg *Config) []string {
	if cfg != nil && len(cfg.ProdPatterns.AppSetNameSubstrings) > 0 {
		return append([]string(nil), cfg.ProdPatterns.AppSetNameSubstrings...)
	}
	return append([]string(nil), defaultProdAppSetNameSubstrings...)
}

// ResolveAllowDeleteKeys returns every annotation key (primary + aliases)
// whose value "true" silences R23 / R26 for the resource carrying it.
// Replace-on-primary, additive-on-aliases. Order: primary first, then
// aliases in declaration order. Duplicates dropped.
func ResolveAllowDeleteKeys(cfg *Config) []string {
	if cfg == nil {
		return mergeBypass(defaultAllowDeletePrimary, defaultAllowDeleteAliases, BypassKeyConfig{})
	}
	return mergeBypass(defaultAllowDeletePrimary, defaultAllowDeleteAliases, cfg.BypassAnnotations.AllowDelete)
}

// ResolveAllowImmutableChangeKeys returns every annotation key whose value
// "true" silences R27 for the resource carrying it. Same merge shape as
// ResolveAllowDeleteKeys.
func ResolveAllowImmutableChangeKeys(cfg *Config) []string {
	if cfg == nil {
		return mergeBypass(defaultAllowImmutableChangePrimary, defaultAllowImmutableChangeAliases, BypassKeyConfig{})
	}
	return mergeBypass(defaultAllowImmutableChangePrimary, defaultAllowImmutableChangeAliases, cfg.BypassAnnotations.AllowImmutableChange)
}

// ResolveCrossplaneStateNeedsOrphanCarveouts returns the substring list R23
// should treat as name carve-outs. Additive-only: built-in defaults always
// participate; user entries append. Duplicates dropped.
func ResolveCrossplaneStateNeedsOrphanCarveouts(cfg *Config) []string {
	out := append([]string(nil), defaultCrossplaneStateNeedsOrphanCarveouts...)
	if cfg == nil {
		return out
	}
	for _, s := range cfg.NameCarveouts.CrossplaneStateNeedsOrphan {
		if s == "" {
			continue
		}
		if containsString(out, s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// ResolveImmutableFields applies the user overlay to the built-in registry
// and returns the merged result. Behaviour:
//
//   - Built-in entries are the starting set.
//   - User entries with Suppress=true remove every (Group, Kind, FieldPath)
//     hit. (One Suppress entry can target multiple paths via Paths.)
//   - User entries with Suppress=false append. If a (Group, Kind, FieldPath)
//     duplicate exists in the built-ins, the user entry wins on the Reason
//     string (mirrors the design doc note: "user entry winning").
func ResolveImmutableFields(cfg *Config, builtins []types.ImmutableField) []types.ImmutableField {
	out := append([]types.ImmutableField(nil), builtins...)
	if cfg == nil {
		return out
	}
	// First pass: suppressions.
	for _, e := range cfg.ImmutableFields {
		if !e.Suppress {
			continue
		}
		group, _, kind := splitGVK(e.GVK)
		for _, path := range e.Paths {
			out = removeImmutable(out, group, kind, path)
		}
	}
	// Second pass: appends/overrides.
	for _, e := range cfg.ImmutableFields {
		if e.Suppress {
			continue
		}
		group, _, kind := splitGVK(e.GVK)
		for _, path := range e.Paths {
			if path == "" {
				continue
			}
			out = upsertImmutable(out, types.ImmutableField{
				Group:     group,
				Kind:      kind,
				FieldPath: path,
				Reason:    e.Reason,
			})
		}
	}
	return out
}

// ResolveStateBearingKinds applies the user overlay to the built-in
// state-bearing kind allowlist and returns the merged result. Behaviour:
//
//   - Built-in entries are the starting set.
//   - Suppress entries are applied FIRST: any (Group, Kind) match is removed.
//   - Append entries are applied SECOND: any (Group, Kind) not already in
//     the result is appended. Duplicates inside Append are deduped.
//
// Order-of-operations matters when an operator both appends and suppresses
// the same kind — suppression wins, mirroring the immutable-fields contract.
func ResolveStateBearingKinds(cfg *Config, defaults []types.ArgoGroupKind) []types.ArgoGroupKind {
	out := append([]types.ArgoGroupKind(nil), defaults...)
	if cfg == nil {
		return out
	}
	// First pass: suppressions.
	for _, e := range cfg.StateBearingKinds.Suppress {
		out = removeStateBearing(out, e.Group, e.Kind)
	}
	// Second pass: appends.
	for _, e := range cfg.StateBearingKinds.Append {
		if containsStateBearing(out, e.Group, e.Kind) {
			continue
		}
		out = append(out, types.ArgoGroupKind{Group: e.Group, Kind: e.Kind})
	}
	return out
}

func removeStateBearing(in []types.ArgoGroupKind, group, kind string) []types.ArgoGroupKind {
	out := in[:0]
	for _, gk := range in {
		if gk.Group == group && gk.Kind == kind {
			continue
		}
		out = append(out, gk)
	}
	return out
}

func containsStateBearing(in []types.ArgoGroupKind, group, kind string) bool {
	for _, gk := range in {
		if gk.Group == group && gk.Kind == kind {
			return true
		}
	}
	return false
}

// mergeBypass returns primary+aliases for a single bypass slot. Primary
// override semantics; aliases are additive over the built-in alias list.
func mergeBypass(builtinPrimary string, builtinAliases []string, user BypassKeyConfig) []string {
	primary := builtinPrimary
	if user.Primary != "" {
		primary = user.Primary
	}
	out := []string{}
	if primary != "" {
		out = append(out, primary)
	}
	add := func(s string) {
		if s == "" {
			return
		}
		if containsString(out, s) {
			return
		}
		out = append(out, s)
	}
	for _, a := range builtinAliases {
		add(a)
	}
	for _, a := range user.Aliases {
		add(a)
	}
	return out
}

// splitGVK breaks "group/version/Kind" into its three parts. Accepts the
// shorthand "Kind" (group + version empty), "version/Kind" (core API), and
// the canonical "group/version/Kind". The version is parsed but not used by
// any current consumer — R27 keys on (Group, Kind) only.
func splitGVK(gvk string) (group, version, kind string) {
	parts := strings.Split(gvk, "/")
	switch len(parts) {
	case 1:
		return "", "", parts[0]
	case 2:
		return "", parts[0], parts[1]
	case 3:
		return parts[0], parts[1], parts[2]
	default:
		// Defensive: collapse extras into the kind so the entry doesn't
		// silently drop. Loader-level validation rejects malformed GVKs
		// up front, so this branch is only hit by callers bypassing Load.
		return parts[0], parts[1], strings.Join(parts[2:], "/")
	}
}

func removeImmutable(in []types.ImmutableField, group, kind, path string) []types.ImmutableField {
	out := in[:0]
	for _, f := range in {
		if f.Group == group && f.Kind == kind && f.FieldPath == path {
			continue
		}
		out = append(out, f)
	}
	return out
}

func upsertImmutable(in []types.ImmutableField, entry types.ImmutableField) []types.ImmutableField {
	for i, f := range in {
		if f.Group == entry.Group && f.Kind == entry.Kind && f.FieldPath == entry.FieldPath {
			if entry.Reason != "" {
				in[i].Reason = entry.Reason
			}
			return in
		}
	}
	return append(in, entry)
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
