package checker

import "github.com/pyrex41/cross-validate-/pkg/types"

// RunResult is the output of a full check run. Shape matches the
// (now-deleted) obligation.RunResult — Phase 4 of the shen-as-spec
// migration moved this type into the checker package so the bridge
// no longer depends on the obligation framework.
type RunResult struct {
	// Diagnostics is the list of diagnostics produced by the kernel.
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
