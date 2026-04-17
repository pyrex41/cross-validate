package audit

import (
	"path/filepath"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestGenerate(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC002",
			Severity: types.SeverityError,
			Message:  "webhook conversion not acknowledged",
			Source:   types.SourceLocation{File: "bucket.yaml", Line: 1},
		},
		{
			Code:     "XPC007",
			Severity: types.SeverityWarning,
			Message:  "label tracking conflict",
			Source:   types.SourceLocation{File: "app.yaml", Line: 5},
		},
	}

	p := Generate(diags, nil, "sha256:abc123", "sha256:def456")

	if p.RootDigest == "" {
		t.Error("expected non-empty root digest")
	}
	if p.Version != 2 {
		t.Errorf("expected version 2, got %d", p.Version)
	}
	if p.Metadata.IRDigest != "sha256:abc123" {
		t.Errorf("expected IR digest sha256:abc123, got %s", p.Metadata.IRDigest)
	}
	if p.Metadata.SnapshotDigest != "sha256:def456" {
		t.Errorf("expected snapshot digest sha256:def456, got %s", p.Metadata.SnapshotDigest)
	}

	// Check rule subtrees
	xpc002, ok := p.RuleSubtrees["XPC002"]
	if !ok {
		t.Fatal("expected XPC002 rule subtree")
	}
	if len(xpc002.Judgments) != 1 {
		t.Errorf("expected 1 judgment for XPC002, got %d", len(xpc002.Judgments))
	}
	if xpc002.Judgments[0].Status != "error" {
		t.Errorf("expected error status, got %s", xpc002.Judgments[0].Status)
	}
}

func TestVerify(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC001",
			Severity: types.SeverityError,
			Message:  "test error",
			Source:   types.SourceLocation{File: "test.yaml", Line: 1},
		},
	}

	p := Generate(diags, nil, "sha256:ir1", "sha256:snap1")

	if !p.Verify() {
		t.Error("expected verification to pass")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.xpcproof")

	diags := []types.Diagnostic{
		{
			Code:     "XPC003",
			Severity: types.SeverityError,
			Message:  "composition references unknown XRD",
			Source:   types.SourceLocation{File: "comp.yaml", Line: 10},
		},
	}

	p := Generate(diags, nil, "sha256:ir2", "sha256:snap2")
	if err := p.Save(path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadProof(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.RootDigest != p.RootDigest {
		t.Errorf("root digest mismatch: %s vs %s", loaded.RootDigest, p.RootDigest)
	}
	if loaded.Metadata.IRDigest != p.Metadata.IRDigest {
		t.Errorf("IR digest mismatch")
	}
}

func TestVerifyInclusion(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC002",
			Severity: types.SeverityError,
			Message:  "webhook conversion",
			Source:   types.SourceLocation{File: "bucket.yaml", Line: 1},
		},
	}

	p := Generate(diags, nil, "sha256:ir3", "sha256:snap3")

	if !p.VerifyInclusion("XPC002", "bucket.yaml:1") {
		t.Error("expected inclusion of XPC002 for bucket.yaml:1")
	}
	if p.VerifyInclusion("XPC002", "nonexistent.yaml:1") {
		t.Error("expected non-inclusion for nonexistent resource")
	}
	if p.VerifyInclusion("XPC999", "bucket.yaml:1") {
		t.Error("expected non-inclusion for unknown rule")
	}
}

func TestSummary(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC002",
			Severity: types.SeverityError,
			Message:  "webhook conversion",
			Source:   types.SourceLocation{File: "a.yaml", Line: 1},
		},
		{
			Code:     "XPC007",
			Severity: types.SeverityWarning,
			Message:  "label tracking",
			Source:   types.SourceLocation{File: "b.yaml", Line: 2},
		},
	}

	p := Generate(diags, nil, "sha256:ir4", "sha256:snap4")
	summary := p.Summary()

	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestDiffProofs(t *testing.T) {
	diagsA := []types.Diagnostic{
		{
			Code:     "XPC002",
			Severity: types.SeverityError,
			Message:  "webhook conversion",
			Source:   types.SourceLocation{File: "a.yaml", Line: 1},
		},
	}
	diagsB := []types.Diagnostic{} // no errors

	a := Generate(diagsA, nil, "sha256:ir5", "sha256:snap5")
	b := Generate(diagsB, nil, "sha256:ir6", "sha256:snap6")

	diff := DiffProofs(a, b)
	if diff == "" {
		t.Error("expected non-empty diff")
	}
}

func TestEmptyProof(t *testing.T) {
	p := Generate(nil, nil, "sha256:ir7", "sha256:snap7")

	if p.RootDigest == "" {
		t.Error("expected non-empty root digest for empty proof")
	}
	if !p.Verify() {
		t.Error("expected empty proof to verify")
	}

	summary := p.Summary()
	if summary == "" {
		t.Error("expected non-empty summary for empty proof")
	}
}

func TestRunSummaryInRoot(t *testing.T) {
	// A proof with a RunSummary and one without must have different roots even
	// when the diagnostics are identical — the summary is committed.
	diags := []types.Diagnostic{
		{
			Code:     "XPC002",
			Severity: types.SeverityError,
			Message:  "webhook conversion not acknowledged",
			Source:   types.SourceLocation{File: "bucket.yaml", Line: 1},
		},
	}

	summary := &RunSummary{
		TotalObligations: 3,
		Satisfied:        2,
		Violated:         1,
		ObligationIDs:    []string{"XPC.J.x.a", "XPC.B.y.b", "XPC.C.z.c"},
	}

	withSummary := Generate(diags, summary, "sha256:ir", "sha256:snap")
	withoutSummary := Generate(diags, nil, "sha256:ir", "sha256:snap")

	if withSummary.RootDigest == withoutSummary.RootDigest {
		t.Error("expected different root digests when summary is committed vs absent")
	}
	if withSummary.Run == nil || withSummary.Run.TotalObligations != 3 {
		t.Errorf("expected run summary in proof, got %+v", withSummary.Run)
	}
	if !withSummary.Verify() {
		t.Error("expected proof with run summary to verify")
	}

	// Different obligation IDs must produce a different root even with identical diagnostics.
	altSummary := &RunSummary{
		TotalObligations: 3,
		Satisfied:        2,
		Violated:         1,
		ObligationIDs:    []string{"XPC.J.x.a", "XPC.B.y.different", "XPC.C.z.c"},
	}
	alt := Generate(diags, altSummary, "sha256:ir", "sha256:snap")
	if alt.RootDigest == withSummary.RootDigest {
		t.Error("expected different root for different obligation IDs")
	}
}

func TestJudgmentProvenance(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC003",
			Severity: types.SeverityError,
			Message:  "missing XRD",
			Source:   types.SourceLocation{File: "comp.yaml", Line: 1},
			Obligation: &types.ObligationRef{
				ID:        "XPC.B.comp-xrd-ref.xbucket-default",
				Category:  "B",
				Generator: "comp-xrd-ref",
			},
		},
	}

	p := Generate(diags, nil, "sha256:ir", "sha256:snap")
	st := p.RuleSubtrees["XPC003"]
	if st == nil || len(st.Judgments) != 1 {
		t.Fatalf("expected 1 XPC003 judgment, got subtree %+v", st)
	}
	j := st.Judgments[0]
	if j.ObligationID != "XPC.B.comp-xrd-ref.xbucket-default" || j.Category != "B" || j.Generator != "comp-xrd-ref" {
		t.Errorf("expected provenance fields populated, got %+v", j)
	}
}

func TestLoadNonexistent(t *testing.T) {
	_, err := LoadProof("/nonexistent/path")
	if err == nil {
		t.Error("expected error loading nonexistent proof")
	}
}

func TestProofDeterminism(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC001",
			Severity: types.SeverityError,
			Message:  "version error",
			Source:   types.SourceLocation{File: "crd.yaml", Line: 1},
		},
	}

	p1 := Generate(diags, nil, "sha256:same", "sha256:same")
	p2 := Generate(diags, nil, "sha256:same", "sha256:same")

	// Root digests may differ due to timestamp, but rule subtree digests should match
	if p1.RuleSubtrees["XPC001"].Digest != p2.RuleSubtrees["XPC001"].Digest {
		t.Error("expected same rule subtree digest for same input")
	}
}
