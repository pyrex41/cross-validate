package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/config"
	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestDefault_Matches_Builtin pins the safety net described in
// thoughts/shared/design/xpc-yaml-config.md §4.c: an absent xpc.yaml must
// produce behaviour bit-identical to the pre-config compile-time path. If
// this test breaks, either a default in pkg/config diverged from the kernel
// / Go literal it mirrors, or the literal moved. Either way: fix the side
// that changed unintentionally, don't paper over.
func TestDefault_Matches_Builtin(t *testing.T) {
	cfg := config.Default()

	t.Run("ProdPatterns", func(t *testing.T) {
		got := config.ResolveProdPatterns(cfg)
		want := []string{"-prod", "prod-"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ResolveProdPatterns(Default()) = %v, want %v", got, want)
		}
	})

	t.Run("AllowDeleteKeys", func(t *testing.T) {
		got := config.ResolveAllowDeleteKeys(cfg)
		want := []string{"xpc.io/allow-delete"}
		if !reflect.DeepEqual(got, want) {
			// Default is xpc.io-branded only. Org-specific aliases are
			// registered via xpc.yaml. If this fails, somebody added a
			// non-xpc.io default alias — almost certainly a mistake;
			// move it to documented xpc.yaml extension instead.
			t.Errorf("ResolveAllowDeleteKeys(Default()) = %v, want %v", got, want)
		}
	})

	t.Run("AllowImmutableChangeKeys", func(t *testing.T) {
		got := config.ResolveAllowImmutableChangeKeys(cfg)
		want := []string{"xpc.io/allow-immutable-change"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ResolveAllowImmutableChangeKeys(Default()) = %v, want %v", got, want)
		}
	})

	t.Run("CrossplaneStateNeedsOrphanCarveouts", func(t *testing.T) {
		got := config.ResolveCrossplaneStateNeedsOrphanCarveouts(cfg)
		want := []string{"alb-logs"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Resolve…Carveouts(Default()) = %v, want %v", got, want)
		}
	})

	t.Run("ImmutableFields_NoOverlay", func(t *testing.T) {
		builtins := ir.ImmutableFieldRegistry()
		got := config.ResolveImmutableFields(cfg, builtins)
		if !reflect.DeepEqual(sortedFields(got), sortedFields(builtins)) {
			t.Errorf("default config resolved registry should equal builtins (%d vs %d entries)",
				len(got), len(builtins))
		}
	})
}

// TestDefault_NilConfig_Equals_Default verifies the convenience contract:
// passing nil into the resolvers gives the same result as Default(). All
// downstream call sites (R26, R27, EnrichTrajectoryData fallback) pass nil
// when no explicit config is wired in.
func TestDefault_NilConfig_Equals_Default(t *testing.T) {
	d := config.Default()
	if !reflect.DeepEqual(config.ResolveProdPatterns(nil), config.ResolveProdPatterns(d)) {
		t.Error("ResolveProdPatterns(nil) != ResolveProdPatterns(Default())")
	}
	if !reflect.DeepEqual(config.ResolveAllowDeleteKeys(nil), config.ResolveAllowDeleteKeys(d)) {
		t.Error("ResolveAllowDeleteKeys(nil) != ResolveAllowDeleteKeys(Default())")
	}
	if !reflect.DeepEqual(config.ResolveAllowImmutableChangeKeys(nil), config.ResolveAllowImmutableChangeKeys(d)) {
		t.Error("ResolveAllowImmutableChangeKeys(nil) != ResolveAllowImmutableChangeKeys(Default())")
	}
	if !reflect.DeepEqual(
		config.ResolveCrossplaneStateNeedsOrphanCarveouts(nil),
		config.ResolveCrossplaneStateNeedsOrphanCarveouts(d)) {
		t.Error("Resolve…Carveouts(nil) != Resolve…Carveouts(Default())")
	}
}

func TestParse_Empty(t *testing.T) {
	cfg, err := config.Parse([]byte(""))
	if err != nil {
		t.Fatalf("empty bytes: %v", err)
	}
	if !reflect.DeepEqual(config.ResolveProdPatterns(cfg), config.ResolveProdPatterns(nil)) {
		t.Errorf("empty file should be equivalent to default")
	}
}

func TestParse_VersionMismatch(t *testing.T) {
	_, err := config.Parse([]byte("version: 99\n"))
	if err == nil {
		t.Fatal("expected error on version=99")
	}
	if !strings.Contains(err.Error(), "unsupported xpc.yaml version") {
		t.Errorf("expected version-mismatch error, got %v", err)
	}
}

func TestParse_ProdPatterns_Replace(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version: 1
prod-patterns:
  appset-name-substrings:
    - "-production-"
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := config.ResolveProdPatterns(cfg)
	want := []string{"-production-"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected replace-semantics with single user pattern, got %v", got)
	}
}

func TestParse_BypassAnnotations_Override(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version: 1
bypass-annotations:
  allow-delete:
    primary: "mycorp.example.com/allow-delete"
    aliases:
      - "legacy/allow-delete"
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := config.ResolveAllowDeleteKeys(cfg)
	if got[0] != "mycorp.example.com/allow-delete" {
		t.Errorf("expected user-supplied primary first, got %v", got)
	}
	if !contains(got, "legacy/allow-delete") {
		t.Errorf("expected user alias retained, got %v", got)
	}
}

func TestParse_NameCarveouts_Additive(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version: 1
name-carveouts:
  crossplane-state-needs-orphan:
    - "temp-"
    - "alb-logs"   # overlap with built-in is deduped
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := config.ResolveCrossplaneStateNeedsOrphanCarveouts(cfg)
	if !contains(got, "alb-logs") || !contains(got, "temp-") {
		t.Errorf("expected built-in + user carve-outs both present, got %v", got)
	}
	// No duplicates.
	if countOf(got, "alb-logs") != 1 {
		t.Errorf("expected dedup, got %v", got)
	}
}

func TestParse_ImmutableFields_AppendAndSuppress(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version: 1
immutable-fields:
  - gvk: apps/v1/StatefulSet
    paths: [spec.serviceName]
    suppress: true
  - gvk: mycorp.example.com/v1alpha1/Widget
    paths: [spec.forProvider.widgetId]
    reason: "Widget ID is the external identity"
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	builtins := []types.ImmutableField{
		{Group: "apps", Kind: "StatefulSet", FieldPath: "spec.serviceName", Reason: "builtin"},
		{Group: "apps", Kind: "StatefulSet", FieldPath: "spec.volumeClaimTemplates", Reason: "builtin"},
	}
	got := config.ResolveImmutableFields(cfg, builtins)

	for _, f := range got {
		if f.Group == "apps" && f.Kind == "StatefulSet" && f.FieldPath == "spec.serviceName" {
			t.Errorf("expected suppress to remove (apps, StatefulSet, spec.serviceName), got %+v", f)
		}
	}
	found := false
	for _, f := range got {
		if f.Group == "mycorp.example.com" && f.Kind == "Widget" && f.FieldPath == "spec.forProvider.widgetId" {
			found = true
			if f.Reason != "Widget ID is the external identity" {
				t.Errorf("expected user reason preserved, got %q", f.Reason)
			}
		}
	}
	if !found {
		t.Errorf("expected user-appended Widget entry, got %+v", got)
	}
}

func TestParse_ImmutableFields_RejectsMissingPaths(t *testing.T) {
	_, err := config.Parse([]byte(`
version: 1
immutable-fields:
  - gvk: apps/v1/StatefulSet
`))
	if err == nil {
		t.Fatal("expected error on entry with no paths and no suppress")
	}
}

func TestParse_ImmutableFields_RejectsSuppressWithoutPaths(t *testing.T) {
	_, err := config.Parse([]byte(`
version: 1
immutable-fields:
  - gvk: apps/v1/StatefulSet
    suppress: true
`))
	if err == nil {
		t.Fatal("expected error on suppress entry with no paths")
	}
}

func TestParse_ImmutableFields_RejectsAmbiguousTwoSegmentGVK(t *testing.T) {
	_, err := config.Parse([]byte(`
version: 1
immutable-fields:
  - gvk: apps/StatefulSet
    paths: [spec.serviceName]
`))
	if err == nil {
		t.Fatal("expected error on ambiguous two-segment grouped GVK")
	}

	cfg, err := config.Parse([]byte(`
version: 1
immutable-fields:
  - gvk: v1/ConfigMap
    paths: [metadata.name]
`))
	if err != nil {
		t.Fatalf("expected core API two-segment GVK to remain accepted: %v", err)
	}
	got := config.ResolveImmutableFields(cfg, nil)
	if len(got) != 1 || got[0].Group != "" || got[0].Kind != "ConfigMap" {
		t.Fatalf("unexpected core GVK resolution: %+v", got)
	}
}

func TestParse_UnknownNestedKey_IsError(t *testing.T) {
	// Strict-decode rejects unknown keys inside known sections so users
	// can't silently mistype a knob name.
	_, err := config.Parse([]byte(`
version: 1
prod-patterns:
  totally-bogus: ["foo"]
`))
	if err == nil {
		t.Fatal("expected error on unknown nested key")
	}
}

func TestDiscover_NotFound(t *testing.T) {
	tmp := t.TempDir()
	p, ok, _, err := config.Discover(tmp)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if ok || p != "" {
		t.Errorf("expected no discovery in empty dir, got (%q, %v)", p, ok)
	}
}

func TestDiscover_FoundUpward(t *testing.T) {
	tmp := t.TempDir()
	deep := filepath.Join(tmp, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(tmp, "xpc.yaml")
	if err := os.WriteFile(cfgFile, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, ok, viaExe, err := config.Discover(deep)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !ok {
		t.Fatal("expected to find xpc.yaml upward")
	}
	if viaExe {
		t.Error("upward find shouldn't be marked as exe-dir fallback")
	}
	gotAbs, _ := filepath.Abs(p)
	wantAbs, _ := filepath.Abs(cfgFile)
	if gotAbs != wantAbs {
		t.Errorf("discovered %q, want %q", gotAbs, wantAbs)
	}
}

// helpers

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func countOf(haystack []string, needle string) int {
	n := 0
	for _, s := range haystack {
		if s == needle {
			n++
		}
	}
	return n
}

func sortedFields(in []types.ImmutableField) []types.ImmutableField {
	out := append([]types.ImmutableField(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].FieldPath < out[j].FieldPath
	})
	return out
}
