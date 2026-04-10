package checker

import (
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestR10_SecretTaintLeak(t *testing.T) {
	world := types.NewWorld()
	world.Compositions = append(world.Compositions, types.CompositionInfo{
		Name:             "test-comp",
		CompositeTypeRef: types.GVK{Group: "example.com", Version: "v1", Kind: "XDB"},
		Mode:             "Resources",
		Resources: []types.ComposedResource{
			{
				Name: "rds",
				Base: types.ResourceBase{APIVersion: "rds.aws.m.upbound.io/v1beta1", Kind: "Instance"},
				Patches: []types.PatchInfo{
					{
						Type:          "FromCompositeFieldPath",
						FromFieldPath: "spec.forProvider.masterPassword",
						ToFieldPath:   "spec.forProvider.region",
					},
				},
			},
		},
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	})

	diags := checkR10(world)
	xpc010 := findDiagByCode(diags, "XPC010")
	if len(xpc010) != 1 {
		t.Fatalf("expected 1 XPC010 error for secret taint leak, got %d", len(xpc010))
	}
	if xpc010[0].Severity != types.SeverityError {
		t.Errorf("expected error severity, got %s", xpc010[0].Severity)
	}
}

func TestR10_SecretToSecretSink_OK(t *testing.T) {
	world := types.NewWorld()
	world.Compositions = append(world.Compositions, types.CompositionInfo{
		Name:             "test-comp",
		CompositeTypeRef: types.GVK{Group: "example.com", Version: "v1", Kind: "XDB"},
		Mode:             "Resources",
		Resources: []types.ComposedResource{
			{
				Name: "rds",
				Base: types.ResourceBase{APIVersion: "rds.aws.m.upbound.io/v1beta1", Kind: "Instance"},
				Patches: []types.PatchInfo{
					{
						Type:          "FromCompositeFieldPath",
						FromFieldPath: "spec.forProvider.masterPassword",
						ToFieldPath:   "spec.forProvider.passwordSecretRef",
					},
				},
			},
		},
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	})

	diags := checkR10(world)
	if len(diags) > 0 {
		t.Errorf("expected no errors for secret-to-secret-sink, got %d: %v", len(diags), diags)
	}
}

func TestR10_NonSecretPatch_OK(t *testing.T) {
	world := types.NewWorld()
	world.Compositions = append(world.Compositions, types.CompositionInfo{
		Name:             "test-comp",
		CompositeTypeRef: types.GVK{Group: "example.com", Version: "v1", Kind: "XDB"},
		Mode:             "Resources",
		Resources: []types.ComposedResource{
			{
				Name: "rds",
				Base: types.ResourceBase{APIVersion: "rds.aws.m.upbound.io/v1beta1", Kind: "Instance"},
				Patches: []types.PatchInfo{
					{
						Type:          "FromCompositeFieldPath",
						FromFieldPath: "spec.parameters.region",
						ToFieldPath:   "spec.forProvider.region",
					},
				},
			},
		},
		Source: types.SourceLocation{File: "comp.yaml", Line: 1},
	})

	diags := checkR10(world)
	if len(diags) > 0 {
		t.Errorf("expected no errors for non-secret patch, got %d", len(diags))
	}
}

func TestR11_DeprecatedAPIVersion(t *testing.T) {
	world := types.NewWorld()
	world.Resources = append(world.Resources, types.ResourceInfo{
		APIVersion: "s3.aws.m.upbound.io/v1alpha1",
		Kind:       "Bucket",
		Name:       "old-bucket",
		Source:     types.SourceLocation{File: "bucket.yaml", Line: 1},
	})

	diags := checkR11(world)
	xpc011 := findDiagByCode(diags, "XPC011")
	if len(xpc011) == 0 {
		t.Fatal("expected at least 1 XPC011 warning for deprecated API version")
	}
}

func TestR11_NonDeprecatedResource_OK(t *testing.T) {
	world := types.NewWorld()
	world.Resources = append(world.Resources, types.ResourceInfo{
		APIVersion: "s3.aws.m.upbound.io/v1beta2",
		Kind:       "Bucket",
		Name:       "good-bucket",
		Source:     types.SourceLocation{File: "bucket.yaml", Line: 1},
	})

	diags := checkR11(world)
	xpc011 := findDiagByCode(diags, "XPC011")
	if len(xpc011) > 0 {
		t.Errorf("expected no XPC011 warnings for non-deprecated resource, got %d", len(xpc011))
	}
}

func TestR11_UnservedCRDVersion(t *testing.T) {
	world := types.NewWorld()
	world.CRDs = append(world.CRDs, types.CRDInfo{
		Group: "example.com",
		Kind:  "Foo",
		Versions: []types.CRDVersion{
			{Name: "v1alpha1", Served: false, Storage: false},
			{Name: "v1", Served: true, Storage: true},
		},
		Source: types.SourceLocation{File: "crd.yaml", Line: 1},
	})

	diags := checkR11(world)
	xpc011 := findDiagByCode(diags, "XPC011")
	if len(xpc011) != 1 {
		t.Fatalf("expected 1 XPC011 warning for unserved CRD version, got %d", len(xpc011))
	}
}

func TestR11_DeprecatedProviderVersion(t *testing.T) {
	world := types.NewWorld()
	world.Providers = append(world.Providers, types.ProviderInfo{
		Name:    "provider-aws",
		Package: "xpkg.crossplane.io/upbound/provider-aws:v0.35.0",
		Source:  types.SourceLocation{File: "provider.yaml", Line: 1},
	})

	diags := checkR11(world)
	xpc011 := findDiagByCode(diags, "XPC011")
	if len(xpc011) == 0 {
		t.Fatal("expected XPC011 warning for deprecated provider version")
	}
}

func TestR10R11_IntegrationWithCheck(t *testing.T) {
	world := types.NewWorld()
	world.Resources = append(world.Resources, types.ResourceInfo{
		APIVersion: "s3.aws.m.upbound.io/v1alpha1",
		Kind:       "Bucket",
		Name:       "old-bucket",
		Source:     types.SourceLocation{File: "bucket.yaml", Line: 1},
	})

	diags, err := Check(world, Config{})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}

	hasXPC011 := false
	for _, d := range diags {
		if d.Code == "XPC011" {
			hasXPC011 = true
			break
		}
	}
	if !hasXPC011 {
		t.Error("expected XPC011 warning from integrated Check()")
	}
}
