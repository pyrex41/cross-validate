package ir

import (
	"path/filepath"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// valueRefOutcome captures the result of resolving a single `$<ref>/...`
// prefixed Helm valueFile against an Application's sibling sources. The
// builder uses this to decide whether to rewrite the valueFile in place,
// emit a warning, or leave it alone for the renderer to fail noisily.
type valueRefOutcome int

const (
	valueRefNoChange   valueRefOutcome = iota // file had no $<ref>/ prefix
	valueRefResolved                          // rewrote to absolute path
	valueRefUnknownRef                        // $<ref>/ prefix with no matching sibling
	valueRefRemote                            // matching sibling, but its repo isn't locally available
)

// resolveValueFilePrefix resolves a single `$<ref>/<rest>` Helm valueFile
// against the Application's sibling sources. Returns the rewritten path
// (same as input for a non-ref file) and the outcome so callers can decide
// whether to record an info-level diagnostic.
//
// Local resolution rules:
//   - `$<ref>/rest` with a sibling source whose `Ref == <ref>`:
//   - if the sibling has an explicit `Path`, the resolved root is
//     `filepath.Join(cwd, sibling.Path)`.
//   - else, the resolved root is the repo root discovered by walking up
//     from `cwd` looking for a `.git` directory or file (Argo's
//     `ref: <name>` on a co-located source means "use that source's repo
//     checkout"; for fg-manifold's same-repo case, that's the appfile's
//     repo).
//   - if no repo root can be located, the outcome is `valueRefRemote`
//     (truly-remote repos would need a clone; xpc's first cut only handles
//     local co-located cases).
func resolveValueFilePrefix(valueFile string, siblings []types.ArgoSource, cwd string) (string, valueRefOutcome) {
	if !strings.HasPrefix(valueFile, "$") {
		return valueFile, valueRefNoChange
	}
	slash := strings.Index(valueFile, "/")
	if slash < 2 {
		return valueFile, valueRefNoChange
	}
	refName := valueFile[1:slash]
	rest := valueFile[slash+1:]

	var sibling *types.ArgoSource
	for i := range siblings {
		if siblings[i].Ref == refName {
			sibling = &siblings[i]
			break
		}
	}
	if sibling == nil {
		return valueFile, valueRefUnknownRef
	}

	var root string
	if sibling.Path != "" {
		if filepath.IsAbs(sibling.Path) {
			root = filepath.Clean(sibling.Path)
		} else {
			root = filepath.Clean(filepath.Join(cwd, sibling.Path))
		}
	} else {
		r := findRepoRoot(cwd)
		if r == "" {
			return valueFile, valueRefRemote
		}
		root = r
	}
	resolved := filepath.Clean(filepath.Join(root, rest))
	if abs, err := filepath.Abs(resolved); err == nil {
		resolved = abs
	}
	return resolved, valueRefResolved
}

// rewriteHelmValueFiles walks the ValueFiles slice and resolves any
// `$<ref>/...` prefixes against `siblings` anchored at `cwd`. Returns a
// shallow-cloned *ArgoHelmSource so the caller never mutates the input
// (which may be shared across Applications produced by AppSet expansion).
// Also returns per-valueFile outcomes (same length as input slice) so the
// caller can emit info diagnostics for unresolved or remote refs.
func rewriteHelmValueFiles(h *types.ArgoHelmSource, siblings []types.ArgoSource, cwd string) (*types.ArgoHelmSource, []valueRefOutcome) {
	if h == nil || len(h.ValueFiles) == 0 {
		return h, nil
	}
	outcomes := make([]valueRefOutcome, len(h.ValueFiles))
	rewrote := false
	files := make([]string, len(h.ValueFiles))
	for i, vf := range h.ValueFiles {
		resolved, outcome := resolveValueFilePrefix(vf, siblings, cwd)
		outcomes[i] = outcome
		files[i] = resolved
		if outcome == valueRefResolved {
			rewrote = true
		}
	}
	if !rewrote {
		return h, outcomes
	}
	clone := *h
	clone.ValueFiles = files
	return &clone, outcomes
}
