package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Format is one of the supported plan output formats.
type Format string

const (
	FormatJSON     Format = "json"
	FormatMarkdown Format = "markdown"
)

// jsonPlan is the wire shape for the JSON envelope. Fields kept small and
// regular so consumers (CI scripts, PR bot) can parse without a schema.
type jsonPlan struct {
	Base        jsonVariant     `json:"base"`
	Head        jsonVariant     `json:"head"`
	Delta       jsonDeltaCounts `json:"delta"`
	Destructive []jsonChange    `json:"destructive,omitempty"`
	Diagnostics jsonDiagSides   `json:"diagnostics"`
}

type jsonVariant struct {
	Ref string `json:"ref"`
}

type jsonDeltaCounts struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Modified int `json:"modified"`
}

type jsonChange struct {
	Code       string               `json:"code"`
	Message    string               `json:"message"`
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Namespace  string               `json:"namespace,omitempty"`
	Name       string               `json:"name"`
	App        string               `json:"app,omitempty"`
	Reason     string               `json:"reason,omitempty"`
	Source     types.SourceLocation `json:"source"`
}

type jsonDiagSides struct {
	Base []types.Diagnostic `json:"base"`
	Head []types.Diagnostic `json:"head"`
}

// WriteJSON emits the plan envelope as JSON. Destructive section is empty
// until R26 runs; populated via plan-time diagnostics whose code starts
// with "XPC.P.".
func WriteJSON(w io.Writer, p *Plan) error {
	out := jsonPlan{
		Base:  jsonVariant{Ref: p.Base.Ref},
		Head:  jsonVariant{Ref: p.Head.Ref},
		Delta: jsonDeltaCounts{len(p.Delta.Added), len(p.Delta.Removed), len(p.Delta.Modified)},
		Diagnostics: jsonDiagSides{
			Base: p.Base.Diagnostics,
			Head: p.Head.Diagnostics,
		},
	}
	for _, d := range p.Diagnostics {
		if !strings.HasPrefix(d.Code, "XPC.P.") {
			continue
		}
		out.Destructive = append(out.Destructive, jsonChangeFromDiagnostic(p.Delta, d))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func jsonChangeFromDiagnostic(delta ResourceDelta, d types.Diagnostic) jsonChange {
	change, ok := findChangeBySource(delta, d.Source)
	out := jsonChange{
		Code:    d.Code,
		Message: d.Message,
		Reason:  d.Detail,
		Source:  d.Source,
	}
	if !ok {
		return out
	}
	out.APIVersion = change.Identity.APIVersion
	out.Kind = change.Identity.Kind
	out.Namespace = change.Identity.Namespace
	out.Name = change.Identity.Name
	out.App = change.Identity.AppName
	return out
}

func findChangeBySource(delta ResourceDelta, src types.SourceLocation) (ResourceChange, bool) {
	if src == (types.SourceLocation{}) {
		return ResourceChange{}, false
	}
	for _, c := range delta.Removed {
		if c.BaseSource == src || c.HeadSource == src {
			return c, true
		}
	}
	for _, c := range delta.Modified {
		if c.BaseSource == src || c.HeadSource == src {
			return c, true
		}
	}
	for _, c := range delta.Added {
		if c.BaseSource == src || c.HeadSource == src {
			return c, true
		}
	}
	return ResourceChange{}, false
}

// WriteMarkdown emits a PR-comment-shaped plan summary. When destructive
// entries exist, they lead the output with a ⚠ header; otherwise the
// section is collapsed. Diagnostic counts are summary only — detailed
// per-code listing is left to the existing report formats for `xpc check`.
func WriteMarkdown(w io.Writer, p *Plan) error {
	fmt.Fprintf(w, "## xpc plan: %s → %s\n\n", p.Base.Ref, p.Head.Ref)

	destructive := filterPlanDiagnostics(p.Diagnostics, "XPC.P.")
	if len(destructive) > 0 {
		fmt.Fprintf(w, "### ⚠ Destructive changes (%d)\n\n", len(destructive))
		for _, d := range destructive {
			fmt.Fprintf(w, "- %s\n", d.Message)
			if d.Detail != "" {
				fmt.Fprintf(w, "  - %s\n", d.Detail)
			}
			if d.Fix != "" {
				fmt.Fprintf(w, "  - **Fix:** %s\n", d.Fix)
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "### Resource changes")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "- Added: %d\n", len(p.Delta.Added))
	fmt.Fprintf(w, "- Modified: %d\n", len(p.Delta.Modified))
	fmt.Fprintf(w, "- Removed: %d\n", len(p.Delta.Removed))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "### Per-tip diagnostics")
	fmt.Fprintln(w)
	baseErr, baseWarn, baseInfo := countBySeverity(p.Base.Diagnostics)
	headErr, headWarn, headInfo := countBySeverity(p.Head.Diagnostics)
	fmt.Fprintf(w, "- base (%s): %d errors, %d warnings, %d info\n", p.Base.Ref, baseErr, baseWarn, baseInfo)
	fmt.Fprintf(w, "- head (%s): %d errors, %d warnings, %d info\n", p.Head.Ref, headErr, headWarn, headInfo)

	return nil
}

func filterPlanDiagnostics(diags []types.Diagnostic, codePrefix string) []types.Diagnostic {
	var out []types.Diagnostic
	for _, d := range diags {
		if strings.HasPrefix(d.Code, codePrefix) {
			out = append(out, d)
		}
	}
	return out
}

func countBySeverity(diags []types.Diagnostic) (errs, warns, infos int) {
	for _, d := range diags {
		switch d.Severity {
		case types.SeverityError:
			errs++
		case types.SeverityWarning:
			warns++
		case types.SeverityInfo:
			infos++
		}
	}
	return
}
