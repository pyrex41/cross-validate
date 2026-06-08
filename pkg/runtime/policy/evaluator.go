package policy

import (
	"bytes"
	"fmt"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/checker"
	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// ObjectRef identifies the Kubernetes object under admission. It mirrors the
// fields an admission webhook receives (GVK + name/namespace/uid) and is
// echoed back on the Decision for audit logging.
type ObjectRef struct {
	Group     string
	Version   string
	Kind      string
	Name      string
	Namespace string
	UID       string
}

// Decision is the outcome of evaluating a single object against the runtime
// rule subset.
type Decision struct {
	// Allowed is true when no error-severity diagnostic fired. Warnings and
	// info diagnostics do not block.
	Allowed bool
	// Mode is set by the caller (e.g. "enforce" / "dryrun"); left "" here.
	Mode        string
	Ref         ObjectRef
	Diagnostics []types.Diagnostic
	Errors      int
	Warnings    int
	// EvalNanos is the wall-clock cost of the build+check, for SLO tracking.
	EvalNanos int64
}

// Evaluator runs the single-object admission subset against incoming objects.
type Evaluator struct {
	KernelPath string
	Subset     []string
}

// New builds an Evaluator. A nil subset defaults to DecidableSubset() — the
// single-object admission-safe rules.
func New(kernelPath string, subset []string) *Evaluator {
	if subset == nil {
		subset = DecidableSubset()
	}
	return &Evaluator{KernelPath: kernelPath, Subset: subset}
}

// Evaluate parses one Kubernetes object (JSON or YAML) and checks it against
// the evaluator's rule subset.
//
// When ambient is non-nil its type-environment slices (CRDs/XRDs/Compositions/
// Functions/Providers/Configurations) are merged into the built world so
// ambient-tier rules can resolve references; the object's own resources are
// kept. The checker runs with RuleAllowlist == e.Subset, so only the subset's
// rules dispatch.
//
// Allowed is false iff any error-severity diagnostic fired.
func (e *Evaluator) Evaluate(raw []byte, ref ObjectRef, ambient *types.World) (Decision, error) {
	start := time.Now()

	docs, err := loader.LoadReader(bytes.NewReader(raw), admissionSource(ref))
	if err != nil {
		return Decision{Ref: ref}, fmt.Errorf("parsing admission object: %w", err)
	}

	builder := ir.NewBuilder()
	// Runtime admission has no helm/kustomize on PATH and renders nothing.
	builder.SkipRender = true
	world, err := builder.Build(docs)
	if err != nil {
		return Decision{Ref: ref}, fmt.Errorf("building IR for admission object: %w", err)
	}

	if ambient != nil {
		mergeAmbient(world, ambient)
	}

	diags, err := checker.Check(world, checker.Config{
		KernelPath:    e.KernelPath,
		RuleAllowlist: e.Subset,
	})
	if err != nil {
		return Decision{Ref: ref}, fmt.Errorf("checking admission object: %w", err)
	}

	dec := Decision{
		Ref:         ref,
		Diagnostics: diags,
		EvalNanos:   time.Since(start).Nanoseconds(),
	}
	for _, d := range diags {
		switch d.Severity {
		case types.SeverityError:
			dec.Errors++
		case types.SeverityWarning:
			dec.Warnings++
		}
	}
	dec.Allowed = dec.Errors == 0
	return dec, nil
}

// mergeAmbient appends the ambient World's read-only type-environment slices
// onto the freshly-built single-object world so reference-resolving rules have
// context. The object's own resources (and everything else already built) are
// left untouched — we only add ambient context, never replace it.
func mergeAmbient(world, ambient *types.World) {
	world.CRDs = append(world.CRDs, ambient.CRDs...)
	world.XRDs = append(world.XRDs, ambient.XRDs...)
	world.Compositions = append(world.Compositions, ambient.Compositions...)
	world.Functions = append(world.Functions, ambient.Functions...)
	world.Providers = append(world.Providers, ambient.Providers...)
	world.Configurations = append(world.Configurations, ambient.Configurations...)
}

// admissionSource synthesizes a stable source path for diagnostics so they
// point at the object under admission rather than a real file.
func admissionSource(ref ObjectRef) string {
	ns := ref.Namespace
	if ns == "" {
		ns = "_cluster"
	}
	return fmt.Sprintf("admission://%s/%s/%s", ref.Kind, ns, ref.Name)
}
