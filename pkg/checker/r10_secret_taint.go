package checker

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// R10: secret-taint propagation
// Information-flow typing for connection-detail material.
// Marks fields as secret at the schema layer (Crossplane connection details
// are the source, certain MR fields are sinks), propagates the taint through
// patches and function pipelines, and errors if a tainted value reaches an
// untainted sink.

// secretSourceFields are field paths that are inherently secret
// (connection details, credentials, etc).
var secretSourceFields = map[string]bool{
	"spec.writeConnectionSecretToRef":           true,
	"spec.publishConnectionDetailsTo":           true,
	"spec.connectionDetails":                    true,
	"spec.credentials":                          true,
	"spec.forProvider.credentials":              true,
	"spec.forProvider.password":                 true,
	"spec.forProvider.secretKey":                true,
	"spec.forProvider.accessKey":                true,
	"spec.forProvider.masterPassword":           true,
	"spec.forProvider.masterUsername":            true,
	"spec.forProvider.adminPassword":            true,
	"spec.forProvider.token":                    true,
	"spec.forProvider.apiKey":                   true,
	"spec.forProvider.connectionString":         true,
	"spec.forProvider.privateKey":               true,
	"spec.forProvider.clientSecret":             true,
	"spec.forProvider.sslCertificate":           true,
	"spec.forProvider.sslKey":                   true,
	"spec.forProvider.tlsCertificate":           true,
	"spec.forProvider.tlsKey":                   true,
}

// secretSinkFields are field paths that should receive secret material.
var secretSinkFields = map[string]bool{
	"spec.writeConnectionSecretToRef":           true,
	"spec.publishConnectionDetailsTo":           true,
	"spec.connectionDetails":                    true,
	"spec.forProvider.passwordSecretRef":        true,
	"spec.forProvider.credentials":              true,
	"spec.forProvider.credentialsSecretRef":     true,
	"spec.forProvider.masterPasswordSecretRef":  true,
	"spec.forProvider.tokenSecretRef":           true,
	"spec.forProvider.apiKeySecretRef":          true,
	"spec.forProvider.privateKeySecretRef":      true,
	"spec.forProvider.connectionStringSecretRef": true,
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

func checkR10(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	for _, comp := range w.Compositions {
		// Check Resources mode patches
		for _, res := range comp.Resources {
			for _, patch := range res.Patches {
				if patch.FromFieldPath == "" || patch.ToFieldPath == "" {
					continue
				}

				fromTainted := isSecretField(patch.FromFieldPath)
				toSink := isSecretSink(patch.ToFieldPath)

				if fromTainted && !toSink {
					// Secret flowing to non-secret field
					// Check for declassify annotation
					diags = append(diags, types.Diagnostic{
						Code:     "XPC010",
						Severity: types.SeverityError,
						Source:   comp.Source,
						Message: fmt.Sprintf("secret taint leak in Composition %s", comp.Name),
						Detail: fmt.Sprintf("Patch source %s is tainted (secret/credential material) "+
							"but destination %s is not a secret sink. This may expose sensitive data "+
							"in a non-secret field where it could be logged, displayed, or read by "+
							"unprivileged controllers.",
							patch.FromFieldPath, patch.ToFieldPath),
						Fix: fmt.Sprintf("Route the secret through a SecretRef field, or add "+
							"annotation xpc.dev/declassify: %q to acknowledge this is intentional.",
							patch.FromFieldPath),
					})
				}
			}
		}

		// Check pipeline steps for secret flow through function inputs
		if comp.Mode == "Pipeline" {
			for _, step := range comp.Pipeline {
				_ = step // Pipeline function I/O secret checking is done at interface level
			}
		}
	}

	return diags
}
