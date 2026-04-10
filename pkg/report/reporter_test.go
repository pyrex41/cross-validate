package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestAgentFormat_NoIssues(t *testing.T) {
	var buf bytes.Buffer
	err := reportAgent(&buf, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "ok") {
		t.Errorf("expected 'ok' in output, got: %s", buf.String())
	}
}

func TestAgentFormat_WithErrors(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC002",
			Severity: types.SeverityError,
			Message:  "webhook conversion not acknowledged",
			Source:   types.SourceLocation{File: "manifests/my-bucket.yaml", Line: 1, Column: 13},
			Detail:   "This resource is written at version v1beta1, but the storage version of CRD s3.aws.m.upbound.io.Bucket is v1beta2.",
			Fix:      "Re-author the resource at the storage version v1beta2:\n  apiVersion: s3.aws.m.upbound.io/v1beta2\n\nOr add annotation xpc.dev/accept-conversion-webhook: \"true\" to acknowledge.",
			Related: []types.SourceLocation{
				{File: "crds/bucket.yaml", Line: 14},
			},
		},
	}

	var buf bytes.Buffer
	err := reportAgent(&buf, diags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Check required fields
	if !strings.Contains(output, "XPC002") {
		t.Error("expected XPC002 code in output")
	}
	if !strings.Contains(output, "manifests/my-bucket.yaml:1:13") {
		t.Error("expected file location in output")
	}
	if !strings.Contains(output, "severity: error") {
		t.Error("expected severity field")
	}
	if !strings.Contains(output, "rule:") {
		t.Error("expected rule field")
	}
	if !strings.Contains(output, "fix:") {
		t.Error("expected fix field")
	}
	if !strings.Contains(output, "docs:") {
		t.Error("expected docs field")
	}
	if !strings.Contains(output, "1 error") {
		t.Error("expected error count in summary")
	}
}

func TestAgentFormat_WithWarnings(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC007",
			Severity: types.SeverityWarning,
			Message:  "Argo CD label tracking conflicts with Crossplane",
			Source:   types.SourceLocation{File: "app.yaml", Line: 1},
			Fix:      "Switch to annotation tracking",
		},
	}

	var buf bytes.Buffer
	err := reportAgent(&buf, diags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "severity: warning") {
		t.Error("expected warning severity")
	}
	if !strings.Contains(output, "0 error") {
		t.Error("expected 0 errors in summary")
	}
	if !strings.Contains(output, "1 warning") {
		t.Error("expected 1 warning in summary")
	}
}

func TestHumanFormat(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC001",
			Severity: types.SeverityError,
			Message:  "version coherence error",
			Source:   types.SourceLocation{File: "crd.yaml", Line: 5},
		},
	}

	var buf bytes.Buffer
	err := reportHuman(&buf, diags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "XPC001") {
		t.Error("expected XPC001 in human output")
	}
}

func TestSARIFFormat(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC002",
			Severity: types.SeverityError,
			Message:  "test error",
			Source:   types.SourceLocation{File: "test.yaml", Line: 1},
		},
	}

	var buf bytes.Buffer
	err := reportSARIF(&buf, diags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "sarif") {
		t.Error("expected SARIF schema reference")
	}
	if !strings.Contains(output, "XPC002") {
		t.Error("expected XPC002 in SARIF output")
	}
}

func TestJSONFormat(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC003",
			Severity: types.SeverityError,
			Message:  "test",
			Source:   types.SourceLocation{File: "test.yaml", Line: 1},
		},
	}

	var buf bytes.Buffer
	err := reportJSON(&buf, diags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "XPC003") {
		t.Error("expected XPC003 in JSON output")
	}
}

func TestReport_UnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	err := Report(&buf, nil, "unknown")
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestReport_AllFormats(t *testing.T) {
	diags := []types.Diagnostic{
		{
			Code:     "XPC001",
			Severity: types.SeverityError,
			Message:  "test",
			Source:   types.SourceLocation{File: "t.yaml", Line: 1},
		},
	}

	formats := []Format{FormatHuman, FormatAgent, FormatJSON, FormatJUnit, FormatSARIF, FormatLSP}
	for _, f := range formats {
		var buf bytes.Buffer
		err := Report(&buf, diags, f)
		if err != nil {
			t.Errorf("format %s failed: %v", f, err)
		}
		if buf.Len() == 0 {
			t.Errorf("format %s produced empty output", f)
		}
	}
}
