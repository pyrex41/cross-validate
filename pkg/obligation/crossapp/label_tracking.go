// Package crossapp implements Category G (cross-Application) obligation generators.
package crossapp

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// LabelTracking is a Category G generator that warns when an Argo CD Application
// using label-based tracking conflicts with Crossplane Compositions.
//
// Absorbs legacy rule R7 (XPC007).
type LabelTracking struct{}

var _ obligation.Generator = LabelTracking{}

func (LabelTracking) Name() string                  { return "cross-app-label-tracking" }
func (LabelTracking) Category() obligation.Category { return obligation.CatCrossApp }

func (LabelTracking) Description() string {
	return `Argo CD label tracking conflicts with Crossplane Compositions.

When an Argo CD Application uses label-based tracking, it may conflict with
Crossplane's label propagation on composed resources. This causes Argo CD
to either prune Crossplane-created resources or fight Crossplane for ownership.

Fix: Switch to annotation-based tracking.

Legacy code: XPC007`
}

// Generate emits one obligation per (Application, Composition) pair where
// the Application uses label tracking.
func (LabelTracking) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	var obs []obligation.Obligation
	for _, app := range w.ArgoApps {
		app := app // capture for closure
		if app.TrackingMode != "label" {
			continue
		}

		for _, comp := range w.Compositions {
			comp := comp // capture for closure

			instance := sanitizeInstance(app.Name + "-" + comp.Name)
			obs = append(obs, obligation.Obligation{
				ID:       obligation.MakeID(obligation.CatCrossApp, "cross-app-label-tracking", instance),
				Category: obligation.CatCrossApp,
				Subject:  app.Source,
				Claim: fmt.Sprintf("Argo Application %s label tracking does not conflict with Composition %s",
					app.Name, comp.Name),
				Provenance: obligation.Provenance{
					Generator: "cross-app-label-tracking",
					Category:  obligation.CatCrossApp,
					InputHash: obligation.ContentHash(app.Name + comp.Name),
				},
				LegacyCode: "XPC007",
				Discharge: func(ctx *obligation.Context) obligation.Result {
					return dischargeLabelTracking(app, comp)
				},
			})
		}
	}

	return obs
}

func dischargeLabelTracking(app types.ArgoApplication, comp types.CompositionInfo) obligation.Result {
	return obligation.Result{
		Status: obligation.Violated,
		Diag: &types.Diagnostic{
			Code:     "XPC007",
			Severity: types.SeverityWarning,
			Source:   app.Source,
			Message: fmt.Sprintf("Argo CD label tracking conflicts with Crossplane Composition %s",
				comp.Name),
			Detail: fmt.Sprintf("Argo Application \"%s\" uses label-based tracking, but Composition "+
				"\"%s\" produces resources that Crossplane will label-propagate to. This causes Argo CD "+
				"to either prune Crossplane-created resources or fight Crossplane for ownership "+
				"(see crossplane/crossplane#2121).", app.Name, comp.Name),
			Fix:     "Switch Argo CD tracking mode to annotation: set argocd.argoproj.io/tracking-method: annotation on the Application.",
			Related: []types.SourceLocation{comp.Source},
		},
	}
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
