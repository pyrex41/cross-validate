# Bug: R16 (selector-needs-ignore-diff) ignores AppSet-defined `ignoreDifferences`

**xpc version:** 0.1.0
**Severity:** false positive on R16 errors for any selector resolved path that is only covered by an `ignoreDifferences` entry defined inside an `ApplicationSet.spec.template.spec.ignoreDifferences` block.

> Not a duplicate of commit `7a66c11` ("fix(r16,r21): honor managedFieldsManagers wildcard on ignoreDifferences"). That commit fixed the **Application** path — the bridge dropped `managedFieldsManagers` from `IgnoreDiffEntry`. This bug is the parallel gap on the **ApplicationSet** template path: the synthetic `ArgoApplication`s expanded from an AppSet have empty `IgnoreDifferences` to begin with, so the per-Application MFM logic never gets a chance to run.

## Summary

`spec.template.spec.ignoreDifferences` on an `ApplicationSet` is dropped at parse time. R16 ("selector-needs-ignore-diff") then sees no IgnoreDiffEntries for resources owned by the AppSet's synthetic Applications, and emits an error for every resolved selector path — even when the AppSet has explicit per-kind coverage and/or a `managedFieldsManagers: [crossplane]` wildcard that should match.

Reproduces against any real-world repo whose `ignoreDifferences` lives on the AppSet template rather than on standalone `Application` CRs (the canonical Argo CD pattern when one AppSet generates many environments).

## Reproduction

In a tree where the only `ignoreDifferences` for a Crossplane managed resource live on its owning ApplicationSet template, e.g.:

```yaml
# applicationset.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  template:
    spec:
      ignoreDifferences:
        - group: "*"
          kind: "*"
          managedFieldsManagers:
            - crossplane
        - group: "rds.aws.upbound.io"
          kind: "ClusterInstance"
          jsonPointers:
            - /spec/forProvider/monitoringRoleArn
```

```yaml
# manifests/aurora-cluster-instance.yaml
apiVersion: rds.aws.upbound.io/v1beta1
kind: ClusterInstance
metadata:
  name: aurora-preview-cluster-instance-1
spec:
  forProvider:
    monitoringRoleArnSelector:
      matchLabels:
        role: aurora-preview-cluster-monitoring
```

`xpc check` against either file alone or both files together emits:

```
XPC.E.selector-needs-ignore-diff ...:N
  rule: ClusterInstance/aurora-preview-cluster-instance-1: selector
        spec.forProvider.monitoringRoleArnSelector resolves to
        spec.forProvider.monitoringRoleArn
  problem: ... No ignoreDifferences entry covers this path.
```

…even though both the per-kind entry and the `managedFieldsManagers: [crossplane]` wildcard should cover this resolved path under the R16 coverage logic in `kernel/r16-selector-needs-ignore-diff.shen`.

The same false positive fires on the in-tree `egress-proxy-preview.yaml`'s AutoscalingGroup/LaunchTemplate selectors despite the AppSet having explicit `kind: AutoscalingGroup` and `kind: LaunchTemplate` ignoreDifferences with matching jqPathExpressions.

## Root cause

Two independent gaps that both need fixing:

**Site 1 — parser drops the field.** `pkg/ir/builder.go:1495` `parseAppSetTemplate` parses `metadata`, `project`, `source`/`sources`, `destination`, and `syncPolicy` from the AppSet template, but does not parse `template.spec.ignoreDifferences`:

```go
// pkg/ir/builder.go:1495-1529
func (b *Builder) parseAppSetTemplate(m map[string]interface{}) types.ArgoAppSetTemplate {
    tmpl := types.ArgoAppSetTemplate{}
    if meta := getMap(m, "metadata"); meta != nil { /* ... */ }
    if spec := getMap(m, "spec"); spec != nil {
        tmpl.Project, _ = spec["project"].(string)
        // source / sources / destination / syncPolicy parsed here
        // ← spec["ignoreDifferences"] never read
    }
    return tmpl
}
```

Compare with `Application` parsing at `pkg/ir/builder.go:748–755`, which does extract the field via `parseArgoIgnoreDiff`.

**Site 2 — expansion drops the field.** `pkg/ir/appset_expand.go:229` `instantiateTemplate` constructs a synthetic `ArgoApplication` from the AppSet template + one parameter set, but the returned struct (lines 277–290) does not copy `IgnoreDifferences`:

```go
return types.ArgoApplication{
    Name:         name,
    Namespace:    ns,
    Project:      project,
    TrackingMode: "annotation",
    Source:       as.Source,
    Sources:      sources,
    Destination:  /* ... */,
    SyncPolicy:   t.SyncPolicy,
    // ← IgnoreDifferences not propagated
}, true
```

So even if `parseAppSetTemplate` were fixed, the synthetic Apps that R16 iterates over (`world.ArgoApps` after `expandAppSets`) would still have empty `IgnoreDifferences`.

## Suggested fix

1. Add `IgnoreDifferences` to `types.ArgoAppSetTemplate` (mirror the field on `ArgoApplication`).
2. Extract it in `parseAppSetTemplate`:

   ```go
   if diffs := getSlice(spec, "ignoreDifferences"); diffs != nil {
       for _, d := range diffs {
           if dm, ok := d.(map[string]interface{}); ok {
               tmpl.IgnoreDifferences = append(tmpl.IgnoreDifferences, parseArgoIgnoreDiff(dm))
           }
       }
   }
   ```

3. Propagate it in `instantiateTemplate`:

   ```go
   IgnoreDifferences: t.IgnoreDifferences,
   ```

`ignoreDifferences` entries don't legally use `{{.param}}` substitution, so no per-field template walk is needed — direct copy is correct.

## Minimal repro fixture

Drop these two files in any directory and run `xpc check --skip-render <dir>`:

```yaml
# appset.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: preview-infra
  namespace: argocd
spec:
  generators:
    - list:
        elements:
          - name: preview
  template:
    metadata:
      name: '{{ .name }}-infra'
    spec:
      project: default
      source:
        repoURL: https://github.com/example/manifests
        targetRevision: HEAD
        path: '{{ .name }}'
      destination:
        server: https://kubernetes.default.svc
        namespace: crossplane-system
      ignoreDifferences:
        - group: "*"
          kind: "*"
          managedFieldsManagers:
            - crossplane
```

```yaml
# cluster-instance.yaml
apiVersion: rds.aws.upbound.io/v1beta1
kind: ClusterInstance
metadata:
  name: aurora-preview-cluster-instance-1
  namespace: crossplane-system
spec:
  forProvider:
    region: us-east-1
    monitoringRoleArnSelector:
      matchLabels:
        role: aurora-preview-cluster-monitoring
```

Observed (xpc 0.1.0 built from `claude/standalone-xpc-cli` HEAD as of 2026-05-06):

```
XPC.E.selector-needs-ignore-diff cluster-instance.yaml:1
  rule: ClusterInstance/aurora-preview-cluster-instance-1: selector
        spec.forProvider.monitoringRoleArnSelector resolves to
        spec.forProvider.monitoringRoleArn
  problem: ... No ignoreDifferences entry covers this path. Argo CD
           will fight Crossplane.
```

Expected: zero `XPC.E.selector-needs-ignore-diff` diagnostics — the AppSet's wildcard MFM entry covers every Crossplane-written field. Cross-check: the analogous fixture with `kind: Application` (in-tree at `testdata/fixtures/selector-drift-mfm-crossplane/`) emits zero R16 findings post-`7a66c11`.

(R23 `XPC.S.crossplane-state-needs-orphan` also fires on this fixture; that one is genuine and unrelated — the ClusterInstance is missing `deletionPolicy: Orphan`. Add it if you want a clean run, or just filter to R16.)

## Test seed

Land the two files above as `testdata/fixtures/selector-drift-appset-mfm-crossplane/{appset,cluster-instance}.yaml` and add a `TestR16_AppSetTemplateMFMCrossplane` mirroring `TestR16_ManagedFieldsManagersCrossplane` (`pkg/checker/check_test.go:622`). The post-fix assertion is `len(findDiagByCode(diags, "XPC.E.selector-needs-ignore-diff")) == 0`. A negative companion (drop the `ignoreDifferences` block from the AppSet template) confirms the rule still fires when coverage is genuinely absent.

## Workaround

None at the rule level. Workarounds attempted:

- Adding the entries to a standalone `Application` CR sibling file: unhelpful, since the real AppSet still owns the resources at runtime.
- Duplicating the entries onto every manifest file via Argo CD annotations: not supported by R16, no equivalent path.
- Suppressing R16 globally: per the skill, rule-level disable is intentionally absent.
- Per-finding waivers in `.xpc-waivers.yaml` work but leak a fix-by-suppression pattern that rots into a list as soon as the AppSet picks up new selector kinds.

## Real-world impact

Encountered on `fg-manifold` MR `perf/NAQ-437-aurora-perf-insights` (adds Aurora Performance Insights + Enhanced Monitoring on `aurora-preview-cluster`). The MR is correct — the new `ClusterInstance.monitoringRoleArnSelector` and `RolePolicyAttachment.roleSelector` are covered by both the existing `managedFieldsManagers: [crossplane]` wildcard and the per-kind explicit entries the contributor added — but xpc reports them as errors, blocking the validate step until the bug is understood.
