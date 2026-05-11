package checker

import (
	"time"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Timing records one measured stage in a check run.
type Timing struct {
	Name         string        `json:"name"`
	Duration     time.Duration `json:"durationNs"`
	Milliseconds float64       `json:"milliseconds"`
}

// NewTiming creates a serializable timing record from a measured duration.
func NewTiming(name string, d time.Duration) Timing {
	return Timing{
		Name:         name,
		Duration:     d,
		Milliseconds: float64(d) / float64(time.Millisecond),
	}
}

// RuleTiming records the cost and yield of one profiled rule group.
type RuleTiming struct {
	Rule             string        `json:"rule"`
	Codes            []string      `json:"codes"`
	Duration         time.Duration `json:"durationNs"`
	Milliseconds     float64       `json:"milliseconds"`
	Diagnostics      int           `json:"diagnostics"`
	TotalObligations int           `json:"totalObligations"`
	Satisfied        int           `json:"satisfied"`
	Violated         int           `json:"violated"`
}

// NewRuleTiming creates a serializable rule timing record.
func NewRuleTiming(rule string, codes []string, d time.Duration, result RunResult) RuleTiming {
	return RuleTiming{
		Rule:             rule,
		Codes:            codes,
		Duration:         d,
		Milliseconds:     float64(d) / float64(time.Millisecond),
		Diagnostics:      len(result.Diagnostics),
		TotalObligations: result.TotalObligations,
		Satisfied:        result.Satisfied,
		Violated:         result.Violated,
	}
}

// RunResult is the output of a full check run.
type RunResult struct {
	// Diagnostics is the list of diagnostics produced by the kernel.
	Diagnostics []types.Diagnostic

	// TotalObligations is the count of all obligations generated.
	TotalObligations int

	// Satisfied is the count of obligations that were discharged successfully.
	Satisfied int

	// Violated is the count of obligations that failed.
	Violated int

	// ObligationIDs lists every obligation ID that was checked (for audit).
	ObligationIDs []string

	// StageTimings records coarse-grained checker stages when timing is enabled.
	StageTimings []Timing

	// RuleTimings records per-rule timings when rule profiling is enabled.
	RuleTimings []RuleTiming
}
