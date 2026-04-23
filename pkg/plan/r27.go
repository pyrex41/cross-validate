package plan

import (
	"fmt"
	"reflect"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// R27ImmutableChange flags changes to scalar-leaf fields registered as
// immutable in ir.ImmutableFieldRegistry.
//
// Operates on plan.Delta.Modified only — Added and Removed are out of scope.
// R26 handles destructive removal (state-bearing resources disappearing from
// HEAD); R27 handles the "still present, but about to be reshaped in a way
// the external system cannot accept" case.
//
// Logic: for each Modified change, look up (group, kind) against the
// registry. For every matching entry, read the field path on both BaseRaw
// and HeadRaw via ir.ReadPath; if both reads succeed and the values differ
// (reflect.DeepEqual is false), emit one XPC.P.immutable-change diagnostic.
// reflect.DeepEqual is the right comparator here: YAML round-tripping
// produces mixed int/float/string types for the same source value, and
// DeepEqual's strict comparison is the stable choice vs. a custom coercion.
//
// Bypass: the HEAD-side annotation `xpc.io/allow-immutable-change: "true"`
// silences all diagnostics for that identity. The head side is where the
// change author consents to the destructive reshape (mirroring R26's
// base-side bypass: the bypass lives where the intent is expressed).
func R27ImmutableChange(delta ResourceDelta) []types.Diagnostic {
	registry := ir.ImmutableFieldRegistry()

	var diags []types.Diagnostic
	for _, c := range delta.Modified {
		if hasImmutableChangeBypass(c.HeadRaw) {
			continue
		}
		group := groupFromAPIVersion(c.Identity.APIVersion)
		for _, entry := range registry {
			if entry.Group != group || entry.Kind != c.Identity.Kind {
				continue
			}
			baseVal, okBase := ir.ReadPath(c.BaseRaw, entry.FieldPath)
			headVal, okHead := ir.ReadPath(c.HeadRaw, entry.FieldPath)
			if !okBase || !okHead {
				continue
			}
			if reflect.DeepEqual(baseVal, headVal) {
				continue
			}
			diags = append(diags, types.Diagnostic{
				Code:     "XPC.P.immutable-change",
				Severity: types.SeverityError,
				Message: fmt.Sprintf("%s %s: immutable field %s changed",
					gkForMessage(group, c.Identity.Kind),
					qualifiedName(c.Identity),
					entry.FieldPath),
				Detail: fmt.Sprintf("Base value %v → head value %v. %s. "+
					"Applying this change will require destroying and recreating the external object.",
					baseVal, headVal, entry.Reason),
				Fix: fmt.Sprintf("Either (a) revert the change to %s on HEAD, "+
					"(b) recreate the resource under a new identity if the reshape is intentional, or "+
					"(c) add annotation xpc.io/allow-immutable-change: \"true\" on the HEAD manifest to consent to the recreate.",
					entry.FieldPath),
				Source: c.BaseSource,
			})
		}
	}
	return diags
}

// hasImmutableChangeBypass looks for xpc.io/allow-immutable-change: "true"
// on metadata.annotations of the HEAD manifest. Mirrors hasBypassAnnotation
// in r26.go (but distinct key — R26's allow-delete is a different intent).
func hasImmutableChangeBypass(raw map[string]interface{}) bool {
	if raw == nil {
		return false
	}
	meta, ok := raw["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	ann, ok := meta["annotations"].(map[string]interface{})
	if !ok {
		return false
	}
	if v, ok := ann["xpc.io/allow-immutable-change"].(string); ok && v == "true" {
		return true
	}
	return false
}

// gkForMessage renders a "group/Kind" prefix for diagnostic messages. For
// core-API kinds (empty group) it collapses to just the Kind so messages
// read naturally ("Service foo" vs "/Service foo").
func gkForMessage(group, kind string) string {
	if group == "" {
		return kind
	}
	return group + "/" + kind
}
