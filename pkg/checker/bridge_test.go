package checker

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestResolveOrMaterialiseKernel_Concurrent spawns 16 goroutines into a
// hermetic temp root. All must succeed and return the same dir.
func TestResolveOrMaterialiseKernel_Concurrent(t *testing.T) {
	root := t.TempDir()
	origRoot := kernelTempRoot
	kernelTempRoot = func() string { return root }
	t.Cleanup(func() { kernelTempRoot = origRoot })

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make([]string, goroutines)
	errs := make([]error, goroutines)
	start := make(chan struct{})

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			<-start
			dir, err := resolveOrMaterialiseKernel("")
			results[idx] = dir
			errs[idx] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	if t.Failed() {
		return
	}

	first := results[0]
	for i, got := range results {
		if got != first {
			t.Errorf("goroutine %d returned %q, expected %q", i, got, first)
		}
	}

	// Marker must exist after concurrent publish; the dir must contain check.shen.
	if _, err := os.Stat(filepath.Join(first, ".xpc-kernel-digest")); err != nil {
		t.Errorf("marker missing after concurrent publish: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first, "check.shen")); err != nil {
		t.Errorf("check.shen missing after concurrent publish: %v", err)
	}
}

// TestResolveOrMaterialiseKernel_LeftoverStaleDir simulates a destination
// directory left behind by a killed previous run: the dir exists and may
// even contain partial files, but no valid marker. The resolver must
// republish into it without erroring.
func TestResolveOrMaterialiseKernel_LeftoverStaleDir(t *testing.T) {
	root := t.TempDir()
	origRoot := kernelTempRoot
	kernelTempRoot = func() string { return root }
	t.Cleanup(func() { kernelTempRoot = origRoot })

	// Discover the digest the resolver will compute by running once cleanly,
	// then nuke the marker to simulate a partial/stale dir.
	dir, err := resolveOrMaterialiseKernel("")
	if err != nil {
		t.Fatalf("warm-up resolve failed: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, ".xpc-kernel-digest")); err != nil {
		t.Fatalf("remove marker to simulate stale state: %v", err)
	}
	// Replace one file with garbage to exercise overwrite-on-republish.
	if err := os.WriteFile(filepath.Join(dir, "check.shen"), []byte("garbage"), 0o600); err != nil {
		t.Fatalf("clobber check.shen: %v", err)
	}

	got, err := resolveOrMaterialiseKernel("")
	if err != nil {
		t.Fatalf("recovery resolve failed: %v", err)
	}
	if got != dir {
		t.Errorf("expected resolver to reuse %q, got %q", dir, got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".xpc-kernel-digest")); err != nil {
		t.Errorf("marker missing after recovery: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "check.shen"))
	if err != nil {
		t.Fatalf("read check.shen after recovery: %v", err)
	}
	if string(data) == "garbage" {
		t.Errorf("recovery did not overwrite stale check.shen")
	}
}
