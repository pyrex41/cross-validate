package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/checker"
	"github.com/pyrex41/cross-validate-/pkg/config"
	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
)

// Config is the runtime configuration for a plan run. Mirrors checker.Config
// for the per-variant check, plus the plan-specific controls.
type Config struct {
	// BaseRef and HeadRef are the user-supplied --base and --head arguments.
	// Each is interpreted as (in order): an existing directory on disk
	// (hermetic-test path), then a git ref resolved in the repo enclosing
	// Path.
	BaseRef string
	HeadRef string

	// Path is the per-variant root to check. Relative paths are resolved
	// against the current working directory. For git-ref variants, the
	// equivalent subdirectory of the worktree is checked.
	Path string

	// Checker settings passed through to each variant run.
	CheckerConfig checker.Config

	// Builder settings mirrored from runCheck. Each variant builds its
	// own World with the same flags.
	SkipRender       bool
	HelmBin          string
	HelmCacheDir     string
	KustomizeBin     string
	CrossplaneBin    string
	SkipAppSetExpand bool
	SSAMPMode        string
	AppSetFixtures   map[string][]map[string]string

	// ConfigOverride, when non-nil, is the user-extensible knob set
	// resolved from --config / XPC_CONFIG_PATH BEFORE the plan ran. Plan
	// has a per-variant resolution mode too (per design §3.c, HEAD wins
	// for the in-repo discovery), but the caller's explicit flag/env
	// overrides win uniformly across both variants.
	ConfigOverride *config.Config
}

// Run materializes two worktrees (if needed), checks each, computes the
// resource delta, and returns a Plan. Every temporary directory created is
// registered with a cleanup closure in the returned `cleanup` function;
// callers are responsible for invoking it (typically via `defer`).
//
// Cleanup safety: worktree creation and removal use `git worktree add` /
// `git worktree remove --force`; an in-flight panic before cleanup will
// leave a worktree on disk but no repo corruption. A SIGINT handler is NOT
// installed here — callers that want signal-triggered cleanup should wire
// their own (see cmd/xpc/main.go).
func Run(cfg Config) (*Plan, func(), error) {
	cleanups := []func(){}
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	baseDir, baseCleanup, err := resolveVariant(cfg.BaseRef, cfg.Path, "base")
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("resolve base: %w", err)
	}
	if baseCleanup != nil {
		cleanups = append(cleanups, baseCleanup)
	}

	headDir, headCleanup, err := resolveVariant(cfg.HeadRef, cfg.Path, "head")
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("resolve head: %w", err)
	}
	if headCleanup != nil {
		cleanups = append(cleanups, headCleanup)
	}

	baseResult, err := runVariant(cfg, cfg.BaseRef, baseDir)
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("check base: %w", err)
	}
	headResult, err := runVariant(cfg, cfg.HeadRef, headDir)
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("check head: %w", err)
	}

	plan := &Plan{
		Base:  baseResult,
		Head:  headResult,
		Delta: Diff(baseResult.World, headResult.World),
	}
	// R26/R27 read knob-shaped state (bypass keys, immutable-field overlay)
	// off the base World — both variants resolve from the same xpc.yaml
	// inside the repo, so base and head agree.
	bypassKeys := baseResult.World.BypassKeys
	immutables := baseResult.World.ImmutableFields
	plan.Diagnostics = R26DestructiveDelete(plan.Delta, bypassKeys)
	plan.Diagnostics = append(plan.Diagnostics, R27ImmutableChange(plan.Delta, immutables, bypassKeys)...)
	return plan, cleanup, nil
}

// resolveVariant returns the filesystem directory that should be checked for
// a given --base / --head argument, plus an optional cleanup function. If
// ref is an existing directory on disk, it is used as-is with no cleanup.
// Otherwise ref is treated as a git ref resolved against the repo enclosing
// path; a temporary worktree is created and its cleanup returned.
func resolveVariant(ref, path, tag string) (string, func(), error) {
	if ref == "" {
		return "", nil, fmt.Errorf("empty --%s", tag)
	}

	// Hermetic-test path: if ref is a directory that exists, use it.
	if info, err := os.Stat(ref); err == nil && info.IsDir() {
		abs, err := filepath.Abs(ref)
		if err != nil {
			return "", nil, fmt.Errorf("abs(%s): %w", ref, err)
		}
		return abs, nil, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", nil, fmt.Errorf("abs(%s): %w", path, err)
	}
	repoRoot, err := findRepoRoot(absPath)
	if err != nil {
		return "", nil, fmt.Errorf("find repo root from %s: %w", absPath, err)
	}

	// Worktree path contains a hash of (repoRoot, ref, tag) so concurrent
	// plan runs don't collide.
	h := sha256.Sum256([]byte(repoRoot + "\x00" + ref + "\x00" + tag))
	wtParent := filepath.Join(os.TempDir(), "xpc-plan-"+hex.EncodeToString(h[:8]))
	wtDir := filepath.Join(wtParent, tag)
	if err := os.MkdirAll(wtParent, 0o755); err != nil {
		return "", nil, fmt.Errorf("mkdir worktree parent: %w", err)
	}

	// Pre-existing worktree from an aborted earlier run — remove it.
	if _, err := os.Stat(wtDir); err == nil {
		_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", wtDir).Run()
		_ = os.RemoveAll(wtDir)
	}

	cmd := exec.Command("git", "-C", repoRoot, "worktree", "add", "--detach", wtDir, ref)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("git worktree add %s %s: %w\n%s", wtDir, ref, err, string(out))
	}

	cleanup := func() {
		_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", wtDir).Run()
		_ = os.RemoveAll(wtParent)
	}

	// Map the caller's path into the worktree at the same repo-relative path.
	rel, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("rel(%s, %s): %w", repoRoot, absPath, err)
	}
	return filepath.Join(wtDir, rel), cleanup, nil
}

// resolveVariantConfig picks the *config.Config for a single variant. If the
// caller passed an explicit override (--config flag or XPC_CONFIG_PATH env),
// that wins for both variants — same shape as kernel-path. Otherwise we
// discover xpc.yaml inside the variant's worktree (HEAD wins per design
// §3.c). Discovery failure falls through to Default(), matching the
// "absent file is silent" rule.
func resolveVariantConfig(override *config.Config, variantDir string) *config.Config {
	if override != nil {
		return override
	}
	cfg, _, _, err := config.Resolve("", "", variantDir)
	if err != nil {
		// Per-variant config errors are surfaced as a stderr line; the
		// runner continues with defaults so a malformed xpc.yaml on one
		// side doesn't take down the whole plan run. The check-mode
		// caller is the strict path.
		fmt.Fprintf(os.Stderr, "warning: error loading xpc.yaml under %s: %v (using defaults)\n",
			variantDir, err)
		return config.Default()
	}
	return cfg
}

// findRepoRoot walks up from start looking for a `.git` entry. Accepts both
// regular repos and worktrees (where .git is a file). Returns an error if no
// repo is found above start.
func findRepoRoot(start string) (string, error) {
	dir := start
	for {
		_, err := os.Stat(filepath.Join(dir, ".git"))
		if err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .git found above %s", start)
		}
		dir = parent
	}
}

// runVariant loads docs from dir, builds the World, and runs the checker.
// Returns a VariantResult. The builder flags and checker config are copied
// from the Plan Config so each variant gets identical treatment.
func runVariant(cfg Config, ref, dir string) (VariantResult, error) {
	docs, err := loader.LoadDirectory(dir)
	if err != nil {
		return VariantResult{}, fmt.Errorf("load %s: %w", dir, err)
	}

	builder := ir.NewBuilder()
	builder.SkipRender = cfg.SkipRender
	builder.HelmBin = cfg.HelmBin
	builder.HelmCacheDir = cfg.HelmCacheDir
	builder.KustomizeBin = cfg.KustomizeBin
	builder.CrossplaneBin = cfg.CrossplaneBin
	builder.SkipAppSetExpand = cfg.SkipAppSetExpand
	builder.SSAMPMode = cfg.SSAMPMode
	builder.Config = resolveVariantConfig(cfg.ConfigOverride, dir)
	if cfg.AppSetFixtures != nil {
		builder.AppSetContext.PRFixtures = cfg.AppSetFixtures
	}

	world, err := builder.Build(docs)
	if err != nil {
		return VariantResult{}, fmt.Errorf("build IR: %w", err)
	}

	diags, err := checker.Check(world, cfg.CheckerConfig)
	if err != nil {
		return VariantResult{}, fmt.Errorf("check: %w", err)
	}
	diags = append(diags, builder.ExpansionDiags...)

	return VariantResult{
		Ref:         ref,
		ResolvedDir: dir,
		World:       world,
		Diagnostics: diags,
	}, nil
}

// SummarizeDelta returns a one-line counts summary suitable for human /
// markdown output. Example: "added 3, removed 2, modified 11".
func SummarizeDelta(d ResourceDelta) string {
	return fmt.Sprintf("added %d, removed %d, modified %d",
		len(d.Added), len(d.Removed), len(d.Modified))
}

// shortID formats a ResourceIdentity as a human-readable group/kind/name[@app]
// string, suitable for diagnostic messages and markdown bullets.
func shortID(id ResourceIdentity) string {
	base := id.Kind
	if id.APIVersion != "" && !strings.ContainsRune(id.APIVersion, '/') {
		base = id.APIVersion + "/" + id.Kind
	} else if strings.Contains(id.APIVersion, "/") {
		// group/version; strip the version — (Group)/Kind is the useful shape.
		group := id.APIVersion
		if i := strings.Index(group, "/"); i >= 0 {
			group = group[:i]
		}
		base = group + "/" + id.Kind
	}
	out := base + " " + id.Name
	if id.Namespace != "" {
		out = base + " " + id.Namespace + "/" + id.Name
	}
	if id.AppName != "" {
		out += " (" + id.AppName + ")"
	}
	return out
}
