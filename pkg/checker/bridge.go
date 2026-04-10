// Package checker bridges the Go side with the Shen type-checking kernel.
// It serializes the World to Shen-readable s-expressions, invokes the
// Shen kernel over stdin/stdout, and parses the judgment results back
// into Go diagnostics.
package checker

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Config holds checker configuration.
type Config struct {
	// KernelPath is the path to the Shen kernel directory.
	KernelPath string

	// ShenBinary is the path to the shen-cl binary.
	// If empty, falls back to the built-in Go checker.
	ShenBinary string

	// StrictConversions refuses webhook conversions entirely
	// instead of allowing them with an opt-in annotation.
	StrictConversions bool
}

// Check runs all type-checking rules against the World.
// If a Shen binary is available, it invokes the Shen kernel.
// Otherwise, it uses the built-in Go checker.
func Check(w *types.World, cfg Config) ([]types.Diagnostic, error) {
	if cfg.ShenBinary != "" {
		return checkWithShen(w, cfg)
	}
	return checkWithGo(w, cfg)
}

// checkWithShen invokes the Shen kernel process.
func checkWithShen(w *types.World, cfg Config) ([]types.Diagnostic, error) {
	ir := worldToShenIR(w)

	cmd := exec.Command(cfg.ShenBinary, "--eval", fmt.Sprintf(
		`(do (cd "%s") (load "check.shen") (check-world (read-from-string "%s")))`,
		cfg.KernelPath, escapeShen(ir)))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("shen kernel failed: %w\nstderr: %s", err, stderr.String())
	}

	return parseShenJudgments(stdout.String())
}

// checkWithGo is the built-in Go implementation of the type-checking rules.
// This provides the same checks as the Shen kernel but is available when
// no Shen runtime is installed.
func checkWithGo(w *types.World, cfg Config) ([]types.Diagnostic, error) {
	var diags []types.Diagnostic

	diags = append(diags, checkR1(w)...)
	diags = append(diags, checkR2(w, cfg.StrictConversions)...)
	diags = append(diags, checkR3(w)...)
	diags = append(diags, checkR4(w)...)
	diags = append(diags, checkR5(w)...)
	diags = append(diags, checkR6(w)...)
	diags = append(diags, checkR7(w)...)
	diags = append(diags, checkR8(w)...)
	diags = append(diags, checkR9(w)...)

	return diags, nil
}

// worldToShenIR serializes the World to Shen-readable s-expressions.
func worldToShenIR(w *types.World) string {
	var sb strings.Builder
	sb.WriteString("(world\n")

	// CRDs
	sb.WriteString("  (crds\n")
	for _, crd := range w.CRDs {
		sb.WriteString(fmt.Sprintf("    (crd-fact %q %q %q (",
			crd.Group, crd.Kind, crd.Scope))
		for i, v := range crd.Versions {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(fmt.Sprintf("(%q %t %t %q)",
				v.Name, v.Served, v.Storage, v.SchemaDigest))
		}
		sb.WriteString(fmt.Sprintf(") (%q %s %q) (source %q %d))\n",
			crd.Conversion.Strategy, strings.ToLower(string(crd.Conversion.CostClass)),
			crd.Conversion.WebhookService,
			crd.Source.File, crd.Source.Line))
	}
	sb.WriteString("  )\n")

	// XRDs
	sb.WriteString("  (xrds\n")
	for _, xrd := range w.XRDs {
		sb.WriteString(fmt.Sprintf("    (xrd-fact %q %q %q %q (",
			xrd.Group, xrd.Kind, xrd.Scope, xrd.APIVersion))
		for i, v := range xrd.Versions {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(fmt.Sprintf("(%q %t %t %q)",
				v.Name, v.Served, v.Referenceable, v.SchemaDigest))
		}
		sb.WriteString(fmt.Sprintf(") (source %q %d))\n",
			xrd.Source.File, xrd.Source.Line))
	}
	sb.WriteString("  )\n")

	// Compositions
	sb.WriteString("  (compositions\n")
	for _, comp := range w.Compositions {
		sb.WriteString(fmt.Sprintf("    (composition-fact %q (gvk %q %q %q) %q (",
			comp.Name,
			comp.CompositeTypeRef.Group, comp.CompositeTypeRef.Version,
			comp.CompositeTypeRef.Kind,
			comp.Mode))
		for i, step := range comp.Pipeline {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(fmt.Sprintf("(%q %q %q %q)",
				step.Name, step.FunctionRef, step.InputAPIVersion, step.InputKind))
		}
		sb.WriteString(fmt.Sprintf(") (source %q %d))\n",
			comp.Source.File, comp.Source.Line))
	}
	sb.WriteString("  )\n")

	// Functions
	sb.WriteString("  (functions\n")
	for _, fn := range w.Functions {
		sb.WriteString(fmt.Sprintf("    (function-fact %q %q (", fn.Name, fn.Package))
		for i, v := range fn.InputVersions {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(fmt.Sprintf("%q", v))
		}
		sb.WriteString(fmt.Sprintf(") (source %q %d))\n",
			fn.Source.File, fn.Source.Line))
	}
	sb.WriteString("  )\n")

	// Providers
	sb.WriteString("  (providers\n")
	for _, prov := range w.Providers {
		sb.WriteString(fmt.Sprintf("    (provider-fact %q %q (source %q %d))\n",
			prov.Name, prov.Package, prov.Source.File, prov.Source.Line))
	}
	sb.WriteString("  )\n")

	// Configurations
	sb.WriteString("  (configurations\n")
	for _, cfg := range w.Configurations {
		sb.WriteString(fmt.Sprintf("    (configuration-fact %q %q (source %q %d))\n",
			cfg.Name, cfg.Package, cfg.Source.File, cfg.Source.Line))
	}
	sb.WriteString("  )\n")

	// Resources
	sb.WriteString("  (resources\n")
	for _, res := range w.Resources {
		sb.WriteString(fmt.Sprintf("    (resource-fact %q %q %q %q (",
			res.APIVersion, res.Kind, res.Name, res.Namespace))
		for k, v := range res.Annotations {
			sb.WriteString(fmt.Sprintf("(%q %q) ", k, v))
		}
		sb.WriteString(fmt.Sprintf(") (source %q %d))\n",
			res.Source.File, res.Source.Line))
	}
	sb.WriteString("  )\n")

	// Argo Apps
	sb.WriteString("  (argo-apps\n")
	for _, app := range w.ArgoApps {
		sb.WriteString(fmt.Sprintf("    (argo-app-fact %q %q (",
			app.Name, app.TrackingMode))
		for _, sw := range app.SyncWaves {
			sb.WriteString(fmt.Sprintf("(%q %q %d) ", sw.Kind, sw.Name, sw.Wave))
		}
		sb.WriteString(fmt.Sprintf(") (source %q %d))\n",
			app.Source.File, app.Source.Line))
	}
	sb.WriteString("  )\n")

	// Schemas
	sb.WriteString("  (schemas)\n")
	sb.WriteString(")\n")

	return sb.String()
}

func escapeShen(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// parseShenJudgments parses the Shen kernel's stdout output.
func parseShenJudgments(output string) ([]types.Diagnostic, error) {
	// The Shen kernel outputs:
	// (judgments (judgment Code Sev (source File Line) Msg Detail Fix Related) ...)
	// For now, parse it loosely. A more robust parser would use a proper s-expr parser.
	var diags []types.Diagnostic

	// Split by "(judgment " and parse each
	parts := strings.Split(output, "(judgment ")
	for _, part := range parts[1:] { // skip first empty part
		diag, err := parseSingleJudgment(part)
		if err != nil {
			continue // skip malformed judgments
		}
		diags = append(diags, diag)
	}

	return diags, nil
}

func parseSingleJudgment(s string) (types.Diagnostic, error) {
	// Very simplified parser — in production, use a proper s-expr parser
	// Format: Code Sev (source File Line) Msg Detail Fix Related)
	var d types.Diagnostic
	// Extract quoted strings
	quotes := extractQuoted(s)
	if len(quotes) < 4 {
		return d, fmt.Errorf("too few fields in judgment")
	}

	d.Code = quotes[0]
	d.Message = quotes[1]
	d.Detail = quotes[2]
	d.Fix = quotes[3]

	if strings.Contains(s, "error") {
		d.Severity = types.SeverityError
	} else if strings.Contains(s, "warn") {
		d.Severity = types.SeverityWarning
	} else {
		d.Severity = types.SeverityInfo
	}

	return d, nil
}

func extractQuoted(s string) []string {
	var results []string
	inQuote := false
	var current strings.Builder
	escaped := false

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && inQuote {
			escaped = true
			continue
		}
		if r == '"' {
			if inQuote {
				results = append(results, current.String())
				current.Reset()
			}
			inQuote = !inQuote
			continue
		}
		if inQuote {
			current.WriteRune(r)
		}
	}
	return results
}
