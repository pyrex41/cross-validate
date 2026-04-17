// Package deprecation implements Category L (deprecation/calendar) obligation generators.
package deprecation

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

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

// APIDeprecationCalendar is a Category L generator that checks resources,
// Compositions, Providers, and CRDs for deprecated API versions and
// known-deprecated provider versions.
//
// Absorbs legacy rule R11 (XPC011).
type APIDeprecationCalendar struct{}

var _ obligation.Generator = APIDeprecationCalendar{}

func (APIDeprecationCalendar) Name() string                  { return "api-deprecation-calendar" }
func (APIDeprecationCalendar) Category() obligation.Category { return obligation.CatDeprecation }

func (APIDeprecationCalendar) Description() string {
	return `Resources and Compositions must not use deprecated API versions.

Checks for deprecated API versions on resources, deprecated compositeTypeRef
versions on Compositions, deprecated provider package versions, and CRD
versions that are no longer served. Severity is Warning by default, escalating
to Error if the deprecation deadline is within 30 days.

Legacy code: XPC011`
}

// Generate emits obligations for all deprecation concerns across the World.
func (APIDeprecationCalendar) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	var obs []obligation.Obligation

	// Check resources using deprecated API versions
	for _, res := range w.Resources {
		dep, ok := knownDeprecations[res.APIVersion]
		if ok && dep.deprecated {
			res := res // capture for closure
			dep := dep
			instance := sanitizeInstance("res." + res.Kind + "." + res.Name)

			obs = append(obs, obligation.Obligation{
				ID:       obligation.MakeID(obligation.CatDeprecation, "api-deprecation-calendar", instance),
				Category: obligation.CatDeprecation,
				Subject:  res.Source,
				Claim: fmt.Sprintf("Resource %s/%s does not use a deprecated API version %s",
					res.Kind, res.Name, res.APIVersion),
				Provenance: obligation.Provenance{
					Generator: "api-deprecation-calendar",
					Category:  obligation.CatDeprecation,
					InputHash: obligation.ContentHash(res.APIVersion + res.Kind + res.Name),
				},
				LegacyCode: "XPC011",
				Discharge: func(ctx *obligation.Context) obligation.Result {
					sev := types.SeverityWarning
					if !dep.deadline.IsZero() && time.Until(dep.deadline) < time.Duration(deprecationWarningDays)*24*time.Hour {
						sev = types.SeverityError
					}
					return obligation.Result{
						Status: obligation.Violated,
						Diag: &types.Diagnostic{
							Code:     "XPC011",
							Severity: sev,
							Source:   res.Source,
							Message: fmt.Sprintf("deprecated API version %s for %s/%s",
								res.APIVersion, res.Kind, res.Name),
							Detail: dep.message,
							Fix:    "Update to the recommended API version.",
						},
					}
				},
			})
		}
	}

	// Check compositions using deprecated API versions in compositeTypeRef
	for _, comp := range w.Compositions {
		apiVer := comp.CompositeTypeRef.Group + "/" + comp.CompositeTypeRef.Version
		dep, ok := knownDeprecations[apiVer]
		if ok && dep.deprecated {
			comp := comp // capture for closure
			dep := dep
			apiVer := apiVer
			instance := sanitizeInstance("comp." + comp.Name)

			obs = append(obs, obligation.Obligation{
				ID:       obligation.MakeID(obligation.CatDeprecation, "api-deprecation-calendar", instance),
				Category: obligation.CatDeprecation,
				Subject:  comp.Source,
				Claim: fmt.Sprintf("Composition %s compositeTypeRef does not use deprecated API version %s",
					comp.Name, apiVer),
				Provenance: obligation.Provenance{
					Generator: "api-deprecation-calendar",
					Category:  obligation.CatDeprecation,
					InputHash: obligation.ContentHash(comp.Name + apiVer),
				},
				LegacyCode: "XPC011",
				Discharge: func(ctx *obligation.Context) obligation.Result {
					return obligation.Result{
						Status: obligation.Violated,
						Diag: &types.Diagnostic{
							Code:     "XPC011",
							Severity: types.SeverityWarning,
							Source:   comp.Source,
							Message: fmt.Sprintf("Composition %s references deprecated API version %s",
								comp.Name, apiVer),
							Detail: dep.message,
							Fix:    "Update the compositeTypeRef to use a supported version.",
						},
					}
				},
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
				prov := prov // capture for closure
				dep := dep
				provVersion := provVersion
				instance := sanitizeInstance("prov." + prov.Name + "." + provVersion)

				obs = append(obs, obligation.Obligation{
					ID:       obligation.MakeID(obligation.CatDeprecation, "api-deprecation-calendar", instance),
					Category: obligation.CatDeprecation,
					Subject:  prov.Source,
					Claim: fmt.Sprintf("Provider %s at %s is not a deprecated version",
						prov.Name, provVersion),
					Provenance: obligation.Provenance{
						Generator: "api-deprecation-calendar",
						Category:  obligation.CatDeprecation,
						InputHash: obligation.ContentHash(prov.Name + provVersion),
					},
					LegacyCode: "XPC011",
					Discharge: func(ctx *obligation.Context) obligation.Result {
						return obligation.Result{
							Status: obligation.Violated,
							Diag: &types.Diagnostic{
								Code:     "XPC011",
								Severity: types.SeverityWarning,
								Source:   prov.Source,
								Message: fmt.Sprintf("Provider %s at %s may have known issues",
									prov.Name, provVersion),
								Detail: dep.message,
								Fix:    fmt.Sprintf("Upgrade to version %s or later.", dep.versionBefore),
							},
						}
					},
				})
			}
		}
	}

	// Check CRDs for versions marked as not served (removal candidate)
	for _, crd := range w.CRDs {
		for _, v := range crd.Versions {
			if !v.Served {
				crd := crd // capture for closure
				v := v
				instance := sanitizeInstance("crd." + crd.Group + "." + crd.Kind + "." + v.Name)

				obs = append(obs, obligation.Obligation{
					ID:       obligation.MakeID(obligation.CatDeprecation, "api-deprecation-calendar", instance),
					Category: obligation.CatDeprecation,
					Subject:  crd.Source,
					Claim: fmt.Sprintf("CRD %s.%s version %s is still served",
						crd.Group, crd.Kind, v.Name),
					Provenance: obligation.Provenance{
						Generator: "api-deprecation-calendar",
						Category:  obligation.CatDeprecation,
						InputHash: obligation.ContentHash(crd.Group + crd.Kind + v.Name),
					},
					LegacyCode: "XPC011",
					Discharge: func(ctx *obligation.Context) obligation.Result {
						return obligation.Result{
							Status: obligation.Violated,
							Diag: &types.Diagnostic{
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
							},
						}
					},
				})
			}
		}
	}

	return obs
}

// sanitizeInstance cleans a resource name for use in an obligation ID.
func sanitizeInstance(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" {
		return "unnamed"
	}
	return name
}
