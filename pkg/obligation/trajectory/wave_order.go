// Package trajectory implements Category F (trajectory-invariant) obligation generators.
package trajectory

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// WaveOrder is a Category F generator that checks Argo CD sync-wave ordering
// for XRD-before-XR and Function-before-Composition constraints.
//
// Absorbs legacy rule R6 (XPC006).
type WaveOrder struct{}

var _ obligation.Generator = WaveOrder{}

func (WaveOrder) Name() string                  { return "trajectory-wave-order" }
func (WaveOrder) Category() obligation.Category { return obligation.CatTrajectory }

func (WaveOrder) Description() string {
	return `Argo CD sync-wave ordering must respect resource dependencies.

XRDs must have a lower sync-wave than any XR of that kind, so the definition
is Established before instances are applied. Similarly, Functions must have
a lower sync-wave than Compositions that reference them in pipeline mode,
so the Function is Healthy before the Composition tries to use it.

Legacy code: XPC006`
}

// Generate emits obligations for each (XRD, XR) pair and each (Function, Composition) pair.
func (WaveOrder) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	// Build sync-wave lookup from all resources
	waveMap := make(map[string]int) // kind/name -> wave
	for _, res := range w.Resources {
		wave := ir.ParseSyncWave(res.Annotations)
		key := res.Kind + "/" + res.Name
		waveMap[key] = wave
	}

	// Also include XRDs with their waves (default 0 if not in resources)
	for _, xrd := range w.XRDs {
		key := "CompositeResourceDefinition/" + xrd.Kind
		if _, ok := waveMap[key]; !ok {
			waveMap[key] = 0
		}
	}

	var obs []obligation.Obligation

	for _, app := range w.ArgoApps {
		app := app // capture for closure

		// Use sync waves from the Argo Application if available
		for _, sw := range app.SyncWaves {
			key := sw.Kind + "/" + sw.Name
			waveMap[key] = sw.Wave
		}

		// R6a: XRD wave < XR wave
		for _, xrd := range w.XRDs {
			xrd := xrd
			xrdKey := "CompositeResourceDefinition/" + xrd.Kind
			xrdWave := waveMap[xrdKey]

			for _, res := range w.Resources {
				res := res
				if res.Kind != xrd.Kind {
					continue
				}
				resKey := res.Kind + "/" + res.Name
				resWave := waveMap[resKey]

				instance := sanitizeInstance(fmt.Sprintf("xrd-%s-xr-%s", xrd.Kind, res.Name))
				obs = append(obs, obligation.Obligation{
					ID:       obligation.MakeID(obligation.CatTrajectory, "trajectory-wave-order", instance),
					Category: obligation.CatTrajectory,
					Subject:  app.Source,
					Claim: fmt.Sprintf("XRD %s (wave %d) has a lower sync-wave than XR %s (wave %d)",
						xrd.Kind, xrdWave, res.Name, resWave),
					Provenance: obligation.Provenance{
						Generator: "trajectory-wave-order",
						Category:  obligation.CatTrajectory,
						InputHash: obligation.ContentHash(xrd.Kind + res.Name),
					},
					LegacyCode: "XPC006",
					Discharge: func(ctx *obligation.Context) obligation.Result {
						return dischargeWaveOrderXRD(app, xrd, res, xrdWave, resWave)
					},
				})
			}
		}

		// R6b: Function wave < Composition wave (pipeline mode only)
		for _, comp := range w.Compositions {
			comp := comp
			if comp.Mode != "Pipeline" {
				continue
			}
			compKey := "Composition/" + comp.Name
			compWave := waveMap[compKey]

			for _, step := range comp.Pipeline {
				step := step
				fnKey := "Function/" + step.FunctionRef
				fnWave := waveMap[fnKey]

				instance := sanitizeInstance(fmt.Sprintf("fn-%s-comp-%s", step.FunctionRef, comp.Name))
				obs = append(obs, obligation.Obligation{
					ID:       obligation.MakeID(obligation.CatTrajectory, "trajectory-wave-order", instance),
					Category: obligation.CatTrajectory,
					Subject:  app.Source,
					Claim: fmt.Sprintf("Function %s (wave %d) has a lower sync-wave than Composition %s (wave %d)",
						step.FunctionRef, fnWave, comp.Name, compWave),
					Provenance: obligation.Provenance{
						Generator: "trajectory-wave-order",
						Category:  obligation.CatTrajectory,
						InputHash: obligation.ContentHash(step.FunctionRef + comp.Name),
					},
					LegacyCode: "XPC006",
					Discharge: func(ctx *obligation.Context) obligation.Result {
						return dischargeWaveOrderFn(app, comp, step, fnWave, compWave)
					},
				})
			}
		}
	}

	return obs
}

func dischargeWaveOrderXRD(app types.ArgoApplication, xrd types.CRDInfo, res types.ResourceInfo, xrdWave, resWave int) obligation.Result {
	if xrdWave >= resWave {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC006",
				Severity: types.SeverityError,
				Source:   app.Source,
				Message: fmt.Sprintf("XRD %s (wave %d) must have a lower sync-wave than XR %s (wave %d)",
					xrd.Kind, xrdWave, res.Name, resWave),
				Detail: fmt.Sprintf("CompositeResourceDefinition %s must be Established before any XR "+
					"of this kind can be applied. The XRD sync-wave must be strictly less than the XR sync-wave.",
					xrd.Kind),
				Fix:     fmt.Sprintf("Set sync-wave on the XRD to a value less than %d.", resWave),
				Related: []types.SourceLocation{xrd.Source},
			},
		}
	}
	return obligation.Result{Status: obligation.Satisfied}
}

func dischargeWaveOrderFn(app types.ArgoApplication, comp types.CompositionInfo, step types.PipelineStep, fnWave, compWave int) obligation.Result {
	if fnWave >= compWave {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC006",
				Severity: types.SeverityError,
				Source:   app.Source,
				Message: fmt.Sprintf("Function %s (wave %d) must have a lower sync-wave than Composition %s (wave %d)",
					step.FunctionRef, fnWave, comp.Name, compWave),
				Detail: fmt.Sprintf("Function %s must be Healthy before Composition %s can use it. "+
					"The Function sync-wave must be strictly less than the Composition sync-wave.",
					step.FunctionRef, comp.Name),
				Fix: fmt.Sprintf("Set sync-wave on Function %s to a value less than %d.",
					step.FunctionRef, compWave),
				Related: []types.SourceLocation{comp.Source},
			},
		}
	}
	return obligation.Result{Status: obligation.Satisfied}
}

// sanitizeInstance cleans a resource name for use in an obligation ID.
func sanitizeInstance(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" {
		return "unnamed"
	}
	return name
}
