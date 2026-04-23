package plan_test

import (
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/plan"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestR27_ImmutableChange_Positive(t *testing.T) {
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-immutable-change/base",
		"../../testdata/fixtures/plan-immutable-change/head",
	)

	// Same identity on both sides → 1 modified, 0 added/removed.
	if got := len(p.Delta.Modified); got != 1 {
		t.Fatalf("expected 1 modified resource, got %d: %+v", got, p.Delta.Modified)
	}
	if got := len(p.Delta.Added) + len(p.Delta.Removed); got != 0 {
		t.Errorf("expected 0 added/removed, got %d added + %d removed",
			len(p.Delta.Added), len(p.Delta.Removed))
	}

	var immut []types.Diagnostic
	for _, d := range p.Diagnostics {
		if d.Code == "XPC.P.immutable-change" {
			immut = append(immut, d)
		}
	}
	if len(immut) != 1 {
		t.Fatalf("expected 1 XPC.P.immutable-change, got %d: %+v", len(immut), p.Diagnostics)
	}
	if immut[0].Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", immut[0].Severity)
	}
	if !strings.Contains(immut[0].Message, "spec.forProvider.engine") {
		t.Errorf("expected engine field path in message, got %q", immut[0].Message)
	}
	if !strings.Contains(immut[0].Message, "aurora-immutable-test") {
		t.Errorf("expected resource name in message, got %q", immut[0].Message)
	}
}

func TestR27_ImmutableChange_Unchanged(t *testing.T) {
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-immutable-change-ok/base",
		"../../testdata/fixtures/plan-immutable-change-ok/head",
	)

	// Sanity: the fixture should yield exactly 1 modified (retention changed).
	if got := len(p.Delta.Modified); got != 1 {
		t.Fatalf("expected 1 modified resource, got %d: %+v", got, p.Delta.Modified)
	}

	for _, d := range p.Diagnostics {
		if d.Code == "XPC.P.immutable-change" {
			t.Errorf("expected no XPC.P.immutable-change on unrelated-field change, got %+v", d)
		}
	}
}

func TestR27_ImmutableChange_Bypass(t *testing.T) {
	p := runHermeticPlan(t,
		"../../testdata/fixtures/plan-immutable-change-bypass/base",
		"../../testdata/fixtures/plan-immutable-change-bypass/head",
	)

	if got := len(p.Delta.Modified); got != 1 {
		t.Fatalf("expected 1 modified resource, got %d: %+v", got, p.Delta.Modified)
	}
	for _, d := range p.Diagnostics {
		if d.Code == "XPC.P.immutable-change" {
			t.Errorf("expected bypass annotation to silence diag, got %+v", d)
		}
	}
}

func TestR27_ImmutableChange_Added(t *testing.T) {
	// Synthetic delta: single Added entry with a populated immutable field.
	// R27 is scoped to Modified only, so this must yield 0 diags.
	delta := plan.ResourceDelta{
		Added: []plan.ResourceChange{
			{
				Identity: plan.ResourceIdentity{
					APIVersion: "rds.aws.upbound.io/v1beta1",
					Kind:       "Cluster",
					Namespace:  "crossplane-system",
					Name:       "new-cluster",
				},
				HeadRaw: map[string]interface{}{
					"apiVersion": "rds.aws.upbound.io/v1beta1",
					"kind":       "Cluster",
					"metadata": map[string]interface{}{
						"name":      "new-cluster",
						"namespace": "crossplane-system",
					},
					"spec": map[string]interface{}{
						"forProvider": map[string]interface{}{
							"engine": "aurora-postgresql",
						},
					},
				},
			},
		},
	}
	diags := plan.R27ImmutableChange(delta)
	if len(diags) != 0 {
		t.Errorf("expected 0 diags on Added-only delta (out of R27 scope), got %d: %+v", len(diags), diags)
	}
}
