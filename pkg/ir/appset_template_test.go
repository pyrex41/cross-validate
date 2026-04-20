package ir

import (
	"testing"
)

// TestSubstituteTemplate covers the happy path: plain {{ .key }} substitution
// with whitespace tolerance and a missing-key pass-through.
func TestSubstituteTemplate(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		params map[string]string
		want   string
		wantOK bool
	}{
		{
			name:   "basic",
			in:     "pr-{{ .number }}",
			params: map[string]string{"number": "42"},
			want:   "pr-42",
			wantOK: true,
		},
		{
			name:   "no-spaces",
			in:     "{{.name}}-app",
			params: map[string]string{"name": "x"},
			want:   "x-app",
			wantOK: true,
		},
		{
			name:   "missing-key preserved",
			in:     "{{ .missing }}-hi",
			params: map[string]string{"other": "ignored"},
			want:   "{{ .missing }}-hi",
			wantOK: true,
		},
		{
			name:   "empty input",
			in:     "",
			params: map[string]string{},
			want:   "",
			wantOK: true,
		},
		{
			name:   "without leading dot",
			in:     "name-{{ number }}",
			params: map[string]string{"number": "7"},
			want:   "name-7",
			wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := substituteTemplate(tc.in, tc.params)
			if ok != tc.wantOK {
				t.Errorf("supported = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSubstituteTemplate_UnsupportedSyntax verifies the "bail and flag it
// for the caller" behaviour when the source contains ranges, conditionals,
// or pipelines.
func TestSubstituteTemplate_UnsupportedSyntax(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"range", "{{range .items}}{{.}}{{end}}"},
		{"if", "{{if .cond}}yes{{end}}"},
		{"pipeline", "{{ .name | upper }}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := substituteTemplate(tc.in, map[string]string{"name": "x"})
			if ok {
				t.Errorf("expected supported=false for %q, got true (result %q)", tc.in, got)
			}
			if got != tc.in {
				t.Errorf("expected input unchanged when unsupported, got %q", got)
			}
		})
	}
}
