package schemas

import (
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// widgetSchema returns a schema map matching the fixture CRD used by R17
// (testdata/fixtures/resource-field-invalid/**/crd.yaml), suitable for
// ValidateManifest tests. The shape mirrors how CRD schemas are stored on
// World.Schemas: the `{openAPIV3Schema: {...}}` wrapper is preserved.
func widgetSchema() map[string]interface{} {
	return map[string]interface{}{
		"openAPIV3Schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"spec": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []interface{}{"name"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
						"size": map[string]interface{}{"type": "integer"},
						"color": map[string]interface{}{
							"type": "string",
							"enum": []interface{}{"red", "green", "blue"},
						},
						"tags": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
		},
	}
}

func findFact(facts []types.ResourceFieldFact, kind types.ViolationKind, path string) *types.ResourceFieldFact {
	for i := range facts {
		if facts[i].Violation == kind && facts[i].Path == path {
			return &facts[i]
		}
	}
	return nil
}

func TestValidateManifest_Happy(t *testing.T) {
	raw := map[string]interface{}{
		"apiVersion": "example.com/v1alpha1",
		"kind":       "Widget",
		"spec": map[string]interface{}{
			"name":  "gizmo",
			"size":  3,
			"color": "red",
			"tags":  []interface{}{"a", "b"},
		},
	}
	facts := ValidateManifest(widgetSchema(), raw)
	if len(facts) != 0 {
		t.Fatalf("expected no facts on happy path, got %d: %+v", len(facts), facts)
	}
}

func TestValidateManifest_UnknownFieldDepth1(t *testing.T) {
	// Root-level unknown fields are exempt because apiVersion/kind/metadata
	// are structural; to exercise depth-1 unknown-field detection we use the
	// spec.additionalProperties=false branch.
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"name":     "gizmo",
			"nonsense": 42,
		},
	}
	facts := ValidateManifest(widgetSchema(), raw)
	if findFact(facts, types.ViolationUnknownField, "spec.nonsense") == nil {
		t.Fatalf("expected UnknownField at spec.nonsense, got %+v", facts)
	}
}

func TestValidateManifest_UnknownFieldDepth2(t *testing.T) {
	schema := map[string]interface{}{
		"openAPIV3Schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"spec": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"nested": map[string]interface{}{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]interface{}{
								"known": map[string]interface{}{"type": "string"},
							},
						},
					},
				},
			},
		},
	}
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"nested": map[string]interface{}{
				"known":   "ok",
				"unknown": "surprise",
			},
		},
	}
	facts := ValidateManifest(schema, raw)
	if findFact(facts, types.ViolationUnknownField, "spec.nested.unknown") == nil {
		t.Fatalf("expected UnknownField at spec.nested.unknown, got %+v", facts)
	}
}

func TestValidateManifest_UnknownFieldDepth3(t *testing.T) {
	schema := map[string]interface{}{
		"openAPIV3Schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"spec": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"a": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"b": map[string]interface{}{
									"type":                 "object",
									"additionalProperties": false,
									"properties": map[string]interface{}{
										"ok": map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"a": map[string]interface{}{
				"b": map[string]interface{}{
					"ok":  "yes",
					"bad": "no",
				},
			},
		},
	}
	facts := ValidateManifest(schema, raw)
	if findFact(facts, types.ViolationUnknownField, "spec.a.b.bad") == nil {
		t.Fatalf("expected UnknownField at spec.a.b.bad, got %+v", facts)
	}
}

func TestValidateManifest_EnumMatch(t *testing.T) {
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"name":  "gizmo",
			"color": "blue",
		},
	}
	facts := ValidateManifest(widgetSchema(), raw)
	for _, f := range facts {
		if f.Violation == types.ViolationInvalidEnum {
			t.Fatalf("expected no InvalidEnum fact, got %+v", f)
		}
	}
}

func TestValidateManifest_EnumMiss(t *testing.T) {
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"name":  "gizmo",
			"color": "purple",
		},
	}
	facts := ValidateManifest(widgetSchema(), raw)
	if findFact(facts, types.ViolationInvalidEnum, "spec.color") == nil {
		t.Fatalf("expected InvalidEnum at spec.color, got %+v", facts)
	}
}

func TestValidateManifest_RequiredPresent(t *testing.T) {
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"name": "gizmo",
			"size": 2,
		},
	}
	facts := ValidateManifest(widgetSchema(), raw)
	for _, f := range facts {
		if f.Violation == types.ViolationMissingRequired {
			t.Fatalf("expected no MissingRequired fact, got %+v", f)
		}
	}
}

func TestValidateManifest_RequiredAbsent(t *testing.T) {
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"size": 2,
		},
	}
	facts := ValidateManifest(widgetSchema(), raw)
	if findFact(facts, types.ViolationMissingRequired, "spec.name") == nil {
		t.Fatalf("expected MissingRequired at spec.name, got %+v", facts)
	}
}

func TestValidateManifest_WrongTypeScalar(t *testing.T) {
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"name": 7, // want string
		},
	}
	facts := ValidateManifest(widgetSchema(), raw)
	if findFact(facts, types.ViolationWrongType, "spec.name") == nil {
		t.Fatalf("expected WrongType at spec.name, got %+v", facts)
	}
}

func TestValidateManifest_WrongTypeArrayElement(t *testing.T) {
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"name": "gizmo",
			"tags": []interface{}{"ok", 7},
		},
	}
	facts := ValidateManifest(widgetSchema(), raw)
	if findFact(facts, types.ViolationWrongType, "spec.tags[1]") == nil {
		t.Fatalf("expected WrongType at spec.tags[1], got %+v", facts)
	}
}

func TestValidateManifest_AdditionalPropertiesAllowed(t *testing.T) {
	schema := map[string]interface{}{
		"openAPIV3Schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"spec": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"known": map[string]interface{}{"type": "string"},
					},
					// additionalProperties omitted → allowed by default.
				},
			},
		},
	}
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"known":    "ok",
			"stranger": "fine because not additionalProperties:false",
		},
	}
	facts := ValidateManifest(schema, raw)
	if f := findFact(facts, types.ViolationUnknownField, "spec.stranger"); f != nil {
		t.Fatalf("expected no UnknownField (additionalProperties allowed), got %+v", f)
	}
}

func TestValidateManifest_IntegerSatisfiesNumber(t *testing.T) {
	schema := map[string]interface{}{
		"openAPIV3Schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"spec": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"cost": map[string]interface{}{"type": "number"},
					},
				},
			},
		},
	}
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"cost": 42,
		},
	}
	facts := ValidateManifest(schema, raw)
	if f := findFact(facts, types.ViolationWrongType, "spec.cost"); f != nil {
		t.Fatalf("expected integer to satisfy number, got %+v", f)
	}
}

func TestBuildSchemaIndex_XRDAndCRD(t *testing.T) {
	w := types.NewWorld()
	w.Schemas["digestA"] = types.SchemaInfo{Digest: "digestA", Schema: map[string]interface{}{"marker": "A"}}
	w.Schemas["digestB"] = types.SchemaInfo{Digest: "digestB", Schema: map[string]interface{}{"marker": "B"}}
	w.XRDs = append(w.XRDs, types.CRDInfo{
		Group: "example.com",
		Kind:  "XThing",
		Versions: []types.CRDVersion{
			{Name: "v1", Served: true, Referenceable: true, SchemaDigest: "digestA"},
		},
	})
	w.CRDs = append(w.CRDs, types.CRDInfo{
		Group: "example.com",
		Kind:  "Widget",
		Versions: []types.CRDVersion{
			{Name: "v1alpha1", Served: true, Storage: true, SchemaDigest: "digestB"},
		},
	})

	idx := BuildSchemaIndex(w)
	if got := idx[SchemaKey{APIVersion: "example.com/v1", Kind: "XThing"}]; got == nil || got["marker"] != "A" {
		t.Errorf("expected XThing→digestA schema, got %+v", got)
	}
	if got := idx[SchemaKey{APIVersion: "example.com/v1alpha1", Kind: "Widget"}]; got == nil || got["marker"] != "B" {
		t.Errorf("expected Widget→digestB schema, got %+v", got)
	}
}
