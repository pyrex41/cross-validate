package shen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseKernelFiles(t *testing.T) {
	kernelDir := "../../kernel"
	files := []string{
		"prelude.shen",
		"r1-versions.shen",
		"r2-conversion.shen",
		"r3-composition-resolves.shen",
		"r4-pipeline-functions.shen",
		"r5-patch-typecheck.shen",
		"r6-wave-ordering.shen",
		"r7-owner-refs.shen",
		"r8-v1v2-machinery.shen",
		"r9-bootstrap.shen",
		"r10-secret-taint.shen",
		"r11-temporal.shen",
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(kernelDir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		_, err = Parse(string(data))
		if err != nil {
			t.Errorf("parse %s: %v", f, err)
		}
	}
}

func TestParseBasicExpressions(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{`(+ 1 2)`, 1},
		{`[a b c]`, 1},
		{`[a | b]`, 1},
		{`"hello"`, 1},
		{`42`, 1},
		{`true`, 1},
		{`(define foo X -> X)`, 1},
		{`(let X 1 (+ X 2))`, 1},
	}
	for _, tt := range tests {
		vals, err := Parse(tt.input)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.input, err)
			continue
		}
		if len(vals) != tt.count {
			t.Errorf("Parse(%q): got %d values, want %d", tt.input, len(vals), tt.count)
		}
	}
}
