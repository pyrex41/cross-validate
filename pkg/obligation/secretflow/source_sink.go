// Package secretflow implements Category K (secret-flow) obligation generators.
package secretflow

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// secretSourceFields are field paths that are inherently secret
// (connection details, credentials, etc).
var secretSourceFields = map[string]bool{
	"spec.writeConnectionSecretToRef":       true,
	"spec.publishConnectionDetailsTo":       true,
	"spec.connectionDetails":                true,
	"spec.credentials":                      true,
	"spec.forProvider.credentials":          true,
	"spec.forProvider.password":             true,
	"spec.forProvider.secretKey":            true,
	"spec.forProvider.accessKey":            true,
	"spec.forProvider.masterPassword":       true,
	"spec.forProvider.masterUsername":        true,
	"spec.forProvider.adminPassword":        true,
	"spec.forProvider.token":                true,
	"spec.forProvider.apiKey":               true,
	"spec.forProvider.connectionString":     true,
	"spec.forProvider.privateKey":           true,
	"spec.forProvider.clientSecret":         true,
	"spec.forProvider.sslCertificate":       true,
	"spec.forProvider.sslKey":               true,
	"spec.forProvider.tlsCertificate":       true,
	"spec.forProvider.tlsKey":               true,
}

// secretSinkFields are field paths that should receive secret material.
var secretSinkFields = map[string]bool{
	"spec.writeConnectionSecretToRef":            true,
	"spec.publishConnectionDetailsTo":            true,
	"spec.connectionDetails":                     true,
	"spec.forProvider.passwordSecretRef":          true,
	"spec.forProvider.credentials":                true,
	"spec.forProvider.credentialsSecretRef":       true,
	"spec.forProvider.masterPasswordSecretRef":    true,
	"spec.forProvider.tokenSecretRef":             true,
	"spec.forProvider.apiKeySecretRef":            true,
	"spec.forProvider.privateKeySecretRef":        true,
	"spec.forProvider.connectionStringSecretRef":  true,
}

func isSecretField(path string) bool {
	if secretSourceFields[path] {
		return true
	}
	// Check by suffix patterns
	lower := strings.ToLower(path)
	secretPatterns := []string{
		"password", "secret", "credential", "token", "apikey",
		"privatekey", "accesskey", "secretkey", "connectionstring",
	}
	for _, pattern := range secretPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func isSecretSink(path string) bool {
	if secretSinkFields[path] {
		return true
	}
	lower := strings.ToLower(path)
	return strings.Contains(lower, "secretref") || strings.Contains(lower, "secret")
}

// SecretSourceSink is a Category K generator that checks whether secret-tainted
// field paths flow to non-secret sinks in Composition patches.
//
// Absorbs legacy rule R10 (XPC010).
type SecretSourceSink struct{}

var _ obligation.Generator = SecretSourceSink{}

func (SecretSourceSink) Name() string                  { return "secret-source-sink" }
func (SecretSourceSink) Category() obligation.Category { return obligation.CatSecretFlow }

func (SecretSourceSink) Description() string {
	return `Secret-tainted field paths must flow only to secret sinks.

A Composition patch copies from a field marked as secret (connection details,
credentials, passwords, keys, etc.) to a destination field that is not a
recognised secret sink. This may expose sensitive data in a non-secret field
where it could be logged, displayed, or read by unprivileged controllers.

Fix: Route the secret through a SecretRef field, or add an xpc.dev/declassify
annotation to acknowledge this is intentional.

Legacy code: XPC010`
}

// Generate emits one obligation per tainted patch in Resources-mode Compositions.
func (SecretSourceSink) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	var obs []obligation.Obligation
	for _, comp := range w.Compositions {
		for _, res := range comp.Resources {
			for _, patch := range res.Patches {
				if patch.FromFieldPath == "" || patch.ToFieldPath == "" {
					continue
				}

				fromTainted := isSecretField(patch.FromFieldPath)
				toSink := isSecretSink(patch.ToFieldPath)

				if fromTainted && !toSink {
					// Capture loop variables for closure
					comp := comp
					patch := patch
					instance := sanitizeInstance(comp.Name + "." + patch.FromFieldPath + "->" + patch.ToFieldPath)

					obs = append(obs, obligation.Obligation{
						ID:       obligation.MakeID(obligation.CatSecretFlow, "secret-source-sink", instance),
						Category: obligation.CatSecretFlow,
						Subject:  comp.Source,
						Claim: fmt.Sprintf("Composition %s patch from %s to %s does not leak secret material",
							comp.Name, patch.FromFieldPath, patch.ToFieldPath),
						Provenance: obligation.Provenance{
							Generator: "secret-source-sink",
							Category:  obligation.CatSecretFlow,
							InputHash: obligation.ContentHash(comp.Name + patch.FromFieldPath + patch.ToFieldPath),
						},
						LegacyCode: "XPC010",
						Discharge: func(ctx *obligation.Context) obligation.Result {
							return obligation.Result{
								Status: obligation.Violated,
								Diag: &types.Diagnostic{
									Code:     "XPC010",
									Severity: types.SeverityError,
									Source:   comp.Source,
									Message:  fmt.Sprintf("secret taint leak in Composition %s", comp.Name),
									Detail: fmt.Sprintf("Patch source %s is tainted (secret/credential material) "+
										"but destination %s is not a secret sink. This may expose sensitive data "+
										"in a non-secret field where it could be logged, displayed, or read by "+
										"unprivileged controllers.",
										patch.FromFieldPath, patch.ToFieldPath),
									Fix: fmt.Sprintf("Route the secret through a SecretRef field, or add "+
										"annotation xpc.dev/declassify: %q to acknowledge this is intentional.",
										patch.FromFieldPath),
								},
							}
						},
					})
				}
			}
		}
	}

	return obs
}

// sanitizeInstance cleans a resource name for use in an obligation ID.
func sanitizeInstance(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" {
		return "unnamed"
	}
	return name
}
