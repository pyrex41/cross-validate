package ir

import (
	"regexp"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// checkCompositionOrphanSGRef scans every go-templating Composition body for a
// registered rule resource (OrphanSGRefRegistry) whose peer reference dangles on
// teardown — the MR d144aa739b preview SG-orphan wedge (category S, Tier-2
// heuristic → R36 / XPC.S.orphaned-sgref). In a GitOps repo the SecurityGroupRule
// and the per-env SecurityGroup are both produced by this Composition at runtime,
// so neither reaches World.Resources (Tier-1) and neither has a live status
// (Tier-3); only the template text is here.
//
// The decisive static signal is an ASYMMETRY within one rule block:
//
//   - the rule is ATTACHED to a SecurityGroup that is NOT created by this
//     composition (a foreign/shared, presumptively long-lived SG — referenced by
//     `securityGroupIdSelector` whose matchLabels do NOT match any SecurityGroup
//     defined in this same template), AND
//   - the rule REFERENCES a peer SecurityGroup that IS created by this composition
//     (the per-env, short-lived SG — referenced by `sourceSecurityGroupIdSelector`
//     whose matchLabels DO match a SecurityGroup defined in this template).
//
// When the rule lives on a foreign SG but references a locally-created SG, tearing
// down the composition deletes the local SG while the rule on the foreign SG
// survives → the reference dangles and pins the local SG (DependencyViolation),
// exactly the wedge the reaper was written to clean up. Block-scoping (per
// `---`-delimited resource) keeps the attach/ref pair tied to one rule.
//
// Heuristic precision (warn severity, honestly stated):
//   - Both the rule AND the referenced SecurityGroup must live in the SAME checked
//     composition template. A rule whose attach/ref SGs live in a different file is
//     a miss (we cannot resolve a cross-file selector here).
//   - It only fires on SELECTOR-style refs (`*Selector` with matchLabels), because
//     the locality test is "does this selector's labels match an SG defined here?".
//     A rule that names its peer by a literal id (`sourceSecurityGroupId: sg-...`)
//     or an explicit `*Ref.name` is NOT classified (the literal-id egress case in
//     the same composition is a known miss — there is no in-template SG manifest to
//     correlate it against).
//   - It does not read deletionPolicy: the local SG is ASSUMED short-lived because
//     it is composition-scoped (per-env). A composition-scoped SG with an explicit
//     deletionPolicy: Orphan would be a false positive; the escape hatch is the
//     `xpc.io/allow-orphan-sgref` annotation on the rule or a .xpc-waivers.yaml
//     entry.
func (b *Builder) checkCompositionOrphanSGRef() {
	mappings := OrphanSGRefRegistry()
	if len(mappings) == 0 {
		return
	}
	for _, comp := range b.world.Compositions {
		for _, step := range comp.Pipeline {
			if step.InputKind != "GoTemplate" {
				continue
			}
			sc, ok := b.world.Schemas[step.InputDigest]
			if !ok {
				continue
			}
			inline, ok := sc.Schema["inline"].(map[string]interface{})
			if !ok {
				continue
			}
			tmplText, ok := inline["template"].(string)
			if !ok || tmplText == "" {
				continue
			}
			for _, m := range mappings {
				b.scanOrphanSGRefMapping(comp, tmplText, m)
			}
		}
	}
}

// orphanSGRefAllowAnnotationRe matches the per-rule escape-hatch annotation in a
// template block (the annotation lives under metadata.annotations as a YAML key).
var orphanSGRefAllowAnnotationRe = regexp.MustCompile(`(?m)^[ \t]*xpc\.io/allow-orphan-sgref:`)

// matchLabelsBlockRe captures the indented body following a `<field>:` line that
// has a `matchLabels:` child — used to extract the label set a selector keys on.
// The captured group is the remainder of the block; selectorLabelValues then
// pulls the `key: value` pairs out of its matchLabels sub-block.
func (b *Builder) scanOrphanSGRefMapping(comp types.CompositionInfo, tmplText string, m types.OrphanSGRefMapping) {
	attachRe := regexp.MustCompile(m.AttachFieldPattern)
	refRe := regexp.MustCompile(m.RefFieldPattern)

	// Label sets carried by every SecurityGroup defined in this template. A
	// selector whose matchLabels are a subset of one of these resolves to a
	// locally-created (composition-scoped, short-lived) SG.
	localSGLabels := securityGroupLabelSets(tmplText, m.RefKind)

	for _, block := range resourceBlocksOfKind(tmplText, m.Kind) {
		if orphanSGRefAllowAnnotationRe.MatchString(block) {
			continue // explicit bypass
		}
		attach := attachRe.FindStringSubmatch(block)
		ref := refRe.FindStringSubmatch(block)
		if attach == nil || ref == nil {
			continue
		}
		attachField := submatchOrFull(attach)
		refField := submatchOrFull(ref)

		// Only the selector-style ref is classifiable against in-template SGs.
		if !isSelectorField(refField) {
			continue
		}
		refLabels := selectorLabels(block, refField)
		if len(refLabels) == 0 {
			continue
		}
		// The REF must resolve to a locally-created SG (short-lived).
		if !labelsMatchAnyLocalSG(refLabels, localSGLabels) {
			continue
		}
		// The ATTACH must resolve to a NON-local SG (foreign / shared / long-lived).
		// A selector attach whose labels match a local SG is symmetric (both ends
		// local) and not the orphan shape; a literal-id / *Ref attach is
		// presumptively foreign (it names an SG not built here).
		if isSelectorField(attachField) {
			attachLabels := selectorLabels(block, attachField)
			if len(attachLabels) > 0 && labelsMatchAnyLocalSG(attachLabels, localSGLabels) {
				continue // both ends local → not a cross-scope dangling ref
			}
		}

		b.world.OrphanSGRefFindings = append(b.world.OrphanSGRefFindings, types.OrphanSGRefFinding{
			Composition: comp.Name,
			Group:       m.Group,
			Kind:        m.Kind,
			RuleName:    resourceNameInBlock(block),
			AttachField: attachField,
			RefField:    refField,
			Reason:      m.Reason,
			Source:      comp.Source,
		})
	}
}

// submatchOrFull returns capture group 1 when present and non-empty, else the
// whole match (with the trailing colon trimmed by the caller's field regexps,
// group 1 is the bare field name).
func submatchOrFull(m []string) string {
	if len(m) > 1 && m[1] != "" {
		return m[1]
	}
	return m[0]
}

// isSelectorField reports whether a resolved field key is a *Selector form.
func isSelectorField(field string) bool {
	return regexpSelectorSuffix.MatchString(field)
}

var regexpSelectorSuffix = regexp.MustCompile(`Selector$`)

// securityGroupLabelSets returns, for every SecurityGroup-kind block in the
// template, the set of metadata.labels key:value pairs it carries. These are the
// label sets a selector can resolve to a LOCALLY-created SG.
func securityGroupLabelSets(tmplText, sgKind string) []map[string]string {
	var sets []map[string]string
	for _, block := range resourceBlocksOfKind(tmplText, sgKind) {
		labels := metadataLabels(block)
		if len(labels) > 0 {
			sets = append(sets, labels)
		}
	}
	return sets
}

// metadataLabels extracts the key:value pairs from the `labels:` map nested under
// `metadata:` in a single resource block. Heuristic line scan (sufficient for the
// flat label maps compositions emit); values may contain `{{ ... }}` actions and
// are kept verbatim so a templated value matches a templated selector.
var metadataLabelsRe = regexp.MustCompile(`(?m)^([ \t]*)labels:[ \t]*$`)

func metadataLabels(block string) map[string]string {
	return labelMapAfter(block, metadataLabelsRe)
}

// selectorLabels extracts the matchLabels key:value pairs nested under the given
// selector field (e.g. `securityGroupIdSelector:` → `matchLabels:` → pairs).
func selectorLabels(block, field string) map[string]string {
	fieldRe := regexp.MustCompile(`(?m)^([ \t]*)` + regexp.QuoteMeta(field) + `:[ \t]*$`)
	loc := fieldRe.FindStringSubmatchIndex(block)
	if loc == nil {
		return nil
	}
	// Within the selector body, find matchLabels and read its child pairs.
	body := block[loc[1]:]
	mlRe := regexp.MustCompile(`(?m)^([ \t]*)matchLabels:[ \t]*$`)
	return labelMapAfter(body, mlRe)
}

// labelMapAfter finds the first match of mapKeyRe (a `<key>:` line whose indent is
// captured) and reads the immediately-following more-indented `k: v` lines as a
// label map, stopping at the first line that is not more-indented than the key.
func labelMapAfter(text string, mapKeyRe *regexp.Regexp) map[string]string {
	loc := mapKeyRe.FindStringSubmatchIndex(text)
	if loc == nil {
		return nil
	}
	keyIndent := text[loc[2]:loc[3]]
	rest := text[loc[1]:]
	out := map[string]string{}
	kvRe := regexp.MustCompile(`^([ \t]*)([^:\s][^:]*):[ \t]*(.*\S)?[ \t]*$`)
	for _, line := range splitLines(rest) {
		if isBlankOrComment(line) {
			continue
		}
		mm := kvRe.FindStringSubmatch(line)
		if mm == nil {
			break
		}
		indent := mm[1]
		if len(indent) <= len(keyIndent) {
			break // dedented out of the label map
		}
		key := mm[2]
		val := mm[3]
		if val == "" {
			// A nested map under the label block — stop, we only read flat labels.
			break
		}
		out[key] = val
	}
	return out
}

// labelsMatchAnyLocalSG reports whether the selector labels are a subset of any
// local SG's label set (a selector matches an SG when every matchLabels pair is
// present on the SG).
func labelsMatchAnyLocalSG(sel map[string]string, localSGs []map[string]string) bool {
	for _, sg := range localSGs {
		if labelsSubset(sel, sg) {
			return true
		}
	}
	return false
}

func labelsSubset(sel, sg map[string]string) bool {
	if len(sel) == 0 {
		return false
	}
	for k, v := range sel {
		if sg[k] != v {
			return false
		}
	}
	return true
}

// resourceNameInBlock reads the metadata.name line of a resource block (verbatim,
// may contain `{{ ... }}`), returning "" if absent.
var resourceNameRe = regexp.MustCompile(`(?m)^[ \t]*name:[ \t]*(.*\S)[ \t]*$`)

func resourceNameInBlock(block string) string {
	// metadata.name is the first `name:` after `metadata:`; the first name: in
	// the block is a good-enough proxy for the diagnostic label.
	if m := resourceNameRe.FindStringSubmatch(block); m != nil {
		return m[1]
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func isBlankOrComment(line string) bool {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == ' ' || c == '\t' {
			continue
		}
		return c == '#'
	}
	return true
}
