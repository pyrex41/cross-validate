package renderer

import (
	"encoding/json"
	"fmt"

	"github.com/pyrex41/cross-validate-/pkg/schemas"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// ValidateValues validates a merged Helm values map against a values.schema.json
// byte-slice. It reuses schemas.ValidateManifest — originally designed to walk
// a Kubernetes resource against its CRD/XRD schema, which already handles
// nested objects, enums, required, type. For Helm values:
//
//   - The typical values.schema.json does NOT set root additionalProperties:false,
//     so the CR-specific top-level exemption block (apiVersion/kind/metadata/status)
//     is never reached.
//   - If a chart author DOES set additionalProperties:false at root, those four
//     top-level keys will be silently exempted — a harmless quirk for charts since
//     they'd never collide with those names.
//
// Returns one ValuesIssue per schema violation, preserving the walker's path
// and message. Returns an error only when schemaJSON is structurally unparseable.
func ValidateValues(schemaJSON []byte, values map[string]interface{}) ([]types.ValuesIssue, error) {
	if len(schemaJSON) == 0 {
		return nil, nil
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return nil, fmt.Errorf("parsing values.schema.json: %w", err)
	}
	facts := schemas.ValidateManifest(schema, values)
	out := make([]types.ValuesIssue, 0, len(facts))
	for _, f := range facts {
		out = append(out, types.ValuesIssue{
			Path:    f.Path,
			Message: f.Message,
		})
	}
	return out, nil
}
