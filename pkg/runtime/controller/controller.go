// Package controller implements xpcd's observe-only reconcile loop. Where the
// admission webhook (cmd/xpcd serve) judges one object as it arrives, the
// controller (cmd/xpcd watch) periodically captures the WHOLE live cluster and
// evaluates the ambient decidable subset over everything already running.
//
// Capturing the full cluster is what makes the ambient-tier rules sound at
// runtime: every object a cross-reference rule needs (the Argo Application's
// ignoreDifferences, the resolved selector target, the XRD behind a claim) is
// present, so rules that could not run on a single admitted object light up
// here. The controller never mutates the cluster; it emits one observability
// event per offending resource with source "controller".
package controller

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/checker"
	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/runtime/obs"
	"github.com/pyrex41/cross-validate-/pkg/runtime/policy"
	"github.com/pyrex41/cross-validate-/pkg/snapshot"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Capturer abstracts a live-cluster capture. *clustersrc.Capturer satisfies it;
// tests inject a fake that returns a pre-built snapshot.
type Capturer interface {
	Capture(clusterName string) (*snapshot.Snapshot, error)
}

// CheckFunc runs the kernel over a world. checker.Check is the default; tests
// may inject a stub.
type CheckFunc func(*types.World, checker.Config) ([]types.Diagnostic, error)

// Reconciler sweeps the live cluster on an interval.
type Reconciler struct {
	Capturer    Capturer
	ClusterName string
	KernelPath  string
	// Subset is the rule allowlist. Defaults to policy.ControllerSubset() —
	// the single-object and ambient rules plus the live-diff rules (R32) the
	// whole-cluster capture (with observed status) makes sound.
	Subset []string
	// Mode labels emitted events (events are observe-only regardless). Defaults
	// to obs.ModeAudit.
	Mode    string
	Sink    obs.Sink
	Metrics *obs.Metrics

	// Now is an injectable clock (defaults to time.Now).
	Now func() time.Time
	// Check is an injectable kernel entry point (defaults to checker.Check).
	Check CheckFunc
}

// Summary reports the outcome of a single reconcile sweep.
type Summary struct {
	Resources  int
	Violations int
	ByCode     map[string]int
	Duration   time.Duration
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reconciler) subset() []string {
	if len(r.Subset) > 0 {
		return r.Subset
	}
	return policy.ControllerSubset()
}

func (r *Reconciler) checkFn() CheckFunc {
	if r.Check != nil {
		return r.Check
	}
	return checker.Check
}

func (r *Reconciler) mode() string {
	if r.Mode != "" {
		return r.Mode
	}
	return obs.ModeAudit
}

// ReconcileOnce captures the cluster, evaluates the subset over the whole
// world, and emits one event per offending resource. It returns a Summary and
// records sweep metrics. A capture or evaluation failure returns an error
// without emitting partial results.
func (r *Reconciler) ReconcileOnce(ctx context.Context) (Summary, error) {
	start := r.now()

	snap, err := r.Capturer.Capture(r.ClusterName)
	if err != nil {
		return Summary{}, fmt.Errorf("capture cluster %q: %w", r.ClusterName, err)
	}

	// ToWorld() returns a bare type environment + resources; the enrichment
	// fields (selector/late-init usages, deletion-policy facts, SSA conflicts)
	// are not persisted in a snapshot, so recompute them exactly as
	// Builder.Build would before handing the world to the kernel.
	world := snap.ToWorld()
	ir.EnrichTrajectoryData(world)
	ir.EnrichFieldValidation(world)

	diags, err := r.checkFn()(world, checker.Config{
		KernelPath:    r.KernelPath,
		RuleAllowlist: r.subset(),
	})
	if err != nil {
		return Summary{}, fmt.Errorf("evaluate world: %w", err)
	}

	summary := r.emit(world, diags)
	summary.Duration = r.now().Sub(start)
	if r.Metrics != nil {
		r.Metrics.RecordControllerRun(summary.Resources, summary.Violations, r.now())
	}
	return summary, nil
}

// emit groups diagnostics by the resource they flag (via source location),
// attaches resource identity, and emits one event per offending resource.
// Clean resources produce no event — the controller stream carries only
// signal.
func (r *Reconciler) emit(world *types.World, diags []types.Diagnostic) Summary {
	idx := indexResources(world)

	type group struct {
		ref      obs.Event
		codes    []string
		seen     map[string]bool
		errors   int
		warnings int
		message  string
	}
	groups := map[srcKey]*group{}
	var order []srcKey

	for _, d := range diags {
		key := srcKeyOf(d.Source)
		g := groups[key]
		if g == nil {
			g = &group{seen: map[string]bool{}}
			if res, ok := idx[key]; ok {
				grp, ver := splitAPIVersion(res.APIVersion)
				g.ref = obs.Event{Group: grp, Version: ver, Kind: res.Kind, Name: res.Name, Namespace: res.Namespace}
			}
			groups[key] = g
			order = append(order, key)
		}
		if !g.seen[d.Code] {
			g.seen[d.Code] = true
			g.codes = append(g.codes, d.Code)
		}
		switch d.Severity {
		case types.SeverityError:
			g.errors++
		case types.SeverityWarning:
			g.warnings++
		}
		if g.message == "" {
			g.message = d.Message
		}
	}

	byCode := map[string]int{}
	violations := 0
	mode := r.mode()
	when := r.now()

	for _, src := range order {
		g := groups[src]
		if g.errors == 0 && g.warnings == 0 {
			continue
		}
		sort.Strings(g.codes)
		for _, c := range g.codes {
			byCode[c]++
		}
		violations++

		ev := g.ref
		ev.Timestamp = when
		ev.Mode = mode
		ev.Cluster = r.ClusterName
		ev.Operation = "" // not an admission operation
		ev.Source = "controller"
		ev.RuleCodes = g.codes
		ev.Errors = g.errors
		ev.Warnings = g.warnings
		ev.Message = g.message
		if g.errors > 0 {
			ev.Decision = obs.DecisionWouldDeny
		} else {
			ev.Decision = obs.DecisionWarn
		}
		if r.Sink != nil {
			r.Sink.Emit(ev)
		}
		if r.Metrics != nil {
			r.Metrics.Observe(ev)
		}
	}

	return Summary{Resources: len(world.Resources), Violations: violations, ByCode: byCode}
}

// Run sweeps immediately, then every interval until ctx is cancelled. Capture
// and evaluation failures are logged and the loop continues — a transient
// kubectl/API blip must not kill the controller.
func (r *Reconciler) Run(ctx context.Context, interval time.Duration) error {
	r.runOnce(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Reconciler) runOnce(ctx context.Context) {
	s, err := r.ReconcileOnce(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xpcd watch: sweep failed: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "xpcd watch: swept %d resources, %d with violations (%v)\n",
		s.Resources, s.Violations, s.Duration.Round(time.Millisecond))
}

// srcKey identifies a source position by file and line. Column is excluded:
// the kernel stamps diagnostics at column 0 while the loader records a resource
// at column 1, so (file, line) is the stable join key between the two.
type srcKey struct {
	file string
	line int
}

func srcKeyOf(s types.SourceLocation) srcKey { return srcKey{file: s.File, line: s.Line} }

// indexResources keys live resources by (file, line) so a diagnostic carrying
// the same position can be attributed back to its object.
func indexResources(w *types.World) map[srcKey]types.ResourceInfo {
	idx := make(map[srcKey]types.ResourceInfo, len(w.Resources))
	for _, res := range w.Resources {
		idx[srcKeyOf(res.Source)] = res
	}
	return idx
}

// splitAPIVersion splits "group/version" into (group, version); a bare
// "version" (core API) yields ("", version).
func splitAPIVersion(apiVersion string) (group, version string) {
	for i := 0; i < len(apiVersion); i++ {
		if apiVersion[i] == '/' {
			return apiVersion[:i], apiVersion[i+1:]
		}
	}
	return "", apiVersion
}
