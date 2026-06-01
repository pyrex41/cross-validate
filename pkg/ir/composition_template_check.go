package ir

import (
	"fmt"
	"regexp"
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
