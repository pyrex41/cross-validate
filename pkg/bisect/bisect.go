// Package bisect implements `xpc bisect` — given a rule code and two git refs,
// find the first commit on the path from <good> to <bad> where the rule's
// firing state changed (started firing, or stopped firing).
//
// The semantics are git-bisect-style: at <good> the rule has one state, at
// <bad> it has the opposite state, and we binary-search through the range to
// pinpoint the first commit that flipped it.
//
// Design choice: manual binary search rather than `git bisect run`. Reasons:
//  1. We need to carry per-step state (the cached good/bad diagnostic results,
//     the manifest path inside each worktree) that doesn't translate cleanly
//     into env vars passed to a sidecar runner.
//  2. Manual control lets us short-circuit clearly when the rule never fires
//     or never stops firing across the range — git bisect would just report
//     a single commit and the user would have to figure out the meaning.
//  3. Easier to unit-test — the rule detector is a function, so tests can
//     drive the full bisect loop without spawning xpc as a subprocess.
package bisect

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Direction describes how the rule's state changed across the bisect range.
type Direction string

const (
	// StartedFiring means the rule did NOT fire at <good> but DOES fire at <bad>.
	// The bisect identifies the first commit where it began firing.
	StartedFiring Direction = "started-firing"
	// StoppedFiring means the rule fired at <good> but does NOT fire at <bad>.
	// The bisect identifies the first commit where it stopped.
	StoppedFiring Direction = "stopped-firing"
)

// Result is the outcome of a bisect run.
type Result struct {
	// Commit is the SHA where the rule's state transitioned.
	Commit string
	// Subject is the first line of the commit message.
	Subject string
	// Direction is which way the state changed (started or stopped firing).
	Direction Direction
	// GoodSHA / BadSHA are the resolved boundary commits actually used.
	GoodSHA string
	BadSHA  string
	// Steps is how many commits the bisect actually checked (not counting
	// the boundary verifications). Useful for diagnostics.
	Steps int
}

// Options configures a bisect run.
type Options struct {
	// RuleCode is the rule whose firing state is being bisected (e.g., XPC002,
	// XPC.A.resource-field-valid). Must be non-empty and well-formed.
	RuleCode string
	// GoodRef is the known-good git ref (anything `git rev-parse` accepts).
	GoodRef string
	// BadRef is the known-bad git ref.
	BadRef string
	// RepoRoot is the working tree root used for git operations and as the
	// base for finding the manifest path. If empty, the current working
	// directory is used.
	RepoRoot string
	// ManifestPath is the path (relative to RepoRoot, or absolute) where
	// `xpc check` should run inside each materialized worktree. Empty means
	// the worktree root.
	ManifestPath string
	// CheckRule is the function that decides whether the rule fires at a
	// given materialized worktree path. Tests inject a stub; in production
	// this shells out to `xpc check --skip-render --format=json`.
	//
	// Returning an error aborts the bisect.
	CheckRule func(workdir string) (fires bool, err error)
	// GitBin overrides the git binary for tests. Defaults to "git".
	GitBin string
}

// ruleCodePattern matches valid rule codes.
//
// Two accepted shapes:
//   - Legacy:   "XPC" + digits (e.g., XPC002)
//   - Modern:   "XPC." + category-letter + "." + dotted-segments
//     (e.g., XPC.A.resource-field-valid, XPC.E.ssa-managementpolicies-strict)
//
// Anything else is a typo. We fail fast with a clear error rather than
// silently bisecting on a code that will never appear.
var ruleCodePattern = regexp.MustCompile(`^XPC(?:\d{3,}|\.[A-Z]\.[a-zA-Z0-9._-]+)$`)

// ValidateRuleCode returns nil iff code looks like a real xpc rule code.
// Exposed for the CLI to fail before doing any git work.
func ValidateRuleCode(code string) error {
	if code == "" {
		return errors.New("rule code is empty")
	}
	if !ruleCodePattern.MatchString(code) {
		return fmt.Errorf("malformed rule code %q (expected XPC<digits> or XPC.<letter>.<name>, e.g. XPC002 or XPC.A.resource-field-valid)", code)
	}
	return nil
}

// ErrRuleNeverChanged is returned when the rule has the same firing state at
// both <good> and <bad>. The CLI translates this into a friendlier message.
var ErrRuleNeverChanged = errors.New("rule firing state did not change between good and bad")

// ErrSameCommit is returned when --good and --bad resolve to the same SHA.
var ErrSameCommit = errors.New("good and bad refs resolve to the same commit")

// Run executes the bisect described by opts and returns the transition commit.
//
// Algorithm:
//  1. Resolve good/bad to SHAs; verify they differ.
//  2. Materialize worktrees for both boundaries; run CheckRule on each.
//     If both fire or both don't fire, return ErrRuleNeverChanged.
//  3. Get the linear history `good..bad` via `git rev-list --reverse`.
//  4. Binary-search the list. For each midpoint, materialize a worktree and
//     run CheckRule. The invariant is `commits[lo]` matches good's state and
//     `commits[hi]` matches bad's state; we find the first index whose state
//     equals bad's state.
//  5. Report the SHA + commit subject + direction.
//
// Worktrees are torn down with `git worktree remove --force` before Run
// returns, on every path including errors.
func Run(opts Options) (*Result, error) {
	if opts.CheckRule == nil {
		return nil, errors.New("bisect: CheckRule is required")
	}
	if err := ValidateRuleCode(opts.RuleCode); err != nil {
		return nil, err
	}
	if opts.GoodRef == "" {
		return nil, errors.New("bisect: GoodRef is required")
	}
	if opts.BadRef == "" {
		return nil, errors.New("bisect: BadRef is required")
	}
	gitBin := opts.GitBin
	if gitBin == "" {
		gitBin = "git"
	}
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("bisect: getwd: %w", err)
		}
		repoRoot = cwd
	}
	absRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("bisect: abs(%s): %w", repoRoot, err)
	}

	// 1. Resolve good and bad to SHAs.
	goodSHA, err := revParse(gitBin, absRepoRoot, opts.GoodRef)
	if err != nil {
		return nil, fmt.Errorf("bisect: resolve --good=%s: %w", opts.GoodRef, err)
	}
	badSHA, err := revParse(gitBin, absRepoRoot, opts.BadRef)
	if err != nil {
		return nil, fmt.Errorf("bisect: resolve --bad=%s: %w", opts.BadRef, err)
	}
	if goodSHA == badSHA {
		return nil, ErrSameCommit
	}

	// 2. Verify boundary states differ.
	goodFires, cleanup, err := checkAtCommit(gitBin, absRepoRoot, goodSHA, opts.ManifestPath, opts.CheckRule)
	if err != nil {
		return nil, fmt.Errorf("bisect: check good %s: %w", short(goodSHA), err)
	}
	cleanup()
	badFires, cleanup, err := checkAtCommit(gitBin, absRepoRoot, badSHA, opts.ManifestPath, opts.CheckRule)
	if err != nil {
		return nil, fmt.Errorf("bisect: check bad %s: %w", short(badSHA), err)
	}
	cleanup()
	if goodFires == badFires {
		return nil, ErrRuleNeverChanged
	}

	// 3. Get the linear history good..bad in chronological order.
	commits, err := revList(gitBin, absRepoRoot, goodSHA, badSHA)
	if err != nil {
		return nil, fmt.Errorf("bisect: rev-list: %w", err)
	}
	if len(commits) == 0 {
		// good..bad is empty, but their states differ. This means bad is an
		// ancestor of good (or unrelated). Either way, we can't bisect.
		return nil, fmt.Errorf("bisect: no commits in range %s..%s (is bad an ancestor of good?)", short(goodSHA), short(badSHA))
	}

	// 4. Binary search. We're looking for the smallest index i such that
	// commits[i] has bad's firing state. We maintain the invariant that
	// the commit immediately *before* commits[lo] matches good's state
	// (initially that's goodSHA itself), and commits[hi-1] is the last
	// candidate to inspect.
	lo, hi := 0, len(commits)
	steps := 0
	for lo < hi {
		mid := lo + (hi-lo)/2
		fires, cleanup, err := checkAtCommit(gitBin, absRepoRoot, commits[mid], opts.ManifestPath, opts.CheckRule)
		if err != nil {
			return nil, fmt.Errorf("bisect: check %s: %w", short(commits[mid]), err)
		}
		cleanup()
		steps++
		if fires == badFires {
			// commits[mid] has bad's state — transition is at or before mid.
			hi = mid
		} else {
			// commits[mid] has good's state — transition is after mid.
			lo = mid + 1
		}
	}

	if lo >= len(commits) {
		// Shouldn't happen because we verified the boundary states differ,
		// but defend against it anyway.
		return nil, errors.New("bisect: search did not converge (boundary state check disagrees with rev-list)")
	}

	transitionSHA := commits[lo]
	subject, err := commitSubject(gitBin, absRepoRoot, transitionSHA)
	if err != nil {
		return nil, fmt.Errorf("bisect: read subject for %s: %w", short(transitionSHA), err)
	}

	direction := StartedFiring
	if !badFires {
		direction = StoppedFiring
	}

	return &Result{
		Commit:    transitionSHA,
		Subject:   subject,
		Direction: direction,
		GoodSHA:   goodSHA,
		BadSHA:    badSHA,
		Steps:     steps,
	}, nil
}

// revParse resolves ref to a full SHA inside repoRoot.
func revParse(gitBin, repoRoot, ref string) (string, error) {
	cmd := exec.Command(gitBin, "-C", repoRoot, "rev-parse", "--verify", ref+"^{commit}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w\n%s", ref, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// revList returns the commits introduced by good..bad, in chronological
// (oldest-first) order. Empty if bad is an ancestor of good.
func revList(gitBin, repoRoot, goodSHA, badSHA string) ([]string, error) {
	cmd := exec.Command(gitBin, "-C", repoRoot, "rev-list", "--reverse", goodSHA+".."+badSHA)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git rev-list %s..%s: %w\n%s", short(goodSHA), short(badSHA), err, strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// commitSubject returns the first line of the commit message for sha.
func commitSubject(gitBin, repoRoot, sha string) (string, error) {
	cmd := exec.Command(gitBin, "-C", repoRoot, "log", "-1", "--format=%s", sha)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git log %s: %w\n%s", short(sha), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// checkAtCommit materializes a temporary worktree at sha, runs CheckRule
// against the manifest path inside it, and returns the result + a cleanup
// closure. The closure is always non-nil; callers MUST invoke it (typically
// immediately, since we don't need the worktree after the check).
func checkAtCommit(gitBin, repoRoot, sha, manifestPath string, check func(string) (bool, error)) (bool, func(), error) {
	wtDir, cleanup, err := addWorktree(gitBin, repoRoot, sha)
	if err != nil {
		return false, func() {}, err
	}

	// Resolve the manifest path inside the worktree. If absolute, take the
	// relative-to-repo-root portion; if relative, treat as repo-relative.
	checkDir := wtDir
	if manifestPath != "" {
		rel := manifestPath
		if filepath.IsAbs(rel) {
			r, err := filepath.Rel(repoRoot, rel)
			if err != nil {
				cleanup()
				return false, func() {}, fmt.Errorf("manifest path %s not under repo root %s: %w", rel, repoRoot, err)
			}
			rel = r
		}
		checkDir = filepath.Join(wtDir, rel)
	}

	fires, err := check(checkDir)
	if err != nil {
		cleanup()
		return false, func() {}, err
	}
	return fires, cleanup, nil
}

// addWorktree creates a detached worktree at sha under TempDir. The cleanup
// closure removes it. Returns the worktree directory.
func addWorktree(gitBin, repoRoot, sha string) (string, func(), error) {
	wtDir, err := os.MkdirTemp("", "xpc-bisect-"+short(sha)+"-")
	if err != nil {
		return "", nil, fmt.Errorf("mkdir worktree: %w", err)
	}
	// `git worktree add` requires the target NOT to exist.
	if err := os.RemoveAll(wtDir); err != nil {
		return "", nil, fmt.Errorf("clear worktree dir: %w", err)
	}
	cmd := exec.Command(gitBin, "-C", repoRoot, "worktree", "add", "--detach", "--quiet", wtDir, sha)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("git worktree add %s %s: %w\n%s", wtDir, short(sha), err, strings.TrimSpace(string(out)))
	}
	cleanup := func() {
		_ = exec.Command(gitBin, "-C", repoRoot, "worktree", "remove", "--force", wtDir).Run()
		_ = os.RemoveAll(wtDir)
	}
	return wtDir, cleanup, nil
}

// short returns the 12-character prefix of sha for human-readable messages.
func short(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}
