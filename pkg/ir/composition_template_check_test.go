package ir

import "testing"

func TestParseGoTemplateText(t *testing.T) {
	cases := []struct {
		name    string
		tmpl    string
		wantErr bool
	}{
		{
			name: "valid with undefined funcs and comments outside actions",
			// Uses sprig/gotemplating helpers xpc does NOT register (dict, list,
			// printf, toJson, getComposite) plus a well-formed standalone comment.
			// The self-bootstrapping FuncMap must let all of these through.
			tmpl: `{{- $xr := .observed.composite.resource -}}
{{- /* a standalone comment is fine */ -}}
{{- $c := list (dict "name" "x" "v" (printf "%s" $xr.spec.id)) -}}
{{ toJson $c }}
{{ getComposite }}`,
			wantErr: false,
		},
		{
			name: "comment nested inside an open action",
			tmpl: `{{- $c = list
  (dict
    "name" "x"
    {{- /* illegal: nested delimiter inside the open (dict ...) */ -}}
    "essential" false
  )
-}}`,
			wantErr: true,
		},
		{
			name: "hash comment inside an open action",
			tmpl: `{{- $c = list
  (dict
    "name" "x"
    # this YAML-style comment is illegal inside the action
    "essential" false
  )
-}}`,
			wantErr: true,
		},
		{
			name:    "hash in literal output text is fine",
			tmpl:    "metadata:\n  # this is plain output, not in an action\n  name: {{ .x }}\n",
			wantErr: false,
		},
		{
			name:    "unterminated action",
			tmpl:    `{{ $x := list (dict "a" 1 `,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseGoTemplateText(tc.tmpl)
			if tc.wantErr && err == nil {
				t.Fatalf("expected a parse error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}
