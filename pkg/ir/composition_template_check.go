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
//       taskDefinition: arn:aws:ecs:{{ $region }}:...:task-definition/{{ $familyName }}
//       taskDefinition: {{ if $env }}arn:...:task-definition/{{ $familyName }}{{ end }}  (conditional on an unrelated var)
//
//   - Shape B — guarded one-shot seed. The versioned ARN is resolved first
//     ($taskDefArn / atProvider) and the bare literal is only the empty /
//     first-create fallback (an `{{ if $taskDefArn }}…{{ else }}<seed>` or a
//     `| default <seed>`). It emits bare for ~1 reconcile then converges — a
//     transient blip, NOT a permanent storm. This is the validated MR !2232
//     fix shape. PASS.
//       taskDefinition: {{ if $taskDefArn }}{{ $taskDefArn }}{{ else }}arn:...:task-definition/{{ $family }}{{ end }}
//       taskDefinition: {{ $taskDefArn | default (printf "...:task-definition/%s" $id) }}
//
//   - Shape C — never bare. No literal at all. PASS.
//       taskDefinition: {{ $taskDefArn }}
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
