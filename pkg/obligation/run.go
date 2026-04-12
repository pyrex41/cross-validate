package obligation

import (
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// RunResult is the output of a full obligation check run.
type RunResult struct {
	// Diagnostics is the list of diagnostics from violated obligations.
	Diagnostics []types.Diagnostic

	// TotalObligations is the count of all obligations generated.
	TotalObligations int

	// Satisfied is the count of obligations that were discharged successfully.
	Satisfied int

	// Violated is the count of obligations that failed.
	Violated int

	// Unknown is the count of obligations that could not be determined.
	Unknown int

	// ObligationIDs lists every obligation ID that was checked (for audit).
	ObligationIDs []string
}

// Run executes all generators in the registry and discharges every obligation.
// This is the authoritative checker loop. It replaces the sequential
// checkR1..checkR11 calls in pkg/checker/bridge.go.
func Run(reg *Registry, ctx *Context) RunResult {
	var result RunResult

	for _, gen := range reg.All() {
		obligations := gen.Generate(ctx)
		for i := range obligations {
			ob := &obligations[i]
			result.TotalObligations++
			result.ObligationIDs = append(result.ObligationIDs, ob.ID)

			r := ob.Discharge(ctx)

			switch r.Status {
			case Satisfied:
				result.Satisfied++
			case Violated:
				result.Violated++
				if r.Diag != nil {
					// Attach obligation provenance to the diagnostic.
					ref := ob.Ref()
					r.Diag.Obligation = &ref
					result.Diagnostics = append(result.Diagnostics, *r.Diag)
				}
			case Unknown:
				result.Unknown++
			}
		}
	}

	return result
}
