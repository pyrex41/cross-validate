// Package report formats diagnostics for output in multiple formats:
// human-readable, JSON, LSP, JUnit XML, and SARIF.
package report

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Format specifies the output format.
type Format string

const (
	FormatHuman Format = "human"
	FormatJSON  Format = "json"
	FormatLSP   Format = "lsp"
	FormatJUnit Format = "junit"
	FormatSARIF Format = "sarif"
)

// Report writes diagnostics to the given writer in the specified format.
func Report(w io.Writer, diags []types.Diagnostic, format Format) error {
	switch format {
	case FormatHuman:
		return reportHuman(w, diags)
	case FormatJSON:
		return reportJSON(w, diags)
	case FormatJUnit:
		return reportJUnit(w, diags)
	case FormatSARIF:
		return reportSARIF(w, diags)
	case FormatLSP:
		return reportLSP(w, diags)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

// ReportStdout writes diagnostics to stdout.
func ReportStdout(diags []types.Diagnostic, format Format) error {
	return Report(os.Stdout, diags, format)
}

// reportHuman writes pretty human-readable output with source excerpts.
func reportHuman(w io.Writer, diags []types.Diagnostic) error {
	if len(diags) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return nil
	}

	errors := 0
	warnings := 0
	for _, d := range diags {
		switch d.Severity {
		case types.SeverityError:
			errors++
		case types.SeverityWarning:
			warnings++
		}
	}

	for i, d := range diags {
		if i > 0 {
			fmt.Fprintln(w)
		}

		severityStr := string(d.Severity)
		loc := formatLocation(d.Source)

		fmt.Fprintf(w, "%s: %s: %s: %s\n", loc, severityStr, d.Code, d.Message)

		// Show source excerpt if file is available
		if d.Source.File != "" && d.Source.File != "<stdin>" {
			excerpt := getSourceExcerpt(d.Source.File, d.Source.Line, d.Source.Column)
			if excerpt != "" {
				fmt.Fprintln(w)
				fmt.Fprintln(w, excerpt)
			}
		}

		if d.Detail != "" {
			fmt.Fprintln(w)
			// Word-wrap detail at 80 chars
			for _, line := range wordWrap(d.Detail, 78) {
				fmt.Fprintf(w, "  %s\n", line)
			}
		}

		if len(d.Related) > 0 {
			fmt.Fprintln(w)
			for _, rel := range d.Related {
				fmt.Fprintf(w, "  Related: %s\n", formatLocation(rel))
			}
		}

		if d.Fix != "" {
			fmt.Fprintln(w)
			fmt.Fprintf(w, "  Fix:\n")
			for _, line := range strings.Split(d.Fix, "\n") {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}

		fmt.Fprintf(w, "\n  Why this rule exists: https://xpc.dev/errors/%s\n", d.Code)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Found %d error(s), %d warning(s).\n", errors, warnings)
	return nil
}

func formatLocation(src types.SourceLocation) string {
	if src.File == "" {
		return "<unknown>"
	}
	if src.Column > 0 {
		return fmt.Sprintf("%s:%d:%d", src.File, src.Line, src.Column)
	}
	return fmt.Sprintf("%s:%d", src.File, src.Line)
}

func getSourceExcerpt(file string, line, col int) string {
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}

	var sb strings.Builder
	sourceLine := lines[line-1]
	fmt.Fprintf(&sb, "  %d | %s\n", line, sourceLine)

	if col > 0 && col <= len(sourceLine) {
		// Find the extent of the value at the column
		linePrefix := fmt.Sprintf("  %d | ", line)
		padding := strings.Repeat(" ", len(linePrefix)+col-1)

		// Try to underline the relevant token
		end := col
		for end <= len(sourceLine) && sourceLine[end-1] != ' ' && sourceLine[end-1] != '\n' {
			end++
		}
		underline := strings.Repeat("^", end-col)
		fmt.Fprintf(&sb, "%s%s", padding, underline)
	}

	return sb.String()
}

func wordWrap(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	current := words[0]

	for _, word := range words[1:] {
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
		} else {
			current += " " + word
		}
	}
	lines = append(lines, current)
	return lines
}

// reportJSON writes diagnostics as a JSON array.
func reportJSON(w io.Writer, diags []types.Diagnostic) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(diags)
}

// JUnit XML structures

type junitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

func reportJUnit(w io.Writer, diags []types.Diagnostic) error {
	failures := 0
	var cases []junitTestCase
	for _, d := range diags {
		tc := junitTestCase{
			Name:      fmt.Sprintf("%s: %s", d.Code, d.Message),
			ClassName: d.Source.File,
		}
		if d.Severity == types.SeverityError {
			failures++
			tc.Failure = &junitFailure{
				Message: d.Message,
				Type:    d.Code,
				Text:    d.Detail,
			}
		}
		cases = append(cases, tc)
	}

	suites := junitTestSuites{
		Suites: []junitTestSuite{{
			Name:     "xpc",
			Tests:    len(diags),
			Failures: failures,
			Cases:    cases,
		}},
	}

	fmt.Fprint(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	return enc.Encode(suites)
}

// SARIF structures (simplified)

type sarifReport struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string           `json:"id"`
	ShortDescription sarifMessage     `json:"shortDescription"`
	HelpURI          string           `json:"helpUri"`
}

type sarifResult struct {
	RuleID    string            `json:"ruleId"`
	Level     string            `json:"level"`
	Message   sarifMessage      `json:"message"`
	Locations []sarifLocation   `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

func reportSARIF(w io.Writer, diags []types.Diagnostic) error {
	// Collect unique rule IDs
	ruleMap := make(map[string]bool)
	var rules []sarifRule
	for _, d := range diags {
		if !ruleMap[d.Code] {
			ruleMap[d.Code] = true
			rules = append(rules, sarifRule{
				ID:               d.Code,
				ShortDescription: sarifMessage{Text: d.Code},
				HelpURI:          fmt.Sprintf("https://xpc.dev/errors/%s", d.Code),
			})
		}
	}

	var results []sarifResult
	for _, d := range diags {
		level := "error"
		if d.Severity == types.SeverityWarning {
			level = "warning"
		} else if d.Severity == types.SeverityInfo {
			level = "note"
		}

		results = append(results, sarifResult{
			RuleID:  d.Code,
			Level:   level,
			Message: sarifMessage{Text: d.Message + "\n\n" + d.Detail},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: d.Source.File},
					Region:           sarifRegion{StartLine: d.Source.Line, StartColumn: d.Source.Column},
				},
			}},
		})
	}

	report := sarifReport{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{
				Driver: sarifDriver{
					Name:           "xpc",
					Version:        "0.1.0",
					InformationURI: "https://xpc.dev",
					Rules:          rules,
				},
			},
			Results: results,
		}},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// reportLSP writes diagnostics in LSP diagnostic format (JSON-RPC compatible).
func reportLSP(w io.Writer, diags []types.Diagnostic) error {
	type lspDiag struct {
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
		Severity int    `json:"severity"` // 1=Error, 2=Warning, 3=Info, 4=Hint
		Code     string `json:"code"`
		Source   string `json:"source"`
		Message  string `json:"message"`
	}

	// Group by file
	byFile := make(map[string][]lspDiag)
	for _, d := range diags {
		ld := lspDiag{
			Code:    d.Code,
			Source:  "xpc",
			Message: d.Message + "\n" + d.Detail,
		}
		ld.Range.Start.Line = d.Source.Line - 1 // LSP is 0-based
		ld.Range.Start.Character = max(0, d.Source.Column-1)
		ld.Range.End.Line = d.Source.Line - 1
		ld.Range.End.Character = ld.Range.Start.Character + 1

		switch d.Severity {
		case types.SeverityError:
			ld.Severity = 1
		case types.SeverityWarning:
			ld.Severity = 2
		default:
			ld.Severity = 3
		}

		byFile[d.Source.File] = append(byFile[d.Source.File], ld)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(byFile)
}
