package plan_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/plan"
	"github.com/pyrex41/cross-validate-/pkg/snapshot"
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
	destructive := parsed["destructive"].([]interface{})
	if len(destructive) != 1 {
		t.Fatalf("expected one destructive row, got %v", destructive)
	}
	row := destructive[0].(map[string]interface{})
	if row["apiVersion"] != "rds.aws.upbound.io/v1beta1" {
		t.Errorf("expected real apiVersion, got %v", row["apiVersion"])
	}
	if row["kind"] != "Cluster" {
		t.Errorf("expected real kind, got %v", row["kind"])
	}
	if row["name"] != "aurora-prod-cluster" {
		t.Errorf("expected real resource name, got %v", row["name"])
	}
	if row["message"] == "" || row["code"] != "XPC.P.destructive-delete" {
		t.Errorf("expected code/message metadata, got %v", row)
	}
}

func TestPlan_WriteJSON_DoesNotJoinZeroSource(t *testing.T) {
	p := &plan.Plan{
		Base: plan.VariantResult{Ref: "base"},
		Head: plan.VariantResult{Ref: "head"},
		Delta: plan.ResourceDelta{
			Removed: []plan.ResourceChange{{
				Identity: plan.ResourceIdentity{
					APIVersion: "example.com/v1",
					Kind:       "Widget",
					Name:       "removed",
				},
				BaseSource: types.SourceLocation{File: "base.yaml", Line: 1, Column: 1},
			}},
		},
		Diagnostics: []types.Diagnostic{{
			Code:     "XPC.P.future-rule",
			Severity: types.SeverityError,
			Message:  "future rule forgot to set source",
		}},
	}

	var buf bytes.Buffer
	if err := plan.WriteJSON(&buf, p); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("produced invalid JSON: %v\n%s", err, buf.String())
	}
	destructive := parsed["destructive"].([]interface{})
	row := destructive[0].(map[string]interface{})
	if row["apiVersion"] != "" || row["kind"] != "" || row["name"] != "" {
		t.Fatalf("zero source should not join to removed resource with empty HeadSource: %v", row)
	}
}

// buildSnapshotForTest captures the base fixture as an .xpcsnap file in
// t.TempDir() and returns its absolute path. The includeResources flag
// mirrors the on-disk shape produced by `xpc snapshot --include-resources`
// versus the legacy 2-arg shim.
func buildSnapshotForTest(t *testing.T, fixtureDir string, includeResources bool) string {
	t.Helper()
	docs, err := loader.LoadDirectory(fixtureDir)
	if err != nil {
		t.Fatalf("loader.LoadDirectory(%s): %v", fixtureDir, err)
	}
	world, err := ir.NewBuilder().Build(docs)
	if err != nil {
		t.Fatalf("ir.Build: %v", err)
	}
	var snap *snapshot.Snapshot
	if includeResources {
		snap = snapshot.FromWorldWithOptions(world, "test",
			snapshot.FromWorldOptions{IncludeResources: true})
	} else {
		snap = snapshot.FromWorld(world, "test")
	}
	snapPath := filepath.Join(t.TempDir(), "base.xpcsnap")
	if err := snap.Save(snapPath); err != nil {
		t.Fatalf("snap.Save(%s): %v", snapPath, err)
	}
	return snapPath
}

func TestPlan_FromSnapshot(t *testing.T) {
	snapPath := buildSnapshotForTest(t,
		"../../testdata/fixtures/plan-destructive/base", true)

	p, cleanup, err := plan.Run(plan.Config{
		BaseRef:    snapPath,
		HeadRef:    "../../testdata/fixtures/plan-destructive/head",
		Path:       ".",
		SkipRender: true,
	})
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("plan.Run with .xpcsnap base: %v", err)
	}

	if got := len(p.Delta.Removed); got != 1 {
		t.Fatalf("expected 1 removed resource, got %d: %+v", got, p.Delta.Removed)
	}

	for _, d := range p.Base.Diagnostics {
		if d.Code == "XPC.P.snapshot-incomplete" {
			t.Errorf("did not expect XPC.P.snapshot-incomplete on with-resources snapshot; got %+v", d)
		}
	}
}

func TestPlan_FromSnapshot_Incomplete(t *testing.T) {
	snapPath := buildSnapshotForTest(t,
		"../../testdata/fixtures/plan-destructive/base", false)

	p, cleanup, err := plan.Run(plan.Config{
		BaseRef:    snapPath,
		HeadRef:    "../../testdata/fixtures/plan-destructive/head",
		Path:       ".",
		SkipRender: true,
	})
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("plan.Run with legacy .xpcsnap base: %v", err)
	}

	var incomplete []types.Diagnostic
	for _, d := range p.Base.Diagnostics {
		if d.Code == "XPC.P.snapshot-incomplete" {
			incomplete = append(incomplete, d)
		}
	}
	if len(incomplete) != 1 {
		t.Fatalf("expected exactly 1 XPC.P.snapshot-incomplete on Base, got %d: %+v",
			len(incomplete), p.Base.Diagnostics)
	}
	if incomplete[0].Severity != types.SeverityInfo {
		t.Errorf("expected Info severity, got %s", incomplete[0].Severity)
	}
}
