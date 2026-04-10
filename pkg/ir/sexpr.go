package ir

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// DigestWorld computes a content-addressed digest of the World.
func DigestWorld(w *types.World) string {
	sexpr := ToSExpr(w)
	sum := sha256.Sum256([]byte(sexpr))
	return fmt.Sprintf("sha256:%x", sum)
}

// ToSExpr serializes the World to the .xpcir s-expression format.
func ToSExpr(w *types.World) string {
	var sb strings.Builder
	sb.WriteString("(xpcir-version 1)\n\n")

	// CRDs
	for _, crd := range w.CRDs {
		writeCRDSExpr(&sb, crd, false)
	}

	// XRDs
	for _, xrd := range w.XRDs {
		writeCRDSExpr(&sb, xrd, true)
	}

	// Schemas
	for digest, schema := range w.Schemas {
		sb.WriteString(fmt.Sprintf("(schema %q\n", digest))
		writeSchemaValue(&sb, schema.Schema, 1)
		sb.WriteString(")\n\n")
	}

	// Compositions
	for _, comp := range w.Compositions {
		writeCompositionSExpr(&sb, comp)
	}

	// Functions
	for _, fn := range w.Functions {
		sb.WriteString(fmt.Sprintf("(function\n  (name %q)\n  (package %q)\n", fn.Name, fn.Package))
		if len(fn.InputVersions) > 0 {
			sb.WriteString("  (input-versions (")
			for i, v := range fn.InputVersions {
				if i > 0 {
					sb.WriteString(" ")
				}
				sb.WriteString(fmt.Sprintf("%q", v))
			}
			sb.WriteString("))\n")
		}
		writeSourceSExpr(&sb, fn.Source, 1)
		sb.WriteString(")\n\n")
	}

	// Providers
	for _, prov := range w.Providers {
		sb.WriteString(fmt.Sprintf("(provider\n  (name %q)\n  (package %q)\n", prov.Name, prov.Package))
		writeSourceSExpr(&sb, prov.Source, 1)
		sb.WriteString(")\n\n")
	}

	// Configurations
	for _, cfg := range w.Configurations {
		sb.WriteString(fmt.Sprintf("(configuration\n  (name %q)\n  (package %q)\n", cfg.Name, cfg.Package))
		writeSourceSExpr(&sb, cfg.Source, 1)
		sb.WriteString(")\n\n")
	}

	// Resources
	for _, res := range w.Resources {
		sb.WriteString(fmt.Sprintf("(resource\n  (api-version %q)\n  (kind %q)\n  (name %q)\n",
			res.APIVersion, res.Kind, res.Name))
		if res.Namespace != "" {
			sb.WriteString(fmt.Sprintf("  (namespace %q)\n", res.Namespace))
		}
		if len(res.Annotations) > 0 {
			sb.WriteString("  (annotations\n")
			for k, v := range res.Annotations {
				sb.WriteString(fmt.Sprintf("    (%q %q)\n", k, v))
			}
			sb.WriteString("  )\n")
		}
		writeSourceSExpr(&sb, res.Source, 1)
		sb.WriteString(")\n\n")
	}

	// Argo Applications
	for _, app := range w.ArgoApps {
		sb.WriteString(fmt.Sprintf("(argo-application\n  (name %q)\n  (tracking-mode %s)\n",
			app.Name, app.TrackingMode))
		if len(app.SyncWaves) > 0 {
			sb.WriteString("  (sync-waves\n")
			for _, sw := range app.SyncWaves {
				sb.WriteString(fmt.Sprintf("    (resource (kind %q) (name %q) (wave %d))\n",
					sw.Kind, sw.Name, sw.Wave))
			}
			sb.WriteString("  )\n")
		}
		writeSourceSExpr(&sb, app.Source, 1)
		sb.WriteString(")\n\n")
	}

	return sb.String()
}

func writeCRDSExpr(sb *strings.Builder, crd types.CRDInfo, isXRD bool) {
	tag := "crd"
	if isXRD {
		tag = "xrd"
	}
	sb.WriteString(fmt.Sprintf("(%s\n  (group %q)\n  (kind %q)\n  (scope %s)\n",
		tag, crd.Group, crd.Kind, crd.Scope))

	if isXRD && crd.APIVersion != "" {
		sb.WriteString(fmt.Sprintf("  (api-version %q)\n", crd.APIVersion))
	}

	sb.WriteString("  (versions\n")
	for _, v := range crd.Versions {
		sb.WriteString(fmt.Sprintf("    (version (name %q) (served %t) (storage %t)",
			v.Name, v.Served, v.Storage))
		if isXRD {
			sb.WriteString(fmt.Sprintf(" (referenceable %t)", v.Referenceable))
		}
		if v.SchemaDigest != "" {
			sb.WriteString(fmt.Sprintf("\n             (schema-ref %q)", v.SchemaDigest))
		}
		sb.WriteString(")\n")
	}
	sb.WriteString("  )\n")

	if !isXRD {
		sb.WriteString(fmt.Sprintf("  (conversion\n    (strategy %s)\n    (cost-class %s)",
			crd.Conversion.Strategy, crd.Conversion.CostClass))
		if crd.Conversion.WebhookService != "" {
			sb.WriteString(fmt.Sprintf("\n    (webhook-service %q)", crd.Conversion.WebhookService))
		}
		sb.WriteString(")\n")
	}

	writeSourceSExpr(sb, crd.Source, 1)
	sb.WriteString(")\n\n")
}

func writeCompositionSExpr(sb *strings.Builder, comp types.CompositionInfo) {
	sb.WriteString(fmt.Sprintf("(composition\n  (name %q)\n", comp.Name))
	sb.WriteString(fmt.Sprintf("  (composite-type-ref (api-version %q) (kind %q))\n",
		comp.CompositeTypeRef.Group+"/"+comp.CompositeTypeRef.Version,
		comp.CompositeTypeRef.Kind))
	sb.WriteString(fmt.Sprintf("  (mode %s)\n", comp.Mode))

	if comp.Mode == "Pipeline" {
		sb.WriteString("  (pipeline\n")
		for _, step := range comp.Pipeline {
			sb.WriteString(fmt.Sprintf("    (step (name %q)\n", step.Name))
			sb.WriteString(fmt.Sprintf("          (function-ref %q)\n", step.FunctionRef))
			if step.InputAPIVersion != "" {
				sb.WriteString(fmt.Sprintf("          (input-api-version %q)\n", step.InputAPIVersion))
				sb.WriteString(fmt.Sprintf("          (input-kind %q)", step.InputKind))
			}
			sb.WriteString(")\n")
		}
		sb.WriteString("  )\n")
	}

	writeSourceSExpr(sb, comp.Source, 1)
	sb.WriteString(")\n\n")
}

func writeSourceSExpr(sb *strings.Builder, src types.SourceLocation, indent int) {
	prefix := strings.Repeat("  ", indent)
	sb.WriteString(fmt.Sprintf("%s(source (file %q) (line %d))\n", prefix, src.File, src.Line))
}

func writeSchemaValue(sb *strings.Builder, schema map[string]interface{}, indent int) {
	prefix := strings.Repeat("  ", indent)
	for k, v := range schema {
		switch val := v.(type) {
		case map[string]interface{}:
			sb.WriteString(fmt.Sprintf("%s(%s\n", prefix, k))
			writeSchemaValue(sb, val, indent+1)
			sb.WriteString(fmt.Sprintf("%s)\n", prefix))
		case string:
			sb.WriteString(fmt.Sprintf("%s(%s %q)\n", prefix, k, val))
		case bool:
			sb.WriteString(fmt.Sprintf("%s(%s %t)\n", prefix, k, val))
		case float64:
			sb.WriteString(fmt.Sprintf("%s(%s %g)\n", prefix, k, val))
		default:
			sb.WriteString(fmt.Sprintf("%s(%s %v)\n", prefix, k, v))
		}
	}
}
