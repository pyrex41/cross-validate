package schemas

import (
	"fmt"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// ValidateManifest walks a concrete manifest against the provided schema
// subtree (which may be wrapped in `openAPIV3Schema`) and returns one fact
// per violation detected. It checks:
//
//   - UnknownField: a key appears in the manifest but not in the schema's
//     properties (only fired when the schema has `additionalProperties: false`).
//   - WrongType: scalar type mismatch (string vs integer/number vs boolean).
//   - MissingRequired: a schema `required` entry is absent from the manifest.
//   - InvalidEnum: a scalar value is not in the schema's enum list.
//
// Array elements are walked via `items`; each element's path is suffixed with
// "[i]" so e.g. a wrong-typed tag becomes `spec.tags[0]`.
//
// The schema argument is the raw sub-tree stored on the World (still wrapped
// in `{openAPIV3Schema: {...}}` for both CRDs and XRDs); this function
// unwraps it.
func ValidateManifest(schema map[string]interface{}, raw map[string]interface{}) []types.ResourceFieldFact {
	if schema == nil || raw == nil {
		return nil
	}

	root := schema
	if oapi := getNestedMap(root, "openAPIV3Schema"); oapi != nil {
		root = oapi
	}

	// The manifest root has top-level keys (apiVersion, kind, metadata, spec,
	// etc.). Walk each top-level key against the corresponding property in
	// the schema. This mirrors how kube-apiserver validates CRs — only the
	// fields defined by the schema are checked; apiVersion/kind/metadata are
	// structural and aren't declared in the schema's properties.
	var out []types.ResourceFieldFact

	// Check required at root.
	out = append(out, checkRequired(root, raw, "")...)

	props := getNestedMap(root, "properties")
	if props == nil {
		return out
	}

	for fieldName, fieldSchemaRaw := range props {
		fieldSchema, ok := fieldSchemaRaw.(map[string]interface{})
		if !ok {
			continue
		}
		val, present := raw[fieldName]
		if !present {
			continue
		}
		out = append(out, walkField(fieldName, fieldSchema, val)...)
	}

	// additionalProperties: false at the root would catch unknown top-level
	// fields, but for a CR the top level is structurally fixed. We still honor
	// it if the schema explicitly sets additionalProperties=false at the root.
	if additionalPropertiesFalse(root) {
		for key := range raw {
			if _, ok := props[key]; !ok {
				// apiVersion/kind/metadata/status are structural and exempt.
				if key == "apiVersion" || key == "kind" || key == "metadata" || key == "status" {
					continue
				}
				out = append(out, types.ResourceFieldFact{
					Path:      key,
					Violation: types.ViolationUnknownField,
					Message:   fmt.Sprintf("unknown field %q (schema has additionalProperties: false)", key),
				})
			}
		}
	}

	return out
}

// walkField validates a single value against its corresponding schema node.
// path is the dotted path into the manifest (e.g. "spec.color", "spec.tags[0]").
func walkField(path string, schema map[string]interface{}, val interface{}) []types.ResourceFieldFact {
	var out []types.ResourceFieldFact

	schemaType, _ := schema["type"].(string)

	// Type check.
	if schemaType != "" && val != nil {
		actual := jsonType(val)
		if !typeCompatible(actual, schemaType) {
			out = append(out, types.ResourceFieldFact{
				Path:      path,
				Violation: types.ViolationWrongType,
				Message:   fmt.Sprintf("field %q has type %s, want %s", path, actual, schemaType),
			})
			// If scalar type mismatches, don't attempt deeper checks on it.
			return out
		}
	}

	// Enum check — applies to scalar values.
	if enum, ok := schema["enum"].([]interface{}); ok && val != nil {
		matched := false
		for _, e := range enum {
			if equalScalar(e, val) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, types.ResourceFieldFact{
				Path:      path,
				Violation: types.ViolationInvalidEnum,
				Message:   fmt.Sprintf("field %q value %v not in enum %v", path, val, enum),
			})
			return out
		}
	}

	switch schemaType {
	case "object":
		valMap, ok := val.(map[string]interface{})
		if !ok {
			return out
		}
		out = append(out, checkRequired(schema, valMap, path)...)

		props := getNestedMap(schema, "properties")
		// Check known fields.
		if props != nil {
			for fname, fschemaRaw := range props {
				fschema, ok := fschemaRaw.(map[string]interface{})
				if !ok {
					continue
				}
				if sub, ok := valMap[fname]; ok {
					subPath := path + "." + fname
					if path == "" {
						subPath = fname
					}
					out = append(out, walkField(subPath, fschema, sub)...)
				}
			}
		}
		// Check unknown fields when additionalProperties: false.
		if additionalPropertiesFalse(schema) {
			for key := range valMap {
				if props != nil {
					if _, ok := props[key]; ok {
						continue
					}
				}
				subPath := path + "." + key
				if path == "" {
					subPath = key
				}
				out = append(out, types.ResourceFieldFact{
					Path:      subPath,
					Violation: types.ViolationUnknownField,
					Message:   fmt.Sprintf("unknown field %q (schema has additionalProperties: false)", subPath),
				})
			}
		}
	case "array":
		arr, ok := val.([]interface{})
		if !ok {
			return out
		}
		items := getNestedMap(schema, "items")
		if items == nil {
			return out
		}
		for i, el := range arr {
			elPath := fmt.Sprintf("%s[%d]", path, i)
			out = append(out, walkField(elPath, items, el)...)
		}
	}

	return out
}

// checkRequired walks schema.required and emits MissingRequired facts for
// any entry absent from the raw map.
func checkRequired(schema map[string]interface{}, raw map[string]interface{}, parentPath string) []types.ResourceFieldFact {
	reqRaw, ok := schema["required"].([]interface{})
	if !ok {
		return nil
	}
	var out []types.ResourceFieldFact
	for _, r := range reqRaw {
		name, ok := r.(string)
		if !ok {
			continue
		}
		if _, present := raw[name]; present {
			continue
		}
		path := name
		if parentPath != "" {
			path = parentPath + "." + name
		}
		out = append(out, types.ResourceFieldFact{
			Path:      path,
			Violation: types.ViolationMissingRequired,
			Message:   fmt.Sprintf("required field %q is missing", path),
		})
	}
	return out
}

// additionalPropertiesFalse returns true if the schema explicitly sets
// additionalProperties to the boolean false.
func additionalPropertiesFalse(schema map[string]interface{}) bool {
	ap, ok := schema["additionalProperties"]
	if !ok {
		return false
	}
	b, ok := ap.(bool)
	if !ok {
		return false
	}
	return !b
}

// jsonType returns the OpenAPI-style type name for a decoded YAML/JSON value.
func jsonType(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int32, int64, uint, uint32, uint64:
		return "integer"
	case float32, float64:
		// YAML unmarshal typically produces float64 for numbers. Distinguish
		// integers-as-floats from true decimals so integer fields still match.
		f, ok := v.(float64)
		if ok && f == float64(int64(f)) {
			return "integer"
		}
		return "number"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case nil:
		return "null"
	}
	return "unknown"
}

// typeCompatible returns true when `actual` satisfies the schema's declared
// `want` type. Integers satisfy `number`.
func typeCompatible(actual, want string) bool {
	if actual == want {
		return true
	}
	if actual == "integer" && want == "number" {
		return true
	}
	return false
}

// equalScalar compares two scalar values loosely (string==string, numeric
// equivalence for integer-vs-float).
func equalScalar(a, b interface{}) bool {
	if a == b {
		return true
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as == bs
	}
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return false
}

func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
