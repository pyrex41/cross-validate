// xpc is a type checker for Crossplane + Argo CD configurations.
// It catches structural and operational bugs in the relationships between
// CRDs, Compositions, Functions, and Argo Applications before they reach
// a cluster.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/pyrex41/cross-validate-/pkg/audit"
	"github.com/pyrex41/cross-validate-/pkg/bisect"
	"github.com/pyrex41/cross-validate-/pkg/checker"
	"github.com/pyrex41/cross-validate-/pkg/config"
	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/plan"
	"github.com/pyrex41/cross-validate-/pkg/report"
	"github.com/pyrex41/cross-validate-/pkg/snapshot"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Honour XPC_CPUPROFILE for ad-hoc profiling. Set the env var to a path,
	// run any subcommand, and pprof output is written there. Off by default.
	// We stop+close before os.Exit because os.Exit skips deferred funcs.
	var profStop func()
	if path := os.Getenv("XPC_CPUPROFILE"); path != "" {
		if f, err := os.Create(path); err == nil {
			_ = pprof.StartCPUProfile(f)
			profStop = func() { pprof.StopCPUProfile(); _ = f.Close() }
		}
	}
	exit := func(code int) {
		if profStop != nil {
			profStop()
		}
		os.Exit(code)
	}

	switch os.Args[1] {
	case "check":
		exit(runCheck(os.Args[2:]))
	case "dump-ir":
		os.Exit(runDumpIR(os.Args[2:]))
	case "snapshot":
		os.Exit(runSnapshot(os.Args[2:]))
	case "verify":
		os.Exit(runVerify(os.Args[2:]))
	case "proof":
		os.Exit(runProof(os.Args[2:]))
	case "bisect":
		os.Exit(runBisect(os.Args[2:]))
	case "plan":
		os.Exit(runPlan(os.Args[2:]))
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
  xpc snapshot [flags] [<path>]    Capture cluster type environment snapshot
  xpc verify <proof-file>          Verify a proof file
  xpc proof <subcommand>           Proof operations (show, diff)
  xpc bisect [flags] [<path>]      Find the commit that flipped a rule
  xpc plan [flags] <path>          Diff two refs; report destructive changes
  xpc explain <code>               Show docs for an error code (e.g., XPC002)
  xpc version                      Print version

Check flags:
  --format=<fmt>       Output format: agent, human, json, sarif (default: agent)
  --strict-conversions Refuse webhook conversions entirely
  --proof              Generate a proof file alongside the check
  --snapshot=<path>    Use a specific snapshot file
  --skip-render        Skip Helm/Kustomize rendering (emits one info diagnostic per skipped Application)
  --helm-bin=<path>    Path to the helm binary (default: first 'helm' on PATH)
  --helm-cache-dir=<dir>   Dir for remote Helm charts + render cache (enables remote)
  --kustomize-bin=<path>   Path to the kustomize binary (default: first 'kustomize' on PATH)
  --crossplane-bin=<path>  Path to the crossplane binary (default: first 'crossplane' on PATH)
                           Used by the composition-render pass; absent binary
                           degrades to a warning-severity XPC.H.composition-renders.
  --skip-appset-expand Skip ApplicationSet generator expansion
  --appset-fixture=<file>  YAML fixture for pullRequest/scmProvider generators
                             (shape: {appset-name: [{key: value, ...}]})
  --kernel-path=<dir>  Explicit path to the Shen kernel directory (overrides
                       upward cwd search; also XPC_KERNEL_PATH env var)
  --config=<path>      Explicit path to xpc.yaml (overrides upward cwd
                       search; also XPC_CONFIG_PATH env var)
  --ssa-mp-mode=<mode> R22 (ServerSideApply × managementPolicies) strictness:
                       observe (default, narrow), partial, any (broadest)
  --focus=<preset>     Restrict to a curated rule subset. Presets:
                       all (default), inc6-floor (R23+R24+R25 only)

Snapshot flags:
  --output=<path>      Output snapshot to file (default: stdout digest)
  --cluster=<name>     Name of the cluster context (default: current)
  --diff=<a>,<b>       Diff two snapshot files
  --include-resources  Include resource instances and Argo objects in the snapshot

Proof subcommands:
  xpc proof show <proof-file>              Show proof summary
  xpc proof diff <proof-a> <proof-b>       Diff two proofs
  xpc proof show --rule=<id> <proof-file>  Show a specific rule's judgments

Bisect flags:
  --rule=<code>        Rule code to bisect (e.g., XPC002, XPC.A.resource-field-valid)
  --good=<ref>         Known-good git ref
  --bad=<ref>          Known-bad git ref (default: HEAD)
  --xpc-bin=<path>     xpc binary used inside the loop (default: this binary)
  --kernel-path=<dir>  Pass-through to inner xpc check
  --config=<path>      Pass-through to inner xpc check
  Inner check runs with --skip-render; --proof / --snapshot are NOT
  forwarded (bisect cares only about rule firing state).

Plan flags:
  --base=<ref>         Base git ref, directory, or .xpcsnap file
  --head=<ref>         Head git ref, directory, or .xpcsnap file (default: HEAD)
  --format=<fmt>       Output format: json, markdown (default: markdown)
  --kernel-path=<dir>  Explicit kernel directory (as in 'check')
  --config=<path>      Explicit xpc.yaml path (as in 'check'; overrides
                       per-variant in-repo discovery)
  --post-comment=<spec>
                       Post Markdown to GitLab MR or GitHub PR. Spec
                       forms: gitlab://group/proj/-/merge_requests/N,
                       github://owner/repo/pull/N, or 'auto' (CI env).
  --dry-run            Resolve --post-comment target and report bytes
                       without posting. Plan still runs and prints.
  --post-comment-required
                       Promote a posting failure to exit 1. Default
                       false: posting failure logs but doesn't gate.
  (most 'check' flags pass through: --helm-bin, --helm-cache-dir, --skip-render,
   --skip-appset-expand, --appset-fixture, --ssa-mp-mode)

Examples:
  xpc plan --base=main --head=HEAD ./deploy
  xpc plan --base=main --head=HEAD --format=json ./deploy
  xpc check ./manifests
  xpc check --format=sarif ./manifests > results.sarif
  xpc check --proof --snapshot=prod.xpcsnap ./manifests
  xpc snapshot --output=prod.xpcsnap ./manifests
  xpc snapshot --diff=a.xpcsnap,b.xpcsnap
  xpc verify proof.xpcproof
  xpc proof show proof.xpcproof
  xpc proof diff before.xpcproof after.xpcproof
  xpc bisect --rule=XPC002 --good=v1.4.2 --bad=HEAD
  xpc dump-ir ./manifests
  xpc explain XPC002
`)
}

func runCheck(args []string) int {
	format := report.FormatAgent
	strictConversions := false
	generateProof := false
	snapshotPath := ""
	skipRender := false
	helmBin := ""
	kustomizeBin := ""
	crossplaneBin := ""
	appsetFixturePath := ""
	helmCacheDir := ""
	skipAppSetExpand := false
	kernelPath := os.Getenv("XPC_KERNEL_PATH")
	configPath := os.Getenv("XPC_CONFIG_PATH")
	ssaMPMode := "observe"
	focusPreset := "all"
	var paths []string

	for _, arg := range args {
		switch {
		case arg == "--strict-conversions":
			strictConversions = true
		case arg == "--proof":
			generateProof = true
		case arg == "--skip-render":
			skipRender = true
		case arg == "--skip-appset-expand":
			skipAppSetExpand = true
		case len(arg) > 9 && arg[:9] == "--format=":
			format = report.Format(arg[9:])
		case len(arg) > 11 && arg[:11] == "--snapshot=":
			snapshotPath = arg[11:]
		case len(arg) > 11 && arg[:11] == "--helm-bin=":
			helmBin = arg[11:]
		case len(arg) > 16 && arg[:16] == "--kustomize-bin=":
			kustomizeBin = arg[16:]
		case strings.HasPrefix(arg, "--crossplane-bin="):
			crossplaneBin = strings.TrimPrefix(arg, "--crossplane-bin=")
		case len(arg) > 17 && arg[:17] == "--appset-fixture=":
			appsetFixturePath = arg[17:]
		case strings.HasPrefix(arg, "--helm-cache-dir="):
			helmCacheDir = strings.TrimPrefix(arg, "--helm-cache-dir=")
		case strings.HasPrefix(arg, "--kernel-path="):
			kernelPath = strings.TrimPrefix(arg, "--kernel-path=")
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		case strings.HasPrefix(arg, "--ssa-mp-mode="):
			val := strings.TrimPrefix(arg, "--ssa-mp-mode=")
			switch val {
			case "observe", "partial", "any":
				ssaMPMode = val
			default:
				fmt.Fprintf(os.Stderr, "invalid --ssa-mp-mode=%s (must be one of: observe, partial, any)\n", val)
				return 1
			}
		case strings.HasPrefix(arg, "--focus="):
			val := strings.TrimPrefix(arg, "--focus=")
			switch val {
			case "all", "inc6-floor":
				focusPreset = val
			default:
				fmt.Fprintf(os.Stderr, "invalid --focus=%s (must be one of: all, inc6-floor)\n", val)
				return 1
			}
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
		// Default to current directory
		paths = append(paths, ".")
	}

	// Resolve user config (xpc.yaml). Precedence: --config > XPC_CONFIG_PATH
	// > discovery from cwd > Default(). An exe-dir fallback emits one stderr
	// line so the lookup isn't silent — same shape as kernel-path discovery.
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving current directory: %v\n", err)
		return 1
	}
	cfg, cfgPath, viaExe, err := config.Resolve(configPath, "", cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading xpc.yaml: %v\n", err)
		return 1
	}
	if viaExe && cfgPath != "" {
		fmt.Fprintf(os.Stderr, "info: xpc.yaml discovered via exe-dir fallback at %s\n", cfgPath)
	}

	// Load documents
	timing := os.Getenv("XPC_TIMING") != ""
	timed := func(name string, t0 time.Time) {
		if timing {
			fmt.Fprintf(os.Stderr, "  [timing] %-12s %v\n", name, time.Since(t0))
		}
	}
	tLoad := time.Now()
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
	timed("load", tLoad)

	if len(allDocs) == 0 {
		fmt.Fprintln(os.Stderr, "no YAML documents found")
		return 1
	}

	// Build IR
	builder := ir.NewBuilder()
	builder.SkipRender = skipRender
	builder.HelmBin = helmBin
	builder.HelmCacheDir = helmCacheDir
	builder.KustomizeBin = kustomizeBin
	builder.CrossplaneBin = crossplaneBin
	builder.SkipAppSetExpand = skipAppSetExpand
	builder.SSAMPMode = ssaMPMode
	builder.Config = cfg
	if appsetFixturePath != "" {
		fixtures, fxErr := loadAppSetFixtures(appsetFixturePath)
		if fxErr != nil {
			fmt.Fprintf(os.Stderr, "error loading --appset-fixture=%s: %v\n", appsetFixturePath, fxErr)
			return 1
		}
		builder.AppSetContext.PRFixtures = fixtures
	}
	tBuild := time.Now()
	world, err := builder.Build(allDocs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building IR: %v\n", err)
		return 1
	}
	timed("build-ir", tBuild)

	var loadedSnapshot *snapshot.Snapshot

	// If a snapshot is provided, merge its type environment into the world
	if snapshotPath != "" {
		snap, err := snapshot.Load(snapshotPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading snapshot: %v\n", err)
			return 1
		}
		if snap.IsStale(snapshot.DefaultTTL) {
			fmt.Fprintln(os.Stderr, "warning: snapshot is stale (older than 15 minutes)")
		}
		mergeSnapshotIntoWorld(world, snap)
		loadedSnapshot = snap
	}

	// Run checker
	checkerCfg := checker.Config{
		StrictConversions: strictConversions,
		KernelPath:        kernelPath,
		RuleAllowlist:     focusPresetAllowlist(focusPreset),
	}

	tCheck := time.Now()
	result := checker.CheckWithObligations(world, checkerCfg)
	timed("check", tCheck)
	diags := result.Diagnostics

	// Merge in any info-level diagnostics the AppSet expander emitted
	// (unsupported generator kinds, unrenderable templates). These arrive
	// on builder.ExpansionDiags because the kernel never sees AppSets
	// directly — we synthesize their Applications in pkg/ir.
	diags = append(diags, builder.ExpansionDiags...)

	// When rendering is skipped, surface one info diagnostic per
	// Application that had a Helm or Kustomize source we did not render.
	// CI runs without helm/kustomize on PATH use this to know what
	// coverage they missed.
	if skipRender {
		for _, app := range world.ArgoApps {
			helmSeen := false
			kustSeen := false
			for _, src := range app.Sources {
				switch src.Renderer {
				case types.RendererHelm:
					if !helmSeen {
						diags = append(diags, types.Diagnostic{
							Code:     "XPC.H.helm-renders",
							Severity: types.SeverityInfo,
							Message:  fmt.Sprintf("%s: helm render skipped (--skip-render set)", app.Name),
							Source:   app.Source,
						})
						helmSeen = true
					}
				case types.RendererKustomize:
					if !kustSeen {
						diags = append(diags, types.Diagnostic{
							Code:     "XPC.H.kustomize-renders",
							Severity: types.SeverityInfo,
							Message:  fmt.Sprintf("%s: kustomize render skipped (--skip-render set)", app.Name),
							Source:   app.Source,
						})
						kustSeen = true
					}
				}
			}
		}
	}

	// Report
	if err := report.ReportStdout(diags, format); err != nil {
		fmt.Fprintf(os.Stderr, "error writing report: %v\n", err)
		return 1
	}

	// Generate proof if requested
	if generateProof {
		irDigest := ir.DigestWorld(world)
		snapDigest := ""
		if loadedSnapshot != nil {
			snapDigest = loadedSnapshot.Digest
		}

		summary := &audit.RunSummary{
			TotalObligations: result.TotalObligations,
			Satisfied:        result.Satisfied,
			Violated:         result.Violated,
			ObligationIDs:    result.ObligationIDs,
		}
		var (
			rulesetDigest string
			digestErr     error
		)
		if kernelPath != "" {
			rulesetDigest, digestErr = audit.ComputeRulesetDigest(kernelPath)
		} else {
			rulesetDigest, digestErr = audit.ComputeEmbeddedRulesetDigest()
		}
		if digestErr != nil {
			fmt.Fprintf(os.Stderr, "error computing ruleset digest: %v\n", digestErr)
			return 1
		}
		p := audit.GenerateWithRulesetDigest(diags, summary, irDigest, snapDigest, rulesetDigest)
		proofPath := "check.xpcproof"
		if err := p.Save(proofPath); err != nil {
			fmt.Fprintf(os.Stderr, "error saving proof: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "proof written to %s (root: %s)\n", proofPath, p.RootDigest[:20])
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

func runSnapshot(args []string) int {
	outputPath := ""
	clusterName := "local"
	diffPaths := ""
	var includeResources bool
	var paths []string

	for _, arg := range args {
		switch {
		case len(arg) > 9 && arg[:9] == "--output=":
			outputPath = arg[9:]
		case len(arg) > 10 && arg[:10] == "--cluster=":
			clusterName = arg[10:]
		case len(arg) > 7 && arg[:7] == "--diff=":
			diffPaths = arg[7:]
		case arg == "--include-resources":
			includeResources = true
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

	// Handle diff mode
	if diffPaths != "" {
		parts := strings.SplitN(diffPaths, ",", 2)
		if len(parts) != 2 {
			fmt.Fprintln(os.Stderr, "error: --diff requires two comma-separated paths")
			return 1
		}
		a, err := snapshot.Load(parts[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading snapshot %s: %v\n", parts[0], err)
			return 1
		}
		b, err := snapshot.Load(parts[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading snapshot %s: %v\n", parts[1], err)
			return 1
		}
		fmt.Print(snapshot.Diff(a, b))
		return 0
	}

	// Snapshot from manifests (filesystem mode)
	if len(paths) == 0 {
		paths = append(paths, ".")
	}

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

	builder := ir.NewBuilder()
	world, err := builder.Build(allDocs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building IR: %v\n", err)
		return 1
	}

	snap := snapshot.FromWorldWithOptions(world, clusterName,
		snapshot.FromWorldOptions{IncludeResources: includeResources})

	if outputPath != "" {
		if err := snap.Save(outputPath); err != nil {
			fmt.Fprintf(os.Stderr, "error saving snapshot: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "snapshot written to %s\n", outputPath)
	}

	fmt.Println(snap.Digest)
	return 0
}

func runVerify(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: no proof file specified")
		return 1
	}

	proofPath := args[0]
	p, err := audit.LoadProof(proofPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading proof: %v\n", err)
		return 1
	}

	if p.Verify() {
		fmt.Printf("proof verified: %s\n", p.RootDigest[:20])
		fmt.Printf("  IR digest:       %s\n", p.Metadata.IRDigest)
		fmt.Printf("  Snapshot digest:  %s\n", p.Metadata.SnapshotDigest)
		fmt.Printf("  Kernel version:   %s\n", p.Metadata.KernelVersion)
		fmt.Printf("  Ruleset version:  %s\n", p.Metadata.RulesetVersion)
		fmt.Printf("  Timestamp:        %s\n", p.Metadata.Timestamp.Format(time.RFC3339))
		return 0
	}

	fmt.Fprintln(os.Stderr, "proof verification FAILED: Merkle root mismatch")
	return 1
}

func runProof(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: proof subcommand required (show, diff)")
		return 1
	}

	switch args[0] {
	case "show":
		return runProofShow(args[1:])
	case "diff":
		return runProofDiff(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown proof subcommand: %s\n", args[0])
		return 1
	}
}

func runProofShow(args []string) int {
	ruleFilter := ""
	var proofPath string

	for _, arg := range args {
		switch {
		case len(arg) > 7 && arg[:7] == "--rule=":
			ruleFilter = arg[7:]
		case len(arg) > 0 && arg[0] == '-':
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			return 1
		default:
			proofPath = arg
		}
	}

	if proofPath == "" {
		fmt.Fprintln(os.Stderr, "error: no proof file specified")
		return 1
	}

	p, err := audit.LoadProof(proofPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading proof: %v\n", err)
		return 1
	}

	if ruleFilter != "" {
		st, ok := p.RuleSubtrees[ruleFilter]
		if !ok {
			fmt.Fprintf(os.Stderr, "rule %s not found in proof\n", ruleFilter)
			return 1
		}
		fmt.Printf("Rule %s (digest: %s)\n", st.RuleID, st.Digest[:20])
		if len(st.Judgments) == 0 {
			fmt.Println("  No judgments (all resources satisfy this rule)")
		} else {
			for _, j := range st.Judgments {
				fmt.Printf("  [%s] %s: %s\n", j.Status, j.Resource, j.Message)
			}
		}
		return 0
	}

	fmt.Print(p.Summary())
	return 0
}

func runProofDiff(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "error: two proof files required")
		return 1
	}

	a, err := audit.LoadProof(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading proof %s: %v\n", args[0], err)
		return 1
	}
	b, err := audit.LoadProof(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading proof %s: %v\n", args[1], err)
		return 1
	}

	fmt.Print(audit.DiffProofs(a, b))
	return 0
}

func runBisect(args []string) int {
	ruleCode := ""
	goodRef := ""
	badRef := "HEAD"
	manifestPath := ""
	xpcBin := ""
	kernelPath := os.Getenv("XPC_KERNEL_PATH")
	configPath := os.Getenv("XPC_CONFIG_PATH")

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--rule="):
			ruleCode = strings.TrimPrefix(arg, "--rule=")
		case strings.HasPrefix(arg, "--good="):
			goodRef = strings.TrimPrefix(arg, "--good=")
		case strings.HasPrefix(arg, "--bad="):
			badRef = strings.TrimPrefix(arg, "--bad=")
		case strings.HasPrefix(arg, "--xpc-bin="):
			xpcBin = strings.TrimPrefix(arg, "--xpc-bin=")
		case strings.HasPrefix(arg, "--kernel-path="):
			kernelPath = strings.TrimPrefix(arg, "--kernel-path=")
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--help" || arg == "-h":
			printUsage()
			return 0
		case len(arg) > 0 && arg[0] == '-':
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			return 1
		default:
			// Positional path: where to run `xpc check` inside each
			// materialized worktree. Defaults to cwd if omitted.
			if manifestPath != "" {
				fmt.Fprintln(os.Stderr, "error: xpc bisect accepts at most one path")
				return 1
			}
			manifestPath = arg
		}
	}

	if ruleCode == "" {
		fmt.Fprintln(os.Stderr, "error: --rule is required")
		return 1
	}
	if goodRef == "" {
		fmt.Fprintln(os.Stderr, "error: --good is required")
		return 1
	}
	if err := bisect.ValidateRuleCode(ruleCode); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Resolve the xpc binary used inside the bisect loop. We default to the
	// running executable so the inner check uses the same build the user
	// invoked. --xpc-bin lets ops override (e.g., to bisect against a known-
	// stable xpc while the current build is unstable).
	if xpcBin == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: resolve xpc binary: %v\n", err)
			return 1
		}
		xpcBin = exe
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getwd: %v\n", err)
		return 1
	}

	// Pass through kernel/config flags to the inner `xpc check` so the
	// rule we're bisecting on actually evaluates the same way it did
	// originally. We deliberately do not pass --proof or --snapshot —
	// bisect cares only about whether the rule code appears.
	var extraArgs []string
	if kernelPath != "" {
		extraArgs = append(extraArgs, "--kernel-path="+kernelPath)
	}
	if configPath != "" {
		extraArgs = append(extraArgs, "--config="+configPath)
	}

	detector := bisect.XPCCheckDetector(xpcBin, ruleCode, extraArgs)

	res, err := bisect.Run(bisect.Options{
		RuleCode:     ruleCode,
		GoodRef:      goodRef,
		BadRef:       badRef,
		RepoRoot:     cwd,
		ManifestPath: manifestPath,
		CheckRule:    detector,
	})
	if err != nil {
		switch {
		case errors.Is(err, bisect.ErrSameCommit):
			fmt.Fprintln(os.Stderr, "error: --good and --bad resolve to the same commit; nothing to bisect")
			return 1
		case errors.Is(err, bisect.ErrRuleNeverChanged):
			fmt.Fprintf(os.Stderr,
				"rule %s never changed firing state between %s and %s\n",
				ruleCode, goodRef, badRef)
			fmt.Fprintln(os.Stderr, "either both refs fire it, or neither does — bisect has nothing to find")
			return 1
		default:
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	verb := "started firing"
	if res.Direction == bisect.StoppedFiring {
		verb = "stopped firing"
	}
	fmt.Printf("rule %s %s at:\n", ruleCode, verb)
	fmt.Printf("  commit:  %s\n", res.Commit)
	fmt.Printf("  subject: %s\n", res.Subject)
	fmt.Printf("  good:    %s (%s)\n", goodRef, res.GoodSHA[:12])
	fmt.Printf("  bad:     %s (%s)\n", badRef, res.BadSHA[:12])
	fmt.Printf("  steps:   %d\n", res.Steps)
	return 0
}

func runPlan(args []string) int {
	baseRef := ""
	headRef := "HEAD"
	format := plan.FormatMarkdown
	skipRender := true // plan runs default to skip-render; callers opt in via --render
	enableRender := false
	helmBin := ""
	helmCacheDir := ""
	kustomizeBin := ""
	crossplaneBin := ""
	appsetFixturePath := ""
	skipAppSetExpand := false
	ssaMPMode := "observe"
	kernelPath := os.Getenv("XPC_KERNEL_PATH")
	configPath := os.Getenv("XPC_CONFIG_PATH")
	strictConversions := false
	var postCommentSpec string
	var dryRun bool
	var postCommentRequired bool
	var paths []string

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--base="):
			baseRef = strings.TrimPrefix(arg, "--base=")
		case strings.HasPrefix(arg, "--head="):
			headRef = strings.TrimPrefix(arg, "--head=")
		case strings.HasPrefix(arg, "--format="):
			f := strings.TrimPrefix(arg, "--format=")
			switch f {
			case "json":
				format = plan.FormatJSON
			case "markdown", "md":
				format = plan.FormatMarkdown
			default:
				fmt.Fprintf(os.Stderr, "unknown --format=%s (want json or markdown)\n", f)
				return 1
			}
		case arg == "--render":
			enableRender = true
		case arg == "--skip-render":
			skipRender = true
		case strings.HasPrefix(arg, "--helm-bin="):
			helmBin = strings.TrimPrefix(arg, "--helm-bin=")
		case strings.HasPrefix(arg, "--helm-cache-dir="):
			helmCacheDir = strings.TrimPrefix(arg, "--helm-cache-dir=")
		case strings.HasPrefix(arg, "--kustomize-bin="):
			kustomizeBin = strings.TrimPrefix(arg, "--kustomize-bin=")
		case strings.HasPrefix(arg, "--crossplane-bin="):
			crossplaneBin = strings.TrimPrefix(arg, "--crossplane-bin=")
		case strings.HasPrefix(arg, "--appset-fixture="):
			appsetFixturePath = strings.TrimPrefix(arg, "--appset-fixture=")
		case arg == "--skip-appset-expand":
			skipAppSetExpand = true
		case strings.HasPrefix(arg, "--ssa-mp-mode="):
			val := strings.TrimPrefix(arg, "--ssa-mp-mode=")
			switch val {
			case "observe", "partial", "any":
				ssaMPMode = val
			default:
				fmt.Fprintf(os.Stderr, "invalid --ssa-mp-mode=%s\n", val)
				return 1
			}
		case strings.HasPrefix(arg, "--kernel-path="):
			kernelPath = strings.TrimPrefix(arg, "--kernel-path=")
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--strict-conversions":
			strictConversions = true
		case strings.HasPrefix(arg, "--post-comment="):
			postCommentSpec = arg[len("--post-comment="):]
		case arg == "--dry-run":
			dryRun = true
		case arg == "--post-comment-required":
			postCommentRequired = true
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

	var commentTarget plan.CommentTarget
	if postCommentSpec != "" {
		t, err := plan.ParseCommentSpec(postCommentSpec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: --post-comment: %v\n", err)
			return 1
		}
		commentTarget = t
	}

	if baseRef == "" {
		fmt.Fprintln(os.Stderr, "error: --base is required")
		return 1
	}
	if len(paths) == 0 {
		paths = append(paths, ".")
	}
	if len(paths) > 1 {
		fmt.Fprintln(os.Stderr, "error: xpc plan accepts at most one path")
		return 1
	}

	if enableRender {
		skipRender = false
	}

	// Resolve --config / XPC_CONFIG_PATH once. If neither is set, plan.Run
	// discovers xpc.yaml inside each variant worktree (per design §3.c).
	var explicitCfg *config.Config
	if configPath != "" {
		c, lerr := config.Load(configPath)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "error loading xpc.yaml: %v\n", lerr)
			return 1
		}
		explicitCfg = c
	}

	cfg := plan.Config{
		BaseRef:          baseRef,
		HeadRef:          headRef,
		Path:             paths[0],
		SkipRender:       skipRender,
		HelmBin:          helmBin,
		HelmCacheDir:     helmCacheDir,
		KustomizeBin:     kustomizeBin,
		CrossplaneBin:    crossplaneBin,
		SkipAppSetExpand: skipAppSetExpand,
		SSAMPMode:        ssaMPMode,
		ConfigOverride:   explicitCfg,
		CheckerConfig: checker.Config{
			StrictConversions: strictConversions,
			KernelPath:        kernelPath,
		},
	}

	if appsetFixturePath != "" {
		fixtures, err := loadAppSetFixtures(appsetFixturePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading --appset-fixture=%s: %v\n", appsetFixturePath, err)
			return 1
		}
		cfg.AppSetFixtures = fixtures
	}

	p, cleanup, err := plan.Run(cfg)
	defer cleanup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "plan failed: %v\n", err)
		return 1
	}

	var body bytes.Buffer
	switch format {
	case plan.FormatJSON:
		if err := plan.WriteJSON(&body, p); err != nil {
			fmt.Fprintf(os.Stderr, "write json: %v\n", err)
			return 1
		}
	case plan.FormatMarkdown:
		if err := plan.WriteMarkdown(&body, p); err != nil {
			fmt.Fprintf(os.Stderr, "write markdown: %v\n", err)
			return 1
		}
	}
	if _, err := os.Stdout.Write(body.Bytes()); err != nil {
		fmt.Fprintf(os.Stderr, "write stdout: %v\n", err)
		return 1
	}

	if postCommentSpec != "" {
		if dryRun {
			fmt.Fprintf(os.Stderr, "would post %d bytes to %s\n",
				body.Len(), plan.DescribeTarget(commentTarget))
		} else {
			if err := plan.PostComment(commentTarget, body.String()); err != nil {
				fmt.Fprintf(os.Stderr, "post-comment failed: %v\n", err)
				if postCommentRequired {
					return 1
				}
			} else {
				fmt.Fprintf(os.Stderr, "posted comment to %s\n",
					plan.DescribeTarget(commentTarget))
			}
		}
	}

	// Exit non-zero when destructive section is non-empty (R26, forthcoming).
	for _, d := range p.Diagnostics {
		if strings.HasPrefix(d.Code, "XPC.P.") && d.Severity == types.SeverityError {
			return 1
		}
	}
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
		fmt.Fprintln(os.Stderr, "\nKnown error codes: XPC001-XPC011, XPC.A.resource-field-valid, XPC.D.kind-whitelisted, XPC.E.appset-finalizer-without-preserve, XPC.E.prod-appset-autosync, XPC.E.selector-needs-ignore-diff, XPC.H.composition-renders, XPC.H.helm-renders, XPC.H.values-well-typed, XPC.P.cascade-risk, XPC.P.destructive-delete, XPC.P.immutable-change, XPC.S.crossplane-state-needs-orphan")
		return 1
	}

	fmt.Println(explanation)
	return 0
}

// mergeSnapshotIntoWorld merges snapshot type environment data into a World.
// The snapshot provides CRDs, providers, functions etc. that may not be
// present in the manifest files being checked.
// loadAppSetFixtures parses a YAML file that supplies PR-stub parameter
// sets for ApplicationSets whose generators hit remote APIs
// (pullRequest, scmProvider). The file's top-level shape is:
//
//	appset-name:
//	  - number: 42
//	    branch: feature/x
//	    headSha: abc123
//	  - number: 43
//	    ...
//
// Any non-string value is coerced via fmt.Sprintf("%v", …) so integers
// like `number: 42` survive the type-mismatch — ApplicationSet templates
// consume these as plain {{ .number }} substitutions, which our minimal
// engine does as strings anyway.
// focusPresetAllowlist maps a --focus preset to the kernel rule codes the
// run is restricted to. "all" returns nil so the kernel runs everything.
func focusPresetAllowlist(preset string) []string {
	switch preset {
	case "inc6-floor":
		return []string{
			"XPC.S.crossplane-state-needs-orphan",
			"XPC.E.appset-finalizer-without-preserve",
			"XPC.E.prod-appset-autosync",
		}
	default:
		return nil
	}
}

func loadAppSetFixtures(path string) (map[string][]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string][]map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decoding YAML: %w", err)
	}
	out := make(map[string][]map[string]string, len(raw))
	for appset, prs := range raw {
		for _, pr := range prs {
			stringified := make(map[string]string, len(pr))
			for k, v := range pr {
				stringified[k] = fmt.Sprintf("%v", v)
			}
			out[appset] = append(out[appset], stringified)
		}
	}
	return out, nil
}

func mergeSnapshotIntoWorld(w *types.World, snap *snapshot.Snapshot) {
	// Add CRDs from snapshot that aren't already in the world
	existingCRDs := make(map[string]bool)
	for _, crd := range w.CRDs {
		existingCRDs[crd.Group+"/"+crd.Kind] = true
	}
	for _, crd := range snap.CRDs {
		key := crd.Group + "/" + crd.Kind
		if !existingCRDs[key] {
			w.CRDs = append(w.CRDs, crd)
		}
	}

	// Add XRDs from snapshot
	existingXRDs := make(map[string]bool)
	for _, xrd := range w.XRDs {
		existingXRDs[xrd.Group+"/"+xrd.Kind] = true
	}
	for _, xrd := range snap.XRDs {
		key := xrd.Group + "/" + xrd.Kind
		if !existingXRDs[key] {
			w.XRDs = append(w.XRDs, xrd)
		}
	}

	// Add Functions from snapshot
	existingFns := make(map[string]bool)
	for _, fn := range w.Functions {
		existingFns[fn.Name] = true
	}
	for _, fn := range snap.Functions {
		if !existingFns[fn.Name] {
			w.Functions = append(w.Functions, fn.FunctionInfo)
		}
	}

	// Add Providers from snapshot
	existingProvs := make(map[string]bool)
	for _, p := range w.Providers {
		existingProvs[p.Name] = true
	}
	for _, p := range snap.Providers {
		if !existingProvs[p.Name] {
			w.Providers = append(w.Providers, p.ProviderInfo)
		}
	}

	// Merge schemas
	for digest, schema := range snap.Schemas {
		if _, ok := w.Schemas[digest]; !ok {
			w.Schemas[digest] = schema
		}
	}

	// Add Resources from snapshot. Identity tuple is
	// (apiVersion, kind, namespace, name); manifest-side wins on conflict.
	existingResources := make(map[string]bool)
	for _, r := range w.Resources {
		existingResources[r.APIVersion+"|"+r.Kind+"|"+r.Namespace+"|"+r.Name] = true
	}
	for _, r := range snap.Resources {
		key := r.APIVersion + "|" + r.Kind + "|" + r.Namespace + "|" + r.Name
		if !existingResources[key] {
			w.Resources = append(w.Resources, r)
		}
	}

	// Add Argo Applications from snapshot. Identity is (namespace, name).
	existingArgoApps := make(map[string]bool)
	for _, a := range w.ArgoApps {
		existingArgoApps[a.Namespace+"|"+a.Name] = true
	}
	for _, a := range snap.ArgoApps {
		key := a.Namespace + "|" + a.Name
		if !existingArgoApps[key] {
			w.ArgoApps = append(w.ArgoApps, a)
		}
	}

	// Add Argo ApplicationSets from snapshot. Identity is
	// (template.namespace, name) — AppSet itself is namespace-implicit, so
	// the template's target namespace is the discriminator.
	existingArgoAppSets := make(map[string]bool)
	for _, s := range w.ArgoAppSets {
		existingArgoAppSets[s.Template.Namespace+"|"+s.Name] = true
	}
	for _, s := range snap.ArgoAppSets {
		key := s.Template.Namespace + "|" + s.Name
		if !existingArgoAppSets[key] {
			w.ArgoAppSets = append(w.ArgoAppSets, s)
		}
	}

	// Add Argo AppProjects from snapshot. AppProjects are cluster-scoped in
	// practice (always argocd namespace), so name alone is the identity.
	existingArgoProjects := make(map[string]bool)
	for _, p := range w.ArgoProjects {
		existingArgoProjects[p.Name] = true
	}
	for _, p := range snap.ArgoProjects {
		if !existingArgoProjects[p.Name] {
			w.ArgoProjects = append(w.ArgoProjects, p)
		}
	}
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

	"XPC010": `XPC010: secret taint leak

A patch flows secret/credential material from a tainted source field to a
non-secret destination. This can expose sensitive data in fields where it
could be logged, displayed in status, or read by unprivileged controllers.

Connection details, passwords, API keys, and similar credential material
should only flow to SecretRef fields or other explicitly secret-typed
destinations.

Fix: Route the secret through a SecretRef field, or add annotation
xpc.dev/declassify to acknowledge the taint leak is intentional.`,

	"XPC011": `XPC011: temporal validity / deprecated feature

A resource or configuration uses a feature (API version, provider version,
CRD version) that is deprecated or approaching end-of-life.

This is a forward-looking warning: the configuration works today but will
break at a known future date. Combined with the proof system, this turns
daily snapshots into a continuous compliance evidence stream that warns
before something expires.

Fix: Migrate to the recommended replacement before the deprecation deadline.`,

	"XPC.E.selector-needs-ignore-diff": `XPC.E.selector-needs-ignore-diff: selector field has no ignoreDifferences coverage

A Crossplane managed resource has a *Selector field set (e.g. vpcIdSelector,
subnetIdSelector, securityGroupSelector). Crossplane resolves selectors at
runtime by writing the concrete value into a sibling field — the "resolved path"
(e.g. vpcId, subnetIds, securityGroupIds). Argo CD sees this late-init write as
unwanted drift: the resolved field was not in the original Git manifest, so Argo
treats it as a live-state deviation and will continuously try to remove it on
every sync. This creates a permanent Crossplane vs. Argo fight.

The fix is to add an ignoreDifferences entry to the owning Application that
covers the resolved path. Both jsonPointers (JSON Pointer form, e.g.
/spec/forProvider/vpcId) and jqPathExpressions are supported.

Rule scope: this first pass checks scalar (non-array-indexed) selector paths.
Array-indexed paths (e.g. launchTemplate[].idSelector) are present in the
registry but skipped by the enrichment loop pending element-wise walking support
in a follow-up. The registry covers 53 known selector→resolved-path pairs
across 14 Crossplane provider groups.

Fix: add an ignoreDifferences block to the owning Application:
  ignoreDifferences:
    - group: <provider-group>
      kind: <resource-kind>
      jsonPointers:
        - /spec/forProvider/<resolvedFieldName>`,

	"XPC.D.kind-whitelisted": `XPC.D.kind-whitelisted: resource kind not in AppProject whitelist

An Argo CD Application is managed by an AppProject, and that AppProject's
clusterResourceWhitelist or namespaceResourceWhitelist does not include the
kind of one of the resources in the Application.

Argo CD enforces project whitelists at sync time: if a resource kind is not
whitelisted, Argo CD will refuse to create or update that resource. This is a
hard sync failure, not a warning.

Cluster-scoped resources (e.g. ClusterRole, Namespace, CRD) must be listed in
clusterResourceWhitelist. Namespace-scoped resources (e.g. Deployment, Service,
custom resources) must be listed in namespaceResourceWhitelist.

Wildcards: setting group or kind to "*" allows all groups or all kinds
respectively. The entry {group: "*", kind: "*"} permits everything.

Fix: Add the missing kind to the appropriate whitelist in the AppProject, or
move the resource to an Application managed by a project that already allows it.`,

	"XPC.H.helm-renders": `XPC.H.helm-renders: Helm rendering failed

An Argo CD Application has a Helm source that xpc could not render. Without
a successful render, xpc cannot inspect the actual manifests Argo CD will
apply, so downstream rules (selector coverage, field validation, project
whitelist) do not see the rendered resources.

Causes:
- helm binary absent on PATH (severity: warning; install helm or pass
  --helm-bin=<path>).
- Template syntax error, missing values, broken dependency (severity: error;
  reproduce with 'helm template' locally and fix the chart).
- Render exceeds the 30s timeout (severity: error; simplify the chart).

Fix: Depends on the ErrorKind — see the diagnostic detail for the concrete
helm failure message.`,

	"XPC.P.destructive-delete": `XPC.P.destructive-delete: state-bearing Crossplane MR about to be removed across a plan

Emitted only by 'xpc plan --base --head'. A resource whose (Group, Kind) is
in xpc's state-bearing allowlist is present on --base and absent on --head,
and the base-side spec.deletionPolicy is not Orphan. Applying this change
will run a real destructive call against the external cloud object (DROP
DATABASE, DeleteCluster, DeleteKey, DeleteBucket, ...).

This is the dynamic / across-variant counterpart of R23
(XPC.S.crossplane-state-needs-orphan, which catches the configuration on a
single tip). Both fire intentionally when the same commit both (a) declared
a non-Orphan policy and (b) schedules removal.

Fix: one of (a) keep the resource on HEAD (revert the removal from the PR),
(b) edit the base-side manifest to add spec.deletionPolicy: Orphan so the
cascade is non-destructive, (c) add annotation xpc.io/allow-delete=true to
the base manifest (the bypass is recognized on either the primary or the
policy.facilitygrid.io alias) if destruction is genuinely intended.`,

	"XPC.P.immutable-change": `XPC.P.immutable-change: scalar-leaf immutable field changed across a plan

Emitted only by 'xpc plan --base --head'. A resource present on both sides
of the plan has a modified field whose (Group, Kind, FieldPath) is registered
in xpc's immutable-field catalog (pkg/ir/immutable_registry.go). Applying the
change will require the underlying system to destroy and recreate the object:
for state-bearing Crossplane MRs this means DeleteCluster + CreateCluster /
DeleteBucket + CreateBucket / etc., with attendant data loss.

The registry is the source of truth for what counts as "immutable". It
currently covers:

  - Core K8s: Service clusterIP/type, PVC storageClassName/accessModes,
    Job selector/template, StatefulSet serviceName/volumeClaimTemplates
  - rds.aws.upbound.io/Cluster, ClusterInstance (Aurora)
  - docdb.aws.upbound.io/Cluster, ClusterInstance (DocDB)
  - s3.aws.upbound.io/Bucket (name, region, objectLockEnabled)
  - kms.aws.upbound.io/Key (keyUsage, customerMasterKeySpec)
  - ec2.aws.upbound.io/VPC (cidrBlock, instanceTenancy)
  - mysql.sql.crossplane.io/Database (name)

Scalar-leaf paths only — array-indexed and object-block paths are a P5 concern.

Bypass: add annotation xpc.io/allow-immutable-change: "true" on the HEAD
manifest. The bypass lives on the head side because the change author is
the one consenting to the destructive reshape — base-side consent made
sense for R26 (the resource is disappearing on head, so there is no head
side), but here the mutator's intent is what matters.

Fix: one of (a) revert the field change on HEAD, (b) recreate the resource
under a new name/identity so the external system sees a create rather than
an update, (c) add xpc.io/allow-immutable-change=true if the recreate is
genuinely intended (and paired with the necessary data migration).`,

	"XPC.P.cascade-risk": `XPC.P.cascade-risk: ArgoCD Application removal with cascading finalizer

Emitted only by 'xpc plan --base --head'. An argoproj.io Application is
present on --base and absent on --head, the base manifest carries the
cascading finalizer resources-finalizer.argocd.argoproj.io in
metadata.finalizers, and spec.syncPolicy.preserveResourcesOnDeletion is not
true. Removing this Application will cascade DELETE through every resource
it owns. This is the fg-synapse INC-6 trigger applied at the per-Application
level (as opposed to the AppSet-level R24 static check).

Fix: one of (a) keep the Application on HEAD (revert the removal),
(b) set spec.syncPolicy.preserveResourcesOnDeletion: true on the base
manifest before removing the Application, (c) drop the
resources-finalizer.argocd.argoproj.io entry from metadata.finalizers if
cascade is not intended, or (d) add annotation xpc.io/allow-delete=true on
the base side if destruction is genuinely intended.`,

	"XPC.S.crossplane-state-needs-orphan": `XPC.S.crossplane-state-needs-orphan: state-bearing Crossplane MR lacks deletionPolicy: Orphan

A Crossplane managed resource whose (Group, Kind) is on xpc's state-bearing
allowlist declares spec.deletionPolicy anything other than Orphan, OR omits
the field entirely (Crossplane's default is Delete).

The allowlist currently covers kinds whose underlying AWS/SQL/KMS object
holds real external state — destroying the CR runs a real destructive call
against the backing system:

  - rds.aws.upbound.io/Cluster, ClusterInstance          (Aurora)
  - docdb.aws.upbound.io/Cluster, ClusterInstance        (DocDB)
  - mysql.sql.crossplane.io/Database, User, Grant        (SQL data)
  - kms.aws.upbound.io/Key                               (CMK)
  - s3.aws.upbound.io/Bucket                             (objects)
  - ec2.aws.upbound.io/VPC                               (network identity)

This list mirrors fg-manifold's crossplane-state-require-orphan
ValidatingAdmissionPolicy. fg-manifold enforces at runtime; xpc enforces in
CI (across all envs, not just prod).

Rationale: fg-synapse INC-6 (2026-04-22, SEV-2). A cascade delete through
an AppSet-owned Application reached ~70 managed resources; data survived by
pure ordering luck. Setting Orphan decouples the CR lifecycle from the
external object's lifecycle so ArgoCD / Crossplane removals are non-destructive.

Bypass: if destruction is genuinely intended (throwaway test, decommissioning),
add one of these annotations to the resource metadata:
  xpc.io/allow-delete: "true"                  (primary)
  policy.facilitygrid.io/allow-delete: "true"  (alias — matches the runtime VAP)

Resources whose name contains "alb-logs" are carved out — ALB access-log
buckets are separately managed and intentionally destroyable.

Fix: Add spec.deletionPolicy: Orphan to the resource.`,

	"XPC.E.prod-appset-autosync": `XPC.E.prod-appset-autosync: prod-named ApplicationSet enables automated sync

An ArgoCD ApplicationSet whose metadata.name matches a prod pattern (contains
"-prod" or "prod-") enables spec.template.spec.syncPolicy.automated, meaning
every generated Application auto-syncs any git change without a human click.

Rationale: INC-6 (fg-synapse, 2026-04-22) was triggered by an out-of-band
delete, but the same cascade would have landed via any commit that reduced the
generator's output — e.g. a filter change that drops a cluster. With automated
sync enabled, a destructive git change has no human gate. fg-manifold's
remediation (commit a5f77a3b8) dropped spec.template.spec.syncPolicy.automated
from 5 prod AppSets; this rule catches any regression.

R25 pairs with R23 (deletionPolicy) and R24 (preserveResourcesOnDeletion) as
the static floor for INC-6-shape incidents. They describe different properties:
R23/R24 limit the blast radius of a destructive event; R25 limits how easily
one can be triggered.

Name patterns are currently hardcoded. A kernel config file (xpc.yaml) that
surfaces prodAppSetNamePatterns is a P1 follow-up.

Fix: Remove spec.template.spec.syncPolicy.automated from the ApplicationSet
template, forcing manual sync for each generated Application. If automated
sync is genuinely required for a prod AppSet, rename the AppSet so it does
not match the prod pattern, or split into a non-prod-named sibling that
targets the same clusters.`,

	"XPC.E.appset-finalizer-without-preserve": `XPC.E.appset-finalizer-without-preserve: ApplicationSet cascading finalizer without preserveResourcesOnDeletion

An ArgoCD ApplicationSet bakes the ` + "`resources-finalizer.argocd.argoproj.io`" + ` finalizer
into every generated Application via spec.template.metadata.finalizers, but the
AppSet itself does NOT set spec.syncPolicy.preserveResourcesOnDeletion: true.

The combination is the root cause of fg-synapse INC-6 (2026-04-22, SEV-2). When
a generator stops producing a parameter set, or the AppSet is deleted, ArgoCD
cascades the delete to every resource each generated Application owns. If any
of those resources are state-bearing Crossplane managed resources defaulting to
deletionPolicy: Delete, the cascade runs real DROP DATABASE / DeleteCluster /
DeleteVolume calls against production infrastructure.

This rule catches the configuration on a single tip. For detecting the actual
destructive change across a PR — a state-bearing resource about to disappear —
see XPC.P.destructive-delete (xpc plan, forthcoming R26).

Fix: Set spec.syncPolicy.preserveResourcesOnDeletion: true on the ApplicationSet,
OR drop the resources-finalizer.argocd.argoproj.io entry from
spec.template.metadata.finalizers. Preservation is strongly preferred: keeping
the finalizer without preservation is the unsafe combination, not the finalizer
by itself.`,

	"XPC.H.composition-renders": `XPC.H.composition-renders: Crossplane Composition rendering failed

An xpc check that has ` + "`SkipRender=false`" + ` (i.e. no ` + "`--skip-render`" + `) walks every
Crossplane Composition that has an XR to render against, shells out to
'crossplane render <xr> <composition> <functions>', and feeds the rendered
managed resources back into World.Resources with provenance tag
'rendered:composition:<xr-name>'. When that shell-out fails, this diagnostic
fires.

Severity is warning when the crossplane binary is absent (CI machines
without crossplane still complete their other checks, with reduced coverage);
error on template failures or timeouts.

ErrorKind classifiers (surfaced in the diagnostic detail):
  - crossplane-absent         — no crossplane binary on PATH.
  - crossplane-render-failed  — crossplane exited non-zero; detail has stderr.
  - crossplane-timeout        — render exceeded ` + "`CompositionRenderTimeout`" + ` (30s).

Fix: depends on the ErrorKind — see the diagnostic detail for the concrete
message and the fix-hint keyed off the kind.`,

	"XPC.H.values-well-typed": `XPC.H.values-well-typed: Helm values violate values.schema.json

A Helm chart ships a values.schema.json (JSON Schema draft 2020-12), and the
merged values xpc would pass to 'helm template' do not satisfy it. Causes
include a scalar of the wrong JSON type (e.g. "three" for an integer field),
a missing required field, a value outside an enum, or an unknown field when
the schema sets additionalProperties: false.

xpc's values walker reuses the same schema-walker that validates direct
Kubernetes manifests against their CRD/XRD schemas, so the violation shapes
(wrong-type, missing-required, unknown-field, invalid-enum) are the same.

Fix: Either correct the value in the Application's valueFiles / valuesObject /
inline values, or relax the chart's values.schema.json if the constraint is
wrong.`,
}
