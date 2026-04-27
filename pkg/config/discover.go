package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalFileName is the only filename Discover looks for. The design
// reserves `.xpc/config.yaml` as a fallback location once we have multiple
// config files to colocate; until then `xpc.yaml` at the repo root is the
// single supported location.
const CanonicalFileName = "xpc.yaml"

// executablePath is overridable in tests; defaults to os.Executable. Mirrors
// the same hook used by pkg/checker for kernel discovery.
var executablePath = os.Executable

// Discover finds an xpc.yaml on disk. Resolution order:
//
//  1. Walk upward from the supplied start directory looking for an xpc.yaml
//     at every ancestor.
//  2. If not found and an xpc binary location can be resolved, repeat the
//     walk from filepath.Dir(exe). This is the "binary lives next to its
//     kernel" fallback that mirrors pkg/checker/bridge.go's resolveKernelPath
//     — same shape so `xpc plan` running in a temp worktree behaves
//     consistently for both kernel and config.
//  3. Return ("", false, nil) when neither walk finds a file. Callers
//     interpret that as "use Default()".
//
// `start` is typically the current working directory; tests may pass an
// explicit fixture root.
func Discover(start string) (path string, found bool, viaExe bool, err error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", false, false, fmt.Errorf("abs(%s): %w", start, err)
	}

	if p, ok := searchUpward(abs); ok {
		return p, true, false, nil
	}

	if exe, exeErr := executablePath(); exeErr == nil {
		if resolved, rlErr := filepath.EvalSymlinks(exe); rlErr == nil {
			exe = resolved
		}
		if p, ok := searchUpward(filepath.Dir(exe)); ok {
			return p, true, true, nil
		}
	}

	return "", false, false, nil
}

// searchUpward walks from start to filesystem root looking for the canonical
// xpc.yaml. Returns the first hit. Symlinks on the path are honored as
// stat() naturally follows them.
func searchUpward(start string) (string, bool) {
	dir := start
	for {
		candidate := filepath.Join(dir, CanonicalFileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// LoadIfPresent loads xpc.yaml from dir if (and only if) the file exists at
// exactly dir/xpc.yaml — no upward walk. Returns Default() when absent.
// Useful for hermetic fixture loaders that want one fixture's xpc.yaml to
// apply to that fixture without leaking from parent directories.
func LoadIfPresent(dir string) (*Config, error) {
	candidate := filepath.Join(dir, CanonicalFileName)
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return Default(), nil
	}
	return Load(candidate)
}

// Resolve combines flag-precedence + env-precedence + discovery into a single
// helper. Order of precedence (per design §3.a):
//
//	--config=<path>            (explicit, fatal-if-missing)
//	XPC_CONFIG_PATH env var    (explicit, fatal-if-missing)
//	discovery from `start`     (silent, falls through to Default if absent)
//	Default()                  (no file present anywhere)
//
// On success the returned (*Config, sourcePath) describes what the loader
// settled on; sourcePath is "" when the result is Default(). The viaExe
// flag is true when discovery had to fall back to the binary location —
// callers may surface that on stderr to mirror the kernel-fallback diag.
func Resolve(flagPath, envPath, start string) (cfg *Config, sourcePath string, viaExe bool, err error) {
	if flagPath != "" {
		c, lerr := Load(flagPath)
		return c, flagPath, false, lerr
	}
	if envPath != "" {
		c, lerr := Load(envPath)
		return c, envPath, false, lerr
	}
	p, ok, viaExe, derr := Discover(start)
	if derr != nil {
		return nil, "", false, derr
	}
	if !ok {
		return Default(), "", false, nil
	}
	c, lerr := Load(p)
	return c, p, viaExe, lerr
}
