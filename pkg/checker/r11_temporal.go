package checker

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// R11: temporal validity
// Every type judgment carries an implicit "valid until" derived from upstream
// metadata: CRD deprecation timelines, provider package versions,
// function deprecation notices.

// deprecationWarningDays is how far in advance to warn about expiring features.
const deprecationWarningDays = 30

// knownDeprecations maps apiVersion patterns to deprecation info.
var knownDeprecations = map[string]deprecationInfo{
	// Crossplane v1 API deprecations
	"apiextensions.crossplane.io/v1alpha1": {
		message:    "apiextensions.crossplane.io/v1alpha1 is deprecated, use v1 or v2",
		deprecated: true,
	},
	"pkg.crossplane.io/v1alpha1": {
		message:    "pkg.crossplane.io/v1alpha1 is deprecated, use v1 or v1beta1",
		deprecated: true,
	},

	// Known provider version deprecations
	"s3.aws.m.upbound.io/v1alpha1": {
		message:    "v1alpha1 is deprecated for s3.aws.m.upbound.io, use v1beta1 or v1beta2",
		deprecated: true,
	},
	"ec2.aws.m.upbound.io/v1alpha1": {
		message:    "v1alpha1 is deprecated for ec2.aws.m.upbound.io, use v1beta1",
		deprecated: true,
	},
	"rds.aws.m.upbound.io/v1alpha1": {
		message:    "v1alpha1 is deprecated for rds.aws.m.upbound.io, use v1beta1",
		deprecated: true,
	},
}

// providerVersionDeprecations tracks provider package versions known to have issues.
var providerVersionDeprecations = []providerDeprecation{
	{
		packagePattern: "xpkg.crossplane.io/upbound/provider-aws",
		versionBefore:  "v0.40.0",
		message:        "provider-aws versions before v0.40.0 have known conversion webhook issues",
	},
	{
		packagePattern: "xpkg.crossplane.io/upbound/provider-family-aws",
		versionBefore:  "v1.0.0",
		message:        "provider-family-aws versions before v1.0.0 are pre-GA and may have breaking changes",
	},
}

type deprecationInfo struct {
	message    string
	deprecated bool
	deadline   time.Time // zero means already deprecated
}

type providerDeprecation struct {
	packagePattern string
	versionBefore  string
	message        string
}

var versionRegex = regexp.MustCompile(`v(\d+)\.(\d+)\.(\d+)`)

func checkR11(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	// Check resources using deprecated API versions
	for _, res := range w.Resources {
		dep, ok := knownDeprecations[res.APIVersion]
		if ok && dep.deprecated {
			sev := types.SeverityWarning
			if !dep.deadline.IsZero() && time.Until(dep.deadline) < time.Duration(deprecationWarningDays)*24*time.Hour {
				sev = types.SeverityError
			}

			diags = append(diags, types.Diagnostic{
				Code:     "XPC011",
				Severity: sev,
				Source:   res.Source,
				Message: fmt.Sprintf("deprecated API version %s for %s/%s",
					res.APIVersion, res.Kind, res.Name),
				Detail: dep.message,
				Fix:    "Update to the recommended API version.",
			})
		}
	}

	// Check compositions using deprecated API versions in compositeTypeRef
	for _, comp := range w.Compositions {
		apiVer := comp.CompositeTypeRef.Group + "/" + comp.CompositeTypeRef.Version
		dep, ok := knownDeprecations[apiVer]
		if ok && dep.deprecated {
			diags = append(diags, types.Diagnostic{
				Code:     "XPC011",
				Severity: types.SeverityWarning,
				Source:   comp.Source,
				Message: fmt.Sprintf("Composition %s references deprecated API version %s",
					comp.Name, apiVer),
				Detail: dep.message,
				Fix:    "Update the compositeTypeRef to use a supported version.",
			})
		}
	}

	// Check providers for known deprecated versions
	for _, prov := range w.Providers {
		for _, dep := range providerVersionDeprecations {
			if !strings.Contains(prov.Package, dep.packagePattern) {
				continue
			}

			provVersion := extractVersion(prov.Package)
			if provVersion != "" && isVersionBefore(provVersion, dep.versionBefore) {
				diags = append(diags, types.Diagnostic{
					Code:     "XPC011",
					Severity: types.SeverityWarning,
					Source:   prov.Source,
					Message: fmt.Sprintf("Provider %s at %s may have known issues",
						prov.Name, provVersion),
					Detail: dep.message,
					Fix:    fmt.Sprintf("Upgrade to version %s or later.", dep.versionBefore),
				})
			}
		}
	}

	// Check CRDs for versions marked as not served (removal candidate)
	for _, crd := range w.CRDs {
		for _, v := range crd.Versions {
			if !v.Served {
				diags = append(diags, types.Diagnostic{
					Code:     "XPC011",
					Severity: types.SeverityWarning,
					Source:   crd.Source,
					Message: fmt.Sprintf("CRD %s.%s version %s is no longer served",
						crd.Group, crd.Kind, v.Name),
					Detail: fmt.Sprintf("Version %s of CRD %s.%s is not served. "+
						"Any resources still using this version will fail on next API server restart. "+
						"This version will likely be removed in a future release.",
						v.Name, crd.Group, crd.Kind),
					Fix: fmt.Sprintf("Migrate resources from %s to a served version.", v.Name),
				})
			}
		}
	}

	return diags
}

func extractVersion(pkg string) string {
	// Extract version from package string like "xpkg.../foo:v1.2.3"
	idx := strings.LastIndex(pkg, ":")
	if idx < 0 {
		return ""
	}
	return pkg[idx+1:]
}

func isVersionBefore(a, b string) bool {
	aMatch := versionRegex.FindStringSubmatch(a)
	bMatch := versionRegex.FindStringSubmatch(b)
	if len(aMatch) != 4 || len(bMatch) != 4 {
		return false
	}

	for i := 1; i <= 3; i++ {
		aNum := mustAtoi(aMatch[i])
		bNum := mustAtoi(bMatch[i])
		if aNum < bNum {
			return true
		}
		if aNum > bNum {
			return false
		}
	}
	return false
}

func mustAtoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
