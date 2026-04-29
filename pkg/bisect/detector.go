package bisect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isBenignNoManifests recognizes stderr messages from `xpc check` that mean
// "this commit had no YAML to inspect" — which we treat as the rule not
// firing rather than a fatal error, so bisect can cross history boundaries
// where directories were renamed or didn't exist yet.
func isBenignNoManifests(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "no yaml documents found") ||
		strings.Contains(s, "no such file or directory")
}

// XPCCheckDetector returns a CheckRule function that shells out to the given
// xpcBin (`xpc check --skip-render --format=json`) inside a worktree and
// scans the JSON output for the configured rule code.
//
// We use --skip-render because:
//  1. Helm/Kustomize binaries may not be available across every commit in
//     the bisect range (or may render differently per commit in ways
//     unrelated to the rule under test).
//  2. The presence/absence of the rule code is what we care about — full
//     rendering is orthogonal.
//
// Note that --skip-render emits its own info diagnostics (XPC.H.helm-renders,
// XPC.H.kustomize-renders); we filter only on the user-supplied ruleCode, so
// that's harmless unless the user is bisecting one of those codes (in which
// case --skip-render isn't the right tool — but we document this in CLI help).
//
// extraArgs lets the caller pass through additional flags (e.g.,
// --kernel-path, --config) that affect rule evaluation but aren't part of
// the bisect itself.
func XPCCheckDetector(xpcBin, ruleCode string, extraArgs []string) func(string) (bool, error) {
	return func(workdir string) (bool, error) {
		args := []string{"check", "--skip-render", "--format=json"}
		args = append(args, extraArgs...)
		args = append(args, workdir)
		cmd := exec.Command(xpcBin, args...)
		// Inherit env so XPC_KERNEL_PATH etc. propagate.
		cmd.Env = os.Environ()
		out, err := cmd.Output()
		// `xpc check` exits 1 when there are errors, but still emits valid
		// JSON to stdout. Only treat exec failures (binary missing, kernel
		// load panic) as fatal — exit-1 with JSON body is a normal "found
		// errors" response.
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				return false, fmt.Errorf("invoke %s: %w", xpcBin, err)
			}
			if len(out) == 0 {
				// Stdout empty + non-zero exit = the binary failed before
				// reaching JSON output. Some of those failures are benign
				// for bisect ("no YAML found" at a commit where the path
				// doesn't have manifests yet) — treat as "rule not firing"
				// so the bisect can proceed across history reorganizations.
				stderr := string(exitErr.Stderr)
				if isBenignNoManifests(stderr) {
					return false, nil
				}
				return false, fmt.Errorf("xpc check failed at %s: %s\n%s", workdir, exitErr, stderr)
			}
		}

		return ScanDiagnostics(out, ruleCode)
	}
}

// ScanDiagnostics parses the JSON array emitted by `xpc check --format=json`
// and returns true iff any diagnostic carries the given rule code.
//
// Exposed for unit tests of the detection logic (independent of running
// xpc as a subprocess).
func ScanDiagnostics(jsonOut []byte, ruleCode string) (bool, error) {
	if len(jsonOut) == 0 {
		// `xpc check` always emits a JSON array (possibly empty) on stdout.
		// Empty bytes means we never reached JSON output — caller handles.
		return false, nil
	}
	var diags []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(jsonOut, &diags); err != nil {
		return false, fmt.Errorf("parse xpc check JSON: %w", err)
	}
	for _, d := range diags {
		if d.Code == ruleCode {
			return true, nil
		}
	}
	return false, nil
}
