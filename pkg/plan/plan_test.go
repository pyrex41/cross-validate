package plan_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/plan"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// runPlan exercises the directory-variant path (no git worktree) so tests
// stay hermetic. Base and Head are loaded from sibling fixture subdirs.
func runHermeticPlan(t *testing.T, baseDir, headDir string) *plan.Plan {
	t.Helper()
	p, cleanup, err := plan.Run(plan.Config{
		BaseRef:    baseDir,
		HeadRef:    headDir,
		Path:       ".", // unused when BaseRef/HeadRef are absolute dirs
		SkipRender: true,
	})
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("plan.Run(%s → %s): %v", baseDir, headDir, err)
	}
	return p
}

func TestR26_DestructiveDelete(t *testing.T) {
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-destructive/base",
		"../../testdata/fixtures/plan-destructive/head",
	)

	// Base declares one state-bearing Cluster; head is empty → 1 removed.
	if got := len(p.Delta.Removed); got != 1 {
		t.Fatalf("expected 1 removed resource, got %d: %+v", got, p.Delta.Removed)
	}
	if got := len(p.Delta.Added); got != 0 {
		t.Errorf("expected 0 added, got %d: %+v", got, p.Delta.Added)
	}

	var destructive []types.Diagnostic
	for _, d := range p.Diagnostics {
		if d.Code == "XPC.P.destructive-delete" {
			destructive = append(destructive, d)
		}
	}
	if len(destructive) != 1 {
		t.Fatalf("expected 1 XPC.P.destructive-delete, got %d: %+v", len(destructive), p.Diagnostics)
	}
	if destructive[0].Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", destructive[0].Severity)
	}
	if !strings.Contains(destructive[0].Message, "aurora-prod-cluster") {
		t.Errorf("expected resource name in message, got %q", destructive[0].Message)
	}
}

func TestR26_DestructiveDelete_OrphanSilent(t *testing.T) {
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-destructive-orphan/base",
		"../../testdata/fixtures/plan-destructive-orphan/head",
	)
	if got := len(p.Delta.Removed); got != 1 {
		t.Fatalf("expected 1 removed resource, got %d", got)
	}
	for _, d := range p.Diagnostics {
		if strings.HasPrefix(d.Code, "XPC.P.") {
			t.Errorf("expected no XPC.P.* diagnostics on Orphan base, got %+v", d)
		}
	}
}

func TestR26_CascadeRisk(t *testing.T) {
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-cascade-risk/base",
		"../../testdata/fixtures/plan-cascade-risk/head",
	)

	var cascade []types.Diagnostic
	for _, d := range p.Diagnostics {
		if d.Code == "XPC.P.cascade-risk" {
			cascade = append(cascade, d)
		}
	}
	if len(cascade) != 1 {
		t.Fatalf("expected 1 XPC.P.cascade-risk, got %d: %+v", len(cascade), p.Diagnostics)
	}
	if !strings.Contains(cascade[0].Message, "preview-alb") {
		t.Errorf("expected app name in message, got %q", cascade[0].Message)
	}
}

func TestPlan_NoDelta(t *testing.T) {
	// Same fixture on both sides → no delta, no diagnostics.
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-destructive/base",
		"../../testdata/fixtures/plan-destructive/base",
	)
	if got := len(p.Delta.Added) + len(p.Delta.Removed) + len(p.Delta.Modified); got != 0 {
		t.Errorf("expected empty delta, got %+v", p.Delta)
	}
	for _, d := range p.Diagnostics {
		if strings.HasPrefix(d.Code, "XPC.P.") {
			t.Errorf("expected no XPC.P.* diagnostics, got %+v", d)
		}
	}
}

func TestPlan_WriteMarkdown(t *testing.T) {
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-destructive/base",
		"../../testdata/fixtures/plan-destructive/head",
	)
	var buf bytes.Buffer
	if err := plan.WriteMarkdown(&buf, p); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"## xpc plan:",
		"Destructive changes",
		"aurora-prod-cluster",
		"Resource changes",
		"Removed: 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected markdown to contain %q; got:\n%s", want, out)
		}
	}
}

func TestPlan_WriteJSON(t *testing.T) {
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-destructive/base",
		"../../testdata/fixtures/plan-destructive/head",
	)
	var buf bytes.Buffer
	if err := plan.WriteJSON(&buf, p); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("produced invalid JSON: %v\n%s", err, buf.String())
	}
	delta, ok := parsed["delta"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected top-level delta object: %v", parsed)
	}
	if delta["removed"].(float64) != 1 {
		t.Errorf("expected delta.removed == 1, got %v", delta["removed"])
	}
	if _, ok := parsed["destructive"]; !ok {
		t.Errorf("expected destructive key present when diagnostics exist")
	}
}
