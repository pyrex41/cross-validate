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
	if !strings.Contains(err.Error(), "suppress entries must list the specific field paths") {
		t.Fatalf("expected suppress migration hint, got %v", err)
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

	_, err = config.Parse([]byte(`
version: 1
immutable-fields:
  - gvk: v2ray/Widget
    paths: [spec.id]
`))
	if err == nil {
		t.Fatal("expected group names starting with v+digit to remain ambiguous")
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

	cfg, err = config.Parse([]byte(`
version: 1
immutable-fields:
  - gvk: v1beta1/ConfigMap
    paths: [metadata.name]
`))
	if err != nil {
		t.Fatalf("expected core API prerelease version GVK to remain accepted: %v", err)
	}
}

func TestParse_StateBearingKinds_Append(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version: 1
state-bearing-kinds:
  append:
    - {group: "myorg.example.com", kind: "ManagedThing"}
    - {group: "myorg.example.com", kind: "Vault"}
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := len(cfg.StateBearingKinds.Append); got != 2 {
		t.Fatalf("expected 2 append entries, got %d", got)
	}
	if cfg.StateBearingKinds.Append[0].Group != "myorg.example.com" ||
		cfg.StateBearingKinds.Append[0].Kind != "ManagedThing" {
		t.Errorf("first append entry mis-parsed: %+v", cfg.StateBearingKinds.Append[0])
	}
	defaults := []types.ArgoGroupKind{
		{Group: "rds.aws.upbound.io", Kind: "Cluster"},
	}
	got := config.ResolveStateBearingKinds(cfg, defaults)
	wantContains := func(group, kind string) {
		t.Helper()
		for _, gk := range got {
			if gk.Group == group && gk.Kind == kind {
				return
			}
		}
		t.Errorf("expected resolved list to contain (%s, %s); got %+v", group, kind, got)
	}
	wantContains("rds.aws.upbound.io", "Cluster")
	wantContains("myorg.example.com", "ManagedThing")
	wantContains("myorg.example.com", "Vault")
}

func TestParse_StateBearingKinds_Suppress(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version: 1
state-bearing-kinds:
  suppress:
    - {group: "kms.aws.upbound.io", kind: "Key"}
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := len(cfg.StateBearingKinds.Suppress); got != 1 {
		t.Fatalf("expected 1 suppress entry, got %d", got)
	}
	defaults := []types.ArgoGroupKind{
		{Group: "kms.aws.upbound.io", Kind: "Key"},
		{Group: "s3.aws.upbound.io", Kind: "Bucket"},
	}
	got := config.ResolveStateBearingKinds(cfg, defaults)
	for _, gk := range got {
		if gk.Group == "kms.aws.upbound.io" && gk.Kind == "Key" {
			t.Errorf("expected suppress to remove (kms.aws.upbound.io, Key), got %+v", got)
		}
	}
	// Other defaults survive.
	saw := false
	for _, gk := range got {
		if gk.Group == "s3.aws.upbound.io" && gk.Kind == "Bucket" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected non-suppressed default to survive, got %+v", got)
	}
}

func TestParse_StateBearingKinds_RejectsEmptyKind(t *testing.T) {
	_, err := config.Parse([]byte(`
version: 1
state-bearing-kinds:
  append:
    - {group: "myorg.example.com", kind: ""}
`))
	if err == nil {
		t.Fatal("expected error on empty kind in append")
	}
	if !strings.Contains(err.Error(), "kind is required") {
		t.Errorf("expected 'kind is required' error, got %v", err)
	}

	_, err = config.Parse([]byte(`
version: 1
state-bearing-kinds:
  suppress:
    - {group: "kms.aws.upbound.io", kind: "   "}
`))
	if err == nil {
		t.Fatal("expected error on whitespace-only kind in suppress")
	}
	if !strings.Contains(err.Error(), "kind must be non-blank") {
		t.Errorf("expected 'kind must be non-blank' error, got %v", err)
	}

	// Whitespace-only group is rejected even if kind is fine — empty
	// group is allowed (core APIs), but blank-with-spaces is a typo.
	_, err = config.Parse([]byte(`
version: 1
state-bearing-kinds:
  append:
    - {group: "  ", kind: "Widget"}
`))
	if err == nil {
		t.Fatal("expected error on whitespace-only group")
	}
}

func TestResolve_StateBearingKinds(t *testing.T) {
	defaults := []types.ArgoGroupKind{
		{Group: "rds.aws.upbound.io", Kind: "Cluster"},
		{Group: "kms.aws.upbound.io", Kind: "Key"},
		{Group: "s3.aws.upbound.io", Kind: "Bucket"},
	}

	t.Run("nil_cfg_returns_defaults", func(t *testing.T) {
		got := config.ResolveStateBearingKinds(nil, defaults)
		if !reflect.DeepEqual(sortedGK(got), sortedGK(defaults)) {
			t.Errorf("nil cfg should return defaults verbatim, got %+v", got)
		}
	})

	t.Run("suppress_of_nonexistent_is_noop", func(t *testing.T) {
		cfg := &config.Config{
			StateBearingKinds: config.StateBearingKindsConfig{
				Suppress: []config.StateBearingKindEntry{
					{Group: "nonexistent.example.com", Kind: "Whatever"},
				},
			},
		}
		got := config.ResolveStateBearingKinds(cfg, defaults)
		if !reflect.DeepEqual(sortedGK(got), sortedGK(defaults)) {
			t.Errorf("suppress of nonexistent should be a no-op, got %+v", got)
		}
	})

	t.Run("append_of_existing_is_no_dupe", func(t *testing.T) {
		cfg := &config.Config{
			StateBearingKinds: config.StateBearingKindsConfig{
				Append: []config.StateBearingKindEntry{
					{Group: "rds.aws.upbound.io", Kind: "Cluster"}, // dup
				},
			},
		}
		got := config.ResolveStateBearingKinds(cfg, defaults)
		if len(got) != len(defaults) {
			t.Errorf("append of existing should not duplicate, got %+v", got)
		}
	})

	t.Run("suppress_then_append_back", func(t *testing.T) {
		cfg := &config.Config{
			StateBearingKinds: config.StateBearingKindsConfig{
				Suppress: []config.StateBearingKindEntry{
					{Group: "kms.aws.upbound.io", Kind: "Key"},
				},
				Append: []config.StateBearingKindEntry{
					{Group: "kms.aws.upbound.io", Kind: "Key"},
				},
			},
		}
		// Suppress applies first; append adds it back. Net: still present.
		got := config.ResolveStateBearingKinds(cfg, defaults)
		saw := false
		for _, gk := range got {
			if gk.Group == "kms.aws.upbound.io" && gk.Kind == "Key" {
				saw = true
			}
		}
		if !saw {
			t.Errorf("suppress-then-append-back should leave entry present, got %+v", got)
		}
	})

	t.Run("suppress_default_plus_append_new", func(t *testing.T) {
		cfg := &config.Config{
			StateBearingKinds: config.StateBearingKindsConfig{
				Suppress: []config.StateBearingKindEntry{
					{Group: "kms.aws.upbound.io", Kind: "Key"},
				},
				Append: []config.StateBearingKindEntry{
					{Group: "myorg.example.com", Kind: "ManagedThing"},
				},
			},
		}
		got := config.ResolveStateBearingKinds(cfg, defaults)
		for _, gk := range got {
			if gk.Group == "kms.aws.upbound.io" && gk.Kind == "Key" {
				t.Errorf("expected KMS Key suppressed, got %+v", got)
			}
		}
		saw := false
		for _, gk := range got {
			if gk.Group == "myorg.example.com" && gk.Kind == "ManagedThing" {
				saw = true
			}
		}
		if !saw {
			t.Errorf("expected appended ManagedThing, got %+v", got)
		}
		// Other defaults survive.
		if len(got) != len(defaults)-1+1 {
			t.Errorf("expected len defaults-1+1, got %d (%+v)", len(got), got)
		}
	})

	t.Run("dedup_within_append", func(t *testing.T) {
		cfg := &config.Config{
			StateBearingKinds: config.StateBearingKindsConfig{
				Append: []config.StateBearingKindEntry{
					{Group: "myorg.example.com", Kind: "Widget"},
					{Group: "myorg.example.com", Kind: "Widget"},
				},
			},
		}
		got := config.ResolveStateBearingKinds(cfg, defaults)
		count := 0
		for _, gk := range got {
			if gk.Group == "myorg.example.com" && gk.Kind == "Widget" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected dedup within append, got %d copies in %+v", count, got)
		}
	})
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

func sortedGK(in []types.ArgoGroupKind) []types.ArgoGroupKind {
	out := append([]types.ArgoGroupKind(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Kind < out[j].Kind
	})
	return out
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
