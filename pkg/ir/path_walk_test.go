package ir

import "testing"

func TestWalkPath_ScalarPath(t *testing.T) {
	raw := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "x",
		},
	}
	hits := WalkPath(raw, "a.b")
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Path != "a.b" {
		t.Errorf("expected Path=\"a.b\", got %q", hits[0].Path)
	}
	if hits[0].Value != "x" {
		t.Errorf("expected Value=\"x\", got %v", hits[0].Value)
	}
}

func TestWalkPath_WildcardStar(t *testing.T) {
	raw := map[string]interface{}{
		"xs": []interface{}{
			map[string]interface{}{"k": "a"},
			map[string]interface{}{"k": "b"},
		},
	}
	hits := WalkPath(raw, "xs[*].k")
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(hits), hits)
	}
	wantPaths := map[string]string{"xs[0].k": "a", "xs[1].k": "b"}
	for _, h := range hits {
		want, ok := wantPaths[h.Path]
		if !ok {
			t.Errorf("unexpected path %q", h.Path)
			continue
		}
		if h.Value != want {
			t.Errorf("path %q: expected %q, got %v", h.Path, want, h.Value)
		}
	}
}

func TestWalkPath_WildcardEmptyBrackets(t *testing.T) {
	// Registry syntax uses "[]" as a wildcard placeholder; it should be
	// equivalent to "[*]".
	raw := map[string]interface{}{
		"xs": []interface{}{
			map[string]interface{}{"k": "a"},
			map[string]interface{}{"k": "b"},
		},
	}
	hits := WalkPath(raw, "xs[].k")
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(hits), hits)
	}
	if hits[0].Path != "xs[0].k" || hits[1].Path != "xs[1].k" {
		t.Errorf("expected rendered indices, got %q and %q", hits[0].Path, hits[1].Path)
	}
}

func TestWalkPath_ExplicitIndex(t *testing.T) {
	raw := map[string]interface{}{
		"xs": []interface{}{
			map[string]interface{}{"k": "a"},
			map[string]interface{}{"k": "b"},
		},
	}
	hits := WalkPath(raw, "xs[0].k")
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Path != "xs[0].k" || hits[0].Value != "a" {
		t.Errorf("unexpected hit: %+v", hits[0])
	}
}

func TestWalkPath_MissingPath(t *testing.T) {
	raw := map[string]interface{}{}
	if hits := WalkPath(raw, "a.b"); hits != nil {
		t.Errorf("expected nil hits for missing path, got %+v", hits)
	}
}

func TestWalkPath_ThroughNonObject(t *testing.T) {
	raw := map[string]interface{}{"a": "string"}
	if hits := WalkPath(raw, "a.b"); hits != nil {
		t.Errorf("expected nil hits when traversing through a scalar, got %+v", hits)
	}
}

func TestWalkPath_OutOfRangeIndex(t *testing.T) {
	raw := map[string]interface{}{
		"xs": []interface{}{
			map[string]interface{}{"k": "a"},
		},
	}
	if hits := WalkPath(raw, "xs[5].k"); hits != nil {
		t.Errorf("expected nil hits for out-of-range index, got %+v", hits)
	}
}

func TestWalkPath_WildcardOnNonArray(t *testing.T) {
	raw := map[string]interface{}{"xs": map[string]interface{}{"k": "a"}}
	if hits := WalkPath(raw, "xs[*].k"); hits != nil {
		t.Errorf("expected nil hits when wildcard hits a non-array, got %+v", hits)
	}
}

func TestWalkPath_EmptyInputs(t *testing.T) {
	if hits := WalkPath(nil, "a"); hits != nil {
		t.Errorf("nil map: expected nil hits, got %+v", hits)
	}
	if hits := WalkPath(map[string]interface{}{"a": "x"}, ""); hits != nil {
		t.Errorf("empty path: expected nil hits, got %+v", hits)
	}
}

func TestWalkPath_NestedArraysAndMaps(t *testing.T) {
	// "spec.forProvider.launchTemplate[*].idSelector" analogue.
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"forProvider": map[string]interface{}{
				"launchTemplate": []interface{}{
					map[string]interface{}{
						"idSelector": map[string]interface{}{
							"matchLabels": map[string]interface{}{
								"env": "preview",
							},
						},
					},
					map[string]interface{}{
						"idSelector": map[string]interface{}{
							"matchLabels": map[string]interface{}{
								"env": "other",
							},
						},
					},
				},
			},
		},
	}
	hits := WalkPath(raw, "spec.forProvider.launchTemplate[*].idSelector")
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(hits), hits)
	}
	if hits[0].Path != "spec.forProvider.launchTemplate[0].idSelector" {
		t.Errorf("unexpected hit[0] path: %q", hits[0].Path)
	}
	if hits[1].Path != "spec.forProvider.launchTemplate[1].idSelector" {
		t.Errorf("unexpected hit[1] path: %q", hits[1].Path)
	}
}

func TestReadPath_Scalar(t *testing.T) {
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"forProvider": map[string]interface{}{
				"engine": "aurora-postgresql",
			},
		},
	}
	v, ok := ReadPath(raw, "spec.forProvider.engine")
	if !ok {
		t.Fatalf("expected hit, got none")
	}
	if v != "aurora-postgresql" {
		t.Errorf("expected aurora-postgresql, got %v", v)
	}
}

func TestReadPath_Missing(t *testing.T) {
	raw := map[string]interface{}{
		"spec": map[string]interface{}{},
	}
	if _, ok := ReadPath(raw, "spec.forProvider.engine"); ok {
		t.Errorf("expected no hit for missing intermediate segment")
	}
	if _, ok := ReadPath(raw, "spec.engine"); ok {
		t.Errorf("expected no hit for missing final segment")
	}
}

func TestReadPath_IntermediateNotMap(t *testing.T) {
	raw := map[string]interface{}{
		"spec": "scalar-at-intermediate",
	}
	if _, ok := ReadPath(raw, "spec.forProvider"); ok {
		t.Errorf("expected no hit when intermediate is not a map")
	}
}

func TestReadPath_ReturnsMap(t *testing.T) {
	inner := map[string]interface{}{"nested": "val"}
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"block": inner,
		},
	}
	v, ok := ReadPath(raw, "spec.block")
	if !ok {
		t.Fatalf("expected hit for sub-map lookup")
	}
	m, isMap := v.(map[string]interface{})
	if !isMap {
		t.Fatalf("expected map value, got %T", v)
	}
	if m["nested"] != "val" {
		t.Errorf("expected nested=val, got %v", m["nested"])
	}
}

func TestReadPath_EmptyInputs(t *testing.T) {
	if _, ok := ReadPath(nil, "spec"); ok {
		t.Errorf("expected no hit for nil map")
	}
	if _, ok := ReadPath(map[string]interface{}{"a": 1}, ""); ok {
		t.Errorf("expected no hit for empty path")
	}
}
