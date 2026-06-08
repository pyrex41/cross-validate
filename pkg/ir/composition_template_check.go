package ir

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// checkCompositionTemplates statically parses the Go template embedded in every
// function-go-templating pipeline step and surfaces grammar errors as
// crossplane-render-failed RenderResults (routed to XPC.H.helm-renders / error
// severity by the kernel).
//
// Why this exists separately from renderCompositions: that pass only invokes
// `crossplane render` when a composite XR matching the Composition's
// compositeTypeRef is present in the doc set. In a GitOps repo the composite XR
// is synthesized at runtime (only the claim is committed, and the claim Kind
// differs from the composite Kind), so the render pass is skipped and the
// template body is never parsed. A malformed template (e.g. a `{{- /* */ -}}`
// comment nested inside an already-open `{{ ... }}` action — "unexpected \"{\"
// in operand") would otherwise ship to the cluster and fail at reconcile.
//
// This pass needs neither the crossplane binary nor a sample XR: it is a pure
// text/template.Parse, so it runs unconditionally (even under --skip-render).
func (b *Builder) checkCompositionTemplates() {
	for _, comp := range b.world.Compositions {
		for _, step := range comp.Pipeline {
			// function-go-templating's input is kind: GoTemplate with the
			// program under inline.template. The builder stores the input
			// map in World.Schemas keyed by the step's input digest.
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
			if err := parseGoTemplateText(tmplText); err != nil {
				b.world.RenderResults = append(b.world.RenderResults, types.RenderResult{
					AppName:   compositionAppName(comp.Name, ""),
					ChartPath: comp.Name,
					Success:   false,
					Error: fmt.Sprintf("go-template parse error in composition %q (step %q): %v",
						comp.Name, step.Name, err),
					ErrorKind: "crossplane-render-failed",
					Source:    comp.Source,
				})
			}
		}
	}
}

// envDictRe matches a literal ECS environment-variable entry built with sprig
// `dict`: `(dict "name" "<X>" "value" …)`. The `"value"` key (with its closing
// quote) distinguishes an env var from a secret (`"valueFrom"`) and from the
// container's own `(dict "name" "worker" "image" …)`.
var envDictRe = regexp.MustCompile(`\(dict\s+"name"\s+"([^"]+)"\s+"value"`)

// checkCompositionDuplicateEnv scans every go-templating Composition body for an
// ECS environment variable name emitted more than once (category M Tier-2,
// heuristic → R33 / XPC.M.duplicate-env-key). AWS dedupes the env array on
// registration, so a desired containerDefinitions with a duplicate name never
// matches the stored task def → a permanent diff on the immutable
// container_definitions field → upjet hard-fails (ReconcileError). The two
// copies routinely live in different parts of the template (a global list and a
// conditionally-appended override), so the scan is whole-template, not scoped to
// one `environment` construction.
//
// Scope: to avoid false positives, the scan runs ONLY on a composition that
// builds a single container — detected as exactly one `"environment"` key in
// the template. A whole-template env count cannot tell whether two same-named
// entries feed the SAME container (the bug) or different containers of a
// multi-container task (legitimate, and common — every container sets APP_ENV,
// OTEL_*, etc.). Restricting to single-container compositions keeps the check
// precise: verified against fg-manifold, this fires on exactly the two real
// turnover-worker duplicates and zero of the ~60 cross-container coincidences in
// the multi-container app/service/sustain compositions. A real same-container
// duplicate in a multi-container composition is a known miss (Tier-1 rendered /
// Tier-3 live would catch it).
//
// Warn severity: still a template-text heuristic.
func (b *Builder) checkCompositionDuplicateEnv() {
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
			// Single-container only — see scope note above.
			if strings.Count(tmplText, `"environment"`) != 1 {
				continue
			}
			counts := map[string]int{}
			var order []string
			for _, m := range envDictRe.FindAllStringSubmatch(tmplText, -1) {
				name := m[1]
				if counts[name] == 0 {
					order = append(order, name)
				}
				counts[name]++
			}
			for _, name := range order {
				if counts[name] < 2 {
					continue
				}
				b.world.DuplicateEnvFindings = append(b.world.DuplicateEnvFindings,
					types.DuplicateEnvFinding{
						Composition: comp.Name,
						EnvName:     name,
						Count:       counts[name],
						Source:      comp.Source,
					})
			}
		}
	}
}

// checkCompositionCanonicalForm scans every go-templating Composition body for
// registered canonical-form fields (CanonicalFormRegistry) assigned to a
// hardcoded, non-canonical ARN literal — the MR !2232 reconcile-storm shape.
// This is category M Tier-2 (heuristic): in a GitOps repo the ECS Service is
// produced by this Composition at runtime, so it never reaches World.Resources
// (Tier-1) and has no live status (Tier-3); only the template text is here.
//
// The scan is deliberately conservative to keep false positives low — see
// templateRHSNonCanonical. Findings are folded into R31 at warn severity.
func (b *Builder) checkCompositionCanonicalForm() {
	mappings := CanonicalFormRegistry()
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
				leaf := lastDotted(m.FieldPath)
				re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(leaf) + `:[ \t]*(.*\S)[ \t]*$`)
				for _, sub := range re.FindAllStringSubmatch(tmplText, -1) {
					rhs := sub[1]
					if !templateRHSNonCanonical(m.Detector, rhs) {
						continue
					}
					b.world.CanonicalFormTemplateFindings = append(b.world.CanonicalFormTemplateFindings,
						types.CanonicalFormTemplateFinding{
							Composition: comp.Name,
							Group:       m.Group,
							Kind:        m.Kind,
							Field:       leaf,
							RHS:         rhs,
							Canonical:   m.Canonical,
							Reason:      m.Reason,
							Source:      comp.Source,
						})
				}
			}
		}
	}
}

// templateRHSNonCanonical reports whether a Composition template's right-hand
// side for a registered field carries an *unguarded* non-canonical literal —
// one that drives a permanent reconcile storm (category M).
//
// For "arn-requires-revision" the scan descends INTO the template actions so a
// bare-family literal reachable only through a conditional branch is not missed
// (the earlier "any {{...}} RHS is canonical" short-circuit was the blind spot).
// Three runtime shapes, and what M does with each:
//
//   - Shape A — unconditional bare family. Renders bare on EVERY reconcile;
//     AWS normalises the read-back to family:revision, so desired never equals
//     observed → permanent storm. FLAG.
//     taskDefinition: arn:aws:ecs:{{ $region }}:...:task-definition/{{ $familyName }}
//     taskDefinition: {{ if $env }}arn:...:task-definition/{{ $familyName }}{{ end }}  (conditional on an unrelated var)
//
//   - Shape B — guarded one-shot seed. The versioned ARN is resolved first
//     ($taskDefArn / atProvider) and the bare literal is only the empty /
//     first-create fallback (an `{{ if $taskDefArn }}…{{ else }}<seed>` or a
//     `| default <seed>`). It emits bare for ~1 reconcile then converges — a
//     transient blip, NOT a permanent storm. This is the validated MR !2232
//     fix shape. PASS.
//     taskDefinition: {{ if $taskDefArn }}{{ $taskDefArn }}{{ else }}arn:...:task-definition/{{ $family }}{{ end }}
//     taskDefinition: {{ $taskDefArn | default (printf "...:task-definition/%s" $id) }}
//
//   - Shape C — never bare. No literal at all. PASS.
//     taskDefinition: {{ $taskDefArn }}
//
// The Shape-B blip (avoidable by never emitting a bare family) is a real but
// lower-stakes concern; it belongs in a future advisory hint, not in the
// permanent-storm rule. Keeping M = permanent storms keeps every M finding a
// true must-fix.
func templateRHSNonCanonical(detector, rhs string) bool {
	rhs = strings.TrimSpace(rhs)
	switch detector {
	case "arn-requires-revision":
		if !hasUnversionedTaskDefLiteral(rhs) {
			return false
		}
		// A bare-family literal is reachable. Pass it only if it is a guarded
		// one-shot seed (Shape B); flag an unguarded literal (Shape A).
		return !rhsHasCanonicalGuard(rhs)
	default:
		return false
	}
}

// templateActionRe matches a single Go-template action `{{ ... }}`. Go templates
// do not nest actions, so a non-greedy body is sufficient.
var templateActionRe = regexp.MustCompile(`\{\{.*?\}\}`)

// defaultPipeRe matches the sprig `| default` pipe — the idiom that makes a
// trailing literal a fallback of an already-resolved value.
var defaultPipeRe = regexp.MustCompile(`\|\s*default\b`)

// hasUnversionedTaskDefLiteral reports whether any LITERAL segment of the RHS
// (text outside `{{ ... }}` actions) contains a "task-definition/<family>" whose
// family component carries no ":revision". Template actions are blanked to a
// neutral space first so an interpolated family ({{ $family }}) reads as
// unversioned while a literal revision (…:42, or {{ $family }}:{{ $rev }}) does
// not. Mirrors the original after-the-last-slash check, generalised to descend
// through actions and to match a literal anywhere in the RHS (e.g. an else arm).
func hasUnversionedTaskDefLiteral(rhs string) bool {
	skeleton := templateActionRe.ReplaceAllString(rhs, " ")
	const marker = "task-definition/"
	for rest := skeleton; ; {
		i := strings.Index(rest, marker)
		if i < 0 {
			return false
		}
		after := rest[i+len(marker):]
		if !strings.Contains(after, ":") {
			return true
		}
		rest = after
	}
}

// rhsHasCanonicalGuard reports whether the RHS resolves the canonical versioned
// ARN first, making any trailing bare literal a one-shot seed (Shape B). The
// established idioms: a reference to the resolved-ARN variable ($taskDefArn), a
// reference to observed state (atProvider), or a `| default` fallback pipe.
func rhsHasCanonicalGuard(rhs string) bool {
	if strings.Contains(strings.ToLower(rhs), "taskdefarn") {
		return true
	}
	if strings.Contains(rhs, "atProvider") {
		return true
	}
	return defaultPipeRe.MatchString(rhs)
}

// checkCompositionComputedBlockAlias scans every go-templating Composition body
// for a registered computed-block action (ComputedBlockAliasRegistry) written in
// the simple scalar-alias form instead of the canonical sub-block — the
// MR !2336 elbv2 reconcile-storm shape (category M, Tier-2 heuristic → R34 /
// XPC.M.computed-block-alias). In a GitOps repo the LBListenerRule is produced
// by this Composition at runtime, so it never reaches World.Resources (Tier-1)
// and has no live status (Tier-3); only the template text is here.
//
// The scan is block-scoped, not whole-template: the decisive signal is "a
// `type: forward` action carries a targetGroupArn* alias but NO sibling
// `forward:` block", and that conjunction is only meaningful within one managed
// resource. function-go-templating emits each resource as its own `---`-delimited
// document, so we split on `---` and inspect each block whose `kind:` matches the
// registry row. Warn severity — a text scan of an unrendered block cannot be as
// certain as a concrete resource.
//
// False-positive guards (verified against fg-manifold, like R33):
//   - Requiring the canonical block ABSENT means the MR !2336 fixed form (which
//     HAS `forward:`) passes — its target group lives under
//     forward.targetGroup[].arnSelector, which the alias regex does not match.
//   - Requiring `type: forward` excludes redirect / fixed-response /
//     authenticate-* actions, which have no forward/target-group diff.
func (b *Builder) checkCompositionComputedBlockAlias() {
	mappings := ComputedBlockAliasRegistry()
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
				aliasRe := regexp.MustCompile(m.AliasFieldPattern)
				typeRe := regexp.MustCompile(`(?m)^[ \t]*-?[ \t]*type:[ \t]*` +
					regexp.QuoteMeta(m.ActionType) + `[ \t]*$`)
				blockRe := regexp.MustCompile(`(?m)^[ \t]*` +
					regexp.QuoteMeta(m.CanonicalBlockKey) + `:`)
				for _, block := range resourceBlocksOfKind(tmplText, m.Kind) {
					if !typeRe.MatchString(block) {
						continue
					}
					if blockRe.MatchString(block) {
						// Canonical sub-block present → correctly written. PASS.
						continue
					}
					alias := aliasRe.FindStringSubmatch(block)
					if alias == nil {
						continue
					}
					aliasField := alias[0]
					if len(alias) > 1 && alias[1] != "" {
						aliasField = alias[1]
					}
					b.world.ComputedBlockAliasFindings = append(b.world.ComputedBlockAliasFindings,
						types.ComputedBlockAliasFinding{
							Composition:    comp.Name,
							Group:          m.Group,
							Kind:           m.Kind,
							ActionType:     m.ActionType,
							AliasField:     aliasField,
							CanonicalBlock: m.CanonicalBlockKey,
							Reason:         m.Reason,
							Source:         comp.Source,
						})
				}
			}
		}
	}
}

// resourceBlocksOfKind splits a go-templating body into its `---`-delimited
// documents and returns those whose `kind:` line matches the given Kind. This
// scopes a per-resource heuristic (e.g. "this LBListenerRule lacks a forward
// block") to one resource at a time, so an alias key on resource A and a
// canonical block on resource B in the same template do not cancel out.
func resourceBlocksOfKind(tmplText, kind string) []string {
	kindRe := regexp.MustCompile(`(?m)^[ \t]*kind:[ \t]*` + regexp.QuoteMeta(kind) + `[ \t]*$`)
	var blocks []string
	for _, doc := range strings.Split(tmplText, "\n---") {
		if kindRe.MatchString(doc) {
			blocks = append(blocks, doc)
		}
	}
	return blocks
}

var undefinedFuncRe = regexp.MustCompile(`function "([^"]+)" not defined`)

// parseGoTemplateText returns a non-nil error only for genuine Go-template
// grammar errors. text/template.Parse also rejects calls to functions that are
// absent from the FuncMap, but at validation time we have neither sprig nor the
// function-go-templating builtins registered — and pulling them in would couple
// xpc to those exact versions. Instead we self-bootstrap the FuncMap: parse,
// and for every "function X not defined" error register a no-op stub for X and
// retry. Function names resolve away one by one until either the template
// parses cleanly or a real syntax error (which is not a missing-function error)
// surfaces. The stub's behaviour is irrelevant — Parse only checks that the
// name exists in the map, never executes it.
func parseGoTemplateText(text string) error {
	funcs := template.FuncMap{}
	stub := func(_ ...interface{}) interface{} { return nil }
	// Bound the loop well above the number of distinct helpers any real
	// composition template uses, so a pathological case can't spin forever.
	for i := 0; i < 1000; i++ {
		_, err := template.New("composition").Funcs(funcs).Parse(text)
		if err == nil {
			return nil
		}
		m := undefinedFuncRe.FindStringSubmatch(err.Error())
		if m == nil {
			// Not a missing-function error → a real grammar error.
			return err
		}
		funcs[m[1]] = stub
	}
	return nil
}
