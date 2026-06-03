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
// side for a registered field is a hardcoded non-canonical literal.
//
// For "arn-requires-revision" the discriminator that separates the MR !2232 bug
// from its fix:
//   - BAD  taskDefinition: arn:aws:ecs:{{ $region }}:...:task-definition/{{ $familyName }}
//     a hardcoded ARN literal — the segment after the last "/" carries no
//     ":revision".
//   - GOOD taskDefinition: {{ $taskDefArn | default (printf "...") }}
//     a value computed entirely by a template action (the fix resolves the
//     versioned ARN from the observed TaskDefinition). A pure "{{ ... }}" RHS,
//     or any RHS that mentions atProvider, is assumed resolved and not flagged.
//
// Consequence: a composition that computes an unversioned value entirely inside
// "{{ ... }}" is a miss here (Tier-1/Tier-3 catch the rendered/live form). That
// is the intended trade — Tier-2 only flags what it can see with confidence.
func templateRHSNonCanonical(detector, rhs string) bool {
	rhs = strings.TrimSpace(rhs)
	switch detector {
	case "arn-requires-revision":
		if strings.HasPrefix(rhs, "{{") || strings.Contains(rhs, "atProvider") {
			return false
		}
		i := strings.LastIndex(rhs, "/")
		if i < 0 {
			return false
		}
		return !strings.Contains(rhs[i+1:], ":")
	default:
		return false
	}
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
