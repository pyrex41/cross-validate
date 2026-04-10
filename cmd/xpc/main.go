// xpc is a type checker for Crossplane + Argo CD configurations.
// It catches structural and operational bugs in the relationships between
// CRDs, Compositions, Functions, and Argo Applications before they reach
// a cluster.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pyrex41/cross-validate-/pkg/checker"
	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/report"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "check":
		os.Exit(runCheck(os.Args[2:]))
	case "dump-ir":
		os.Exit(runDumpIR(os.Args[2:]))
	case "explain":
		os.Exit(runExplain(os.Args[2:]))
	case "version":
		fmt.Printf("xpc %s\n", version)
		os.Exit(0)
	case "help", "--help", "-h":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `xpc — a type checker for Crossplane + Argo CD configurations

Usage:
  xpc check [flags] <path>         Check manifests for errors
  xpc dump-ir <path>               Dump the intermediate representation
  xpc explain <code>               Show docs for an error code (e.g., XPC002)
  xpc version                      Print version

Check flags:
  --format=<fmt>       Output format: human, json, lsp, junit, sarif (default: human)
  --strict-conversions Refuse webhook conversions entirely
  --shen=<path>        Path to shen-cl binary (uses built-in Go checker if absent)
  --kernel=<path>      Path to kernel directory (default: embedded)

Examples:
  xpc check ./manifests
  xpc check --format=sarif ./manifests > results.sarif
  xpc dump-ir ./manifests
  xpc explain XPC002
`)
}

func runCheck(args []string) int {
	format := report.FormatHuman
	strictConversions := false
	shenBinary := ""
	kernelPath := ""
	var paths []string

	for _, arg := range args {
		switch {
		case arg == "--strict-conversions":
			strictConversions = true
		case len(arg) > 9 && arg[:9] == "--format=":
			format = report.Format(arg[9:])
		case len(arg) > 7 && arg[:7] == "--shen=":
			shenBinary = arg[7:]
		case len(arg) > 9 && arg[:9] == "--kernel=":
			kernelPath = arg[9:]
		case arg == "--help" || arg == "-h":
			printUsage()
			return 0
		case len(arg) > 0 && arg[0] == '-':
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			return 1
		default:
			paths = append(paths, arg)
		}
	}

	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "error: no path specified")
		return 1
	}

	// Load documents
	var allDocs []loader.LoadedDocument
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		var docs []loader.LoadedDocument
		if info.IsDir() {
			docs, err = loader.LoadDirectory(path)
		} else {
			docs, err = loader.LoadFile(path)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading %s: %v\n", path, err)
			return 1
		}
		allDocs = append(allDocs, docs...)
	}

	if len(allDocs) == 0 {
		fmt.Fprintln(os.Stderr, "no YAML documents found")
		return 1
	}

	// Build IR
	builder := ir.NewBuilder()
	world, err := builder.Build(allDocs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building IR: %v\n", err)
		return 1
	}

	// Auto-detect kernel path
	if kernelPath == "" {
		exe, _ := os.Executable()
		if exe != "" {
			candidate := filepath.Join(filepath.Dir(exe), "..", "kernel")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				kernelPath = candidate
			}
		}
		if kernelPath == "" {
			kernelPath = "kernel"
		}
	}

	// Run checker
	cfg := checker.Config{
		KernelPath:        kernelPath,
		ShenBinary:        shenBinary,
		StrictConversions: strictConversions,
	}

	diags, err := checker.Check(world, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running checker: %v\n", err)
		return 1
	}

	// Report
	if err := report.ReportStdout(diags, format); err != nil {
		fmt.Fprintf(os.Stderr, "error writing report: %v\n", err)
		return 1
	}

	// Exit non-zero if there are errors
	for _, d := range diags {
		if d.Severity == types.SeverityError {
			return 1
		}
	}
	return 0
}

func runDumpIR(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: no path specified")
		return 1
	}

	path := args[0]
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	var docs []loader.LoadedDocument
	if info.IsDir() {
		docs, err = loader.LoadDirectory(path)
	} else {
		docs, err = loader.LoadFile(path)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading %s: %v\n", path, err)
		return 1
	}

	builder := ir.NewBuilder()
	world, err := builder.Build(docs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building IR: %v\n", err)
		return 1
	}

	fmt.Print(ir.ToSExpr(world))
	return 0
}

func runExplain(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: no error code specified")
		return 1
	}

	code := args[0]
	explanation, ok := errorExplanations[code]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown error code: %s\n", code)
		fmt.Fprintln(os.Stderr, "\nKnown error codes: XPC001-XPC009")
		return 1
	}

	fmt.Println(explanation)
	return 0
}

var errorExplanations = map[string]string{
	"XPC001": `XPC001: CRD/XRD version coherence

Every CRD must have exactly one storage version. Every declared version must
be marked as served. For XRDs, at least one version must be referenceable.

A CRD with no storage version will fail to install. A CRD with unserved
versions has dead entries that confuse tooling. An XRD with no referenceable
version cannot be used by any Composition.

Fix: Ensure exactly one version has storage: true (or referenceable: true for
XRDs) and all versions have served: true.`,

	"XPC002": `XPC002: webhook conversion not acknowledged

A resource is written at a non-storage version, and the CRD uses a Webhook
conversion strategy. Every read or write of this resource will invoke the
conversion webhook — a network round-trip that is a latency risk and a single
point of failure.

This is the motivating bug for xpc: a Crossplane managed resource authored at
v1beta1 when the storage version was v1beta2, causing conversion webhook calls
that grew from 200ms to 15s under load and cascaded into controller timeouts.

See also: kubernetes/kubernetes#129979

Fix: Re-author the resource at the storage version (recommended), or add
annotation xpc.dev/accept-conversion-webhook: "true" to acknowledge the risk.
Use --strict-conversions to refuse webhook conversions entirely.`,

	"XPC003": `XPC003: Composition references unresolvable XRD

A Composition's compositeTypeRef points to a group/version/kind that either:
(a) has no matching XRD at all, or (b) has an XRD but the referenced version
is not marked referenceable.

This will cause the Composition to fail silently — Crossplane won't render any
resources for it because it can't resolve the composite type.

Fix: Ensure the XRD exists and the referenced version is referenceable.`,

	"XPC004": `XPC004: pipeline function not found or input version mismatch

A Composition pipeline step references a Function that either:
(a) doesn't exist as a Function resource, or (b) exists but the input
apiVersion doesn't match what the function accepts.

Case (a) will cause a runtime error. Case (b) will cause silent failures or
partial behavior — the function may ignore input fields it doesn't recognize.

Fix: Ensure the Function resource exists, and that the input apiVersion matches
the function's expected version.`,

	"XPC005": `XPC005: patch type mismatch

A patch in a Composition reads a field of one type (e.g., string) and writes
it to a field of an incompatible type (e.g., integer) without an appropriate
transform.

This will cause a runtime error from Crossplane's patch engine, with a
confusing error message.

Fix: Add a transform (e.g., convert: { toType: integer }) to the patch, or
change the source/target fields to compatible types.`,

	"XPC006": `XPC006: Argo sync-wave ordering violation

Crossplane resources have implicit ordering requirements:
- XRD must be Established before any XR of its kind
- Function must be Healthy before any Composition using it
- Provider must be Healthy before its MRDs are usable
- Composition must exist before XRs of its referenced type

When using Argo CD sync waves, these ordering constraints must be reflected
in the wave numbers. A violation means resources may be applied before their
dependencies are ready.

Fix: Adjust sync-wave annotations to respect the dependency ordering.`,

	"XPC007": `XPC007: Argo label tracking conflicts with Crossplane

When Argo CD uses label-based tracking (argocd.argoproj.io/tracking-method: label),
it conflicts with Crossplane's label propagation. Crossplane propagates labels
to managed resources, causing Argo to think it owns them and either prune them
or fight Crossplane for ownership.

See: crossplane/crossplane#2121, crossplane/crossplane#2928

Fix: Switch Argo CD tracking mode to annotation:
  metadata.annotations["argocd.argoproj.io/tracking-method"]: "annotation"`,

	"XPC008": `XPC008: v1-style machinery fields with v2 XRD

A resource targeting a Crossplane v2 XRD uses top-level machinery fields
(publishConnectionDetailsTo, compositionRef, compositionSelector, etc.)
instead of placing them under spec.crossplane.

In Crossplane v2, machinery fields moved to spec.crossplane. Using v1-style
placement with a v2 XRD will cause the fields to be silently ignored.

Fix: Move machinery fields under spec.crossplane. See the Crossplane v2
migration guide.`,

	"XPC009": `XPC009: required resource not bootstrappable

A Composition pipeline step references a required resource that may not exist
on first reconcile. The resource isn't produced by an earlier pipeline step
and isn't a well-known cluster resource.

This can cause the pipeline to fail on initial creation of the composite
resource, requiring manual intervention.

Fix: Ensure the required resource is produced by an earlier pipeline step,
or mark it with annotation xpc.dev/accept-bootstrap-gap: "true" if the
bootstrap gap is intentional.`,
}
