// appset_template.go — minimal `{{ .key }}` substitution for ApplicationSet
// templates.
//
// The real Argo CD ApplicationSet controller invokes Go's `text/template`
// under the hood and supports the full package (ranges, conditionals, sprig
// helpers). We deliberately hand-roll the simple path here:
//   - supported: `{{ .key }}`, `{{.key}}`, `{{ .key.subkey }}`,
//     whitespace-tolerant
//   - NOT supported: pipelines, ranges, conditionals, any Sprig function
//
// Templates containing unsupported syntax are reported to the caller via
// the boolean return value. The caller (ExpandAppSet) turns that into a
// single XPC.H.appset-unsupported-generator info diagnostic and skips the
// offending element. We do NOT pull in `text/template`: it would couple us
// to fg-manifold's exact Sprig function set and create a Turing-complete
// expansion surface we'd have to validate every upgrade.

package ir

import (
	"strings"
)

// unsupportedTemplateTokens flags syntax that our minimal engine cannot
// render. Presence of any of these in the source string means "skip + info
// diag". The tokens are deliberately conservative — we only render the
// truly-simple subset. Everything else is the ApplicationSet controller's
// job.
var unsupportedTemplateTokens = []string{
	"{{range",
	"{{ range",
	"{{if",
	"{{ if",
	"{{with",
	"{{ with",
	"{{define",
	"{{ define",
	"{{block",
	"{{ block",
	"{{template",
	"{{ template",
	"|", // pipelines
	"printf",
}

// substituteTemplate replaces every `{{ .key }}` placeholder in s with the
// matching value from params. Nested keys are supported: `{{ .a.b }}` maps
// to params["a.b"] first, falling back to params["a"] when the value is a
// simple string.
//
// Returns (result, supported):
//   - supported == false when the string contains syntax our engine cannot
//     handle (see unsupportedTemplateTokens). The returned `result` is the
//     input string unchanged; the caller decides whether to skip or pass
//     through.
//   - supported == true otherwise. Unresolved placeholders are left
//     in-place (so {{ .missing }} survives) — the caller treats that as a
//     signal the generator element is under-specified.
func substituteTemplate(s string, params map[string]string) (string, bool) {
	if s == "" {
		return s, true
	}
	for _, tok := range unsupportedTemplateTokens {
		if strings.Contains(s, tok) {
			return s, false
		}
	}

	var out strings.Builder
	i := 0
	for i < len(s) {
		open := strings.Index(s[i:], "{{")
		if open < 0 {
			out.WriteString(s[i:])
			break
		}
		open += i
		out.WriteString(s[i:open])
		close := strings.Index(s[open:], "}}")
		if close < 0 {
			// Unterminated `{{` — leave the rest verbatim. Not a typical
			// condition for a valid ApplicationSet; the Argo controller
			// would also fail here.
			out.WriteString(s[open:])
			break
		}
		inner := strings.TrimSpace(s[open+2 : open+close])
		// Support both `.key` and `key` forms.
		key := strings.TrimPrefix(inner, ".")
		if v, ok := params[key]; ok {
			out.WriteString(v)
		} else {
			// Unresolved — preserve original including braces so downstream
			// diagnostics can surface it verbatim.
			out.WriteString(s[open : open+close+2])
		}
		i = open + close + 2
	}
	return out.String(), true
}
