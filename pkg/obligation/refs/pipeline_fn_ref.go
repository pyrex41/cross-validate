package refs

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// PipelineFnRef is a Category B generator that checks whether each pipeline
// step's function reference resolves to an existing Function, and whether the
// input version (if specified) matches one the function accepts.
//
// Absorbs legacy rule R4 (XPC004).
type PipelineFnRef struct{}

var _ obligation.Generator = PipelineFnRef{}

func (PipelineFnRef) Name() string                  { return "pipeline-fn-ref" }
func (PipelineFnRef) Category() obligation.Category { return obligation.CatReference }

func (PipelineFnRef) Description() string {
	return `Pipeline function references must resolve to existing Functions.

Each pipeline step in a Composition references a Function by name. If no
Function with that name exists, or if the step specifies an input version that
the Function does not accept, the pipeline will fail at runtime.

Fix: Ensure the Function is defined and accepts the specified input version.

Legacy code: XPC004`
}

// Generate emits one obligation per (Composition pipeline step, function reference) pair.
// Only Pipeline-mode Compositions are considered.
func (PipelineFnRef) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	// Build function lookup
	fnMap := make(map[string]*types.FunctionInfo)
	for i := range w.Functions {
		fnMap[w.Functions[i].Name] = &w.Functions[i]
	}

	var obs []obligation.Obligation
	for _, comp := range w.Compositions {
		comp := comp // capture for closure
		if comp.Mode != "Pipeline" {
			continue
		}
		for _, step := range comp.Pipeline {
			step := step // capture for closure
			instance := sanitizeInstance(comp.Name + "." + step.Name)

			obs = append(obs, obligation.Obligation{
				ID:       obligation.MakeID(obligation.CatReference, "pipeline-fn-ref", instance),
				Category: obligation.CatReference,
				Subject:  comp.Source,
				Claim: fmt.Sprintf("Composition %s step %s function reference %s resolves",
					comp.Name, step.Name, step.FunctionRef),
				Provenance: obligation.Provenance{
					Generator: "pipeline-fn-ref",
					Category:  obligation.CatReference,
					InputHash: obligation.ContentHash(comp.Name + step.Name + step.FunctionRef),
				},
				LegacyCode: "XPC004",
				Discharge: func(ctx *obligation.Context) obligation.Result {
					return dischargePipelineFnRef(comp, step, fnMap)
				},
			})
		}
	}

	return obs
}

func dischargePipelineFnRef(comp types.CompositionInfo, step types.PipelineStep, fnMap map[string]*types.FunctionInfo) obligation.Result {
	fn, ok := fnMap[step.FunctionRef]
	if !ok {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC004",
				Severity: types.SeverityError,
				Source:   comp.Source,
				Message: fmt.Sprintf("Composition %s step %s references unknown function %s",
					comp.Name, step.Name, step.FunctionRef),
				Detail: fmt.Sprintf("Pipeline step \"%s\" references function \"%s\" but no "+
					"Function resource with this name was found.", step.Name, step.FunctionRef),
				Fix: fmt.Sprintf("Ensure Function \"%s\" is defined and included in the checked manifests.",
					step.FunctionRef),
			},
		}
	}

	if step.InputAPIVersion == "" || len(fn.InputVersions) == 0 {
		return obligation.Result{Status: obligation.Satisfied}
	}

	found := false
	for _, v := range fn.InputVersions {
		if v == step.InputAPIVersion {
			found = true
			break
		}
	}
	if !found {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC004",
				Severity: types.SeverityError,
				Source:   comp.Source,
				Message: fmt.Sprintf("Composition %s step %s input version mismatch for function %s",
					comp.Name, step.Name, step.FunctionRef),
				Detail: fmt.Sprintf("Pipeline step \"%s\" passes input at %s but function \"%s\" accepts: %s",
					step.Name, step.InputAPIVersion, step.FunctionRef,
					strings.Join(fn.InputVersions, ", ")),
				Fix: fmt.Sprintf("Change the input apiVersion to one accepted by the function: %s",
					strings.Join(fn.InputVersions, ", ")),
				Related: []types.SourceLocation{fn.Source},
			},
		}
	}

	return obligation.Result{Status: obligation.Satisfied}
}
