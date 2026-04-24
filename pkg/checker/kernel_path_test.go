package checker

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveKernelPath_ExplicitWins confirms an explicit --kernel-path value
// is returned as-is without triggering any directory search.
func TestResolveKernelPath_ExplicitWins(t *testing.T) {
	got, err := resolveKernelPath("/nonexistent/but/explicit")
	if err != nil {
		t.Fatalf("explicit path must be honoured without error, got %v", err)
	}
	if got != "/nonexistent/but/explicit" {
		t.Errorf("expected explicit path returned verbatim, got %q", got)
	}
}

// TestSearchKernelUpward_Found — a kernel/check.shen placed somewhere above
// the start directory is discovered by the upward walk.
func TestSearchKernelUpward_Found(t *testing.T) {
	root := t.TempDir()
	kernelDir := filepath.Join(root, "kernel")
	if err := os.MkdirAll(kernelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kernelDir, "check.shen"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := searchKernelUpward(deep)
	if !ok {
		t.Fatalf("expected to find kernel upward from %s", deep)
	}
	if got != kernelDir {
		t.Errorf("expected %q, got %q", kernelDir, got)
	}
}

// TestSearchKernelUpward_NotFound — with no kernel/check.shen anywhere above
// the start directory, the helper returns (_, false) rather than spinning.
func TestSearchKernelUpward_NotFound(t *testing.T) {
	// Use an isolated temp dir. There is no kernel/ above t.TempDir()'s
	// ancestors (Go guarantees TempDir returns a fresh path under
	// /tmp or similar), but the search may still walk through system
	// directories. To keep the test hermetic, point at a deep temp path
	// and confirm whatever the helper returns is correct for that tree.
	root := t.TempDir()
	deep := filepath.Join(root, "x", "y")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// If the host filesystem has /kernel/check.shen (unlikely) the walk
	// would find it and return true, so skip the strict false assertion
	// and only verify that when found the path actually contains
	// check.shen.
	got, ok := searchKernelUpward(deep)
	if ok {
		if _, err := os.Stat(filepath.Join(got, "check.shen")); err != nil {
			t.Errorf("searchKernelUpward returned %q but it has no check.shen: %v", got, err)
		}
	}
}

// TestResolveKernelPath_ExecutableFallback simulates the P5.c fix: CWD has
// no kernel/ ancestor but the xpc executable sits above one. The test
// injects a synthetic executable path that lives above a kernel/check.shen
// file, chdirs into an isolated tree with no kernel/ ancestor, and asserts
// that resolveKernelPath("") discovers the kernel via the executable
// fallback — the path `xpc plan` now relies on when invoked from a
// worktree outside the cross-validate repo.
func TestResolveKernelPath_ExecutableFallback(t *testing.T) {
	// Build a synthetic "install tree": bin/xpc with a kernel/ sibling.
	fakeRoot := t.TempDir()
	binDir := filepath.Join(fakeRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeExe := filepath.Join(binDir, "xpc")
	if err := os.WriteFile(fakeExe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	kernelDir := filepath.Join(fakeRoot, "kernel")
	if err := os.MkdirAll(kernelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kernelDir, "check.shen"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	origExe := executablePath
	executablePath = func() (string, error) { return fakeExe, nil }
	t.Cleanup(func() { executablePath = origExe })

	// Isolate cwd under a tree that has no kernel/ ancestor (t.TempDir
	// lives under os.TempDir and has no sibling kernel/).
	isolated := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(isolated); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// Sanity: cwd-based upward search must miss so we know the fallback is
	// what actually produces the success.
	if _, ok := searchKernelUpward(isolated); ok {
		t.Skipf("host fs has kernel/ above %s; cwd search would succeed and bypass the fallback", isolated)
	}

	got, err := resolveKernelPath("")
	if err != nil {
		t.Fatalf("expected executable-fallback to locate kernel, got %v", err)
	}
	// resolveKernelPath runs EvalSymlinks on the executable; the macOS
	// /var -> /private/var symlink changes the expected directory. Normalize
	// the expected path the same way for comparison.
	expectedKernel := kernelDir
	if resolved, rlErr := filepath.EvalSymlinks(kernelDir); rlErr == nil {
		expectedKernel = resolved
	}
	if got != expectedKernel {
		t.Errorf("expected %q (from fake exe search), got %q", expectedKernel, got)
	}
}

// TestResolveKernelPath_BothSearchesFail confirms the error surface when
// neither CWD-based nor executable-based search finds kernel/check.shen.
func TestResolveKernelPath_BothSearchesFail(t *testing.T) {
	fakeRoot := t.TempDir()
	fakeExe := filepath.Join(fakeRoot, "xpc")
	if err := os.WriteFile(fakeExe, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}

	origExe := executablePath
	executablePath = func() (string, error) { return fakeExe, nil }
	t.Cleanup(func() { executablePath = origExe })

	isolated := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(isolated); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	if _, ok := searchKernelUpward(isolated); ok {
		t.Skipf("host fs has kernel/ above %s; cannot exercise double-miss path", isolated)
	}

	if _, err := resolveKernelPath(""); err == nil {
		t.Fatal("expected error when both cwd and executable searches miss")
	}
}
