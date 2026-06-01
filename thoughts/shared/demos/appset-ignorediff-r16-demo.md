# AppSet ignoreDifferences propagation — R16/R21 false positives fixed

*2026-05-11T16:14:24Z by Showboat 0.6.1*
<!-- showboat-id: bafdee97-9606-4b0e-9e60-fdd968ca4b31 -->

Two recent fixes both targeted the R16 (`selector-needs-ignore-diff`) and R21 (`destructive-mutation-without-ignore-diff`) rules. Commit `7a66c11` fixed the **Application** path — the bridge dropped `managedFieldsManagers` from `IgnoreDiffEntry`. This demo covers the parallel gap on the **ApplicationSet** template path: synthetic Applications expanded from an AppSet had empty `IgnoreDifferences` to begin with, so the per-Application MFM logic never ran. Commit `793e12a` closes that. Two sites needed fixing — parse and propagate.

## The fixture

A minimal AppSet generates one synthetic Application. Its template's `ignoreDifferences` uses a Crossplane MFM wildcard, plus a per-kind `ClusterInstance` entry. The Crossplane manifest under the AppSet has a selector that resolves to a path covered by both entries.

```bash
ls testdata/fixtures/selector-drift-appset-mfm-crossplane/
```

```output
appset.yaml
cluster-instance.yaml
```

```bash
cat testdata/fixtures/selector-drift-appset-mfm-crossplane/appset.yaml
```

```output
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

```bash
cat testdata/fixtures/selector-drift-appset-mfm-crossplane/cluster-instance.yaml
```

```output
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

The `monitoringRoleArnSelector` resolves to `spec.forProvider.monitoringRoleArn`. Crossplane writes that field, so the MFM wildcard `{group:*, kind:*, managedFieldsManagers:[crossplane]}` should cover it and R16 should stay silent. Before the fix, R16 fired anyway because synthetic Apps from the AppSet had empty `IgnoreDifferences`.

## The two-site fix

**Site 1 — parser.** `parseAppSetTemplate` in `pkg/ir/builder.go` parsed `metadata`, `project`, `source`/`sources`, `destination`, and `syncPolicy`, but never read `template.spec.ignoreDifferences`. The field landed in the AST as zero.

**Site 2 — expansion.** `instantiateTemplate` in `pkg/ir/appset_expand.go` built a synthetic `ArgoApplication` per generator parameter set, but the returned struct didn't copy `IgnoreDifferences` — so even if site 1 were fixed, the synthetic Apps would still have empty diff suppressions.

`ignoreDifferences` entries don't legally use `{{.param}}` substitution, so a direct copy on expansion is correct — no per-field template walk needed.

```bash
git show 793e12a --stat --no-color | head -20
```

```output
commit 793e12a68f1bb19dd45bafb3a9a2ff36d38abd68
Author: Reuben Brooks <reuben.brooks@facilitygrid.com>
Date:   Wed May 6 10:58:55 2026 -0500

    fix(r16,r21): propagate AppSet template ignoreDifferences to synthetic Apps
    
    ApplicationSet `spec.template.spec.ignoreDifferences` was silently dropped
    at parse time and again at expansion time. The synthetic ArgoApplications
    that R16/R21 iterate over carried empty IgnoreDifferences, so the
    managedFieldsManagers wildcard pattern (group: "*", kind: "*",
    managedFieldsManagers: [crossplane]) — the canonical Crossplane-on-Argo
    shape when one AppSet generates many environments — never reached the
    kernel's coverage check. Result: false positives on every selector
    resolved-path and every late-init field on AppSet-managed Crossplane
    resources, even when the AppSet declared correct coverage.
    
    Three sites:
      - pkg/types/types.go: add IgnoreDifferences to ArgoAppSetTemplate.
      - pkg/ir/builder.go: extract spec.ignoreDifferences in
        parseAppSetTemplate via parseArgoIgnoreDiff (mirrors the Application
```

```bash
git show 793e12a -- pkg/types/types.go pkg/ir/builder.go pkg/ir/appset_expand.go --no-color | grep -E '^(\+\+\+|---|\+[^+]|@@)' | head -30
```

```output
--- a/pkg/ir/appset_expand.go
+++ b/pkg/ir/appset_expand.go
@@ -286,7 +286,8 @@ func instantiateTemplate(as types.ArgoApplicationSet, params map[string]string)
+		SyncPolicy:        t.SyncPolicy,
+		IgnoreDifferences: t.IgnoreDifferences,
--- a/pkg/ir/builder.go
+++ b/pkg/ir/builder.go
@@ -1523,6 +1523,13 @@ func (b *Builder) parseAppSetTemplate(m map[string]interface{}) types.ArgoAppSet
+		if diffs := getSlice(spec, "ignoreDifferences"); diffs != nil {
+			for _, d := range diffs {
+				if dm, ok := d.(map[string]interface{}); ok {
+					tmpl.IgnoreDifferences = append(tmpl.IgnoreDifferences, parseArgoIgnoreDiff(dm))
+				}
+			}
+		}
--- a/pkg/types/types.go
+++ b/pkg/types/types.go
@@ -630,6 +630,12 @@ type ArgoAppSetTemplate struct {
+	// IgnoreDifferences from spec.template.spec.ignoreDifferences. Argo CD's
+	// ApplicationSet controller copies these onto each generated Application
+	// verbatim; the entries' fields (group/kind/jsonPointers/jqPathExpressions/
+	// managedFieldsManagers) are not legally template-substituted, so a direct
+	// copy through instantiateTemplate is correct.
+	IgnoreDifferences []ArgoIgnoreDiff `json:"ignoreDifferences,omitempty"`
```

## Run the lint

`xpc check` against the fixture should emit zero `XPC.E.selector-needs-ignore-diff` diagnostics. (R23 `crossplane-state-needs-orphan` is a genuine, unrelated finding — the ClusterInstance lacks `deletionPolicy: Orphan`. We filter for R16 below.)

```bash
./xpc check --skip-render testdata/fixtures/selector-drift-appset-mfm-crossplane/ 2>&1 | grep -E '(selector-needs-ignore-diff|^[0-9]+ errors|^Summary)' || echo 'no R16 findings'
```

```output
no R16 findings
```

```bash
./xpc check --skip-render testdata/fixtures/selector-drift-appset-mfm-crossplane/ 2>&1 | tail -8
```

```output
XPC.S.crossplane-state-needs-orphan testdata/fixtures/selector-drift-appset-mfm-crossplane/cluster-instance.yaml:1
  rule:     ClusterInstance/aurora-preview-cluster-instance-1 is a state-bearing Crossplane managed resource without deletionPolicy: Orphan
  severity: error
  problem:  spec.deletionPolicy is absent (Crossplane default is Delete). Group rds.aws.upbound.io, Kind ClusterInstance is in the state-bearing allowlist (Aurora, DocDB, MySQL, KMS, S3, VPC). Default Crossplane deletion will run a real destructive call against the external object. This is the INC-6 failure mode.
  fix:      Set spec.deletionPolicy: Orphan on this resource. If destruction is genuinely intended (e.g. throwaway test), add annotation xpc.io/allow-delete=true OR policy.facilitygrid.io/allow-delete=true to bypass.
  docs:     https://xpc.dev/errors/XPC.S.crossplane-state-needs-orphan

xpc: 1 error(s), 0 warning(s)
```

```bash
go test ./pkg/checker/ -run TestR16_AppSetTemplateMFMCrossplane -count=1 2>&1 | sed -E 's/[0-9]+\.[0-9]+s/<time>/g'
```

```output
ok  	github.com/pyrex41/cross-validate-/pkg/checker	<time>
```

## Negative companion — does the rule still fire when coverage is genuinely missing?

Strip the `ignoreDifferences` block from the AppSet template and re-check. R16 should fire on the same selector path.

```bash
mkdir -p /tmp/r16-neg && sed '/ignoreDifferences:/,$d' testdata/fixtures/selector-drift-appset-mfm-crossplane/appset.yaml > /tmp/r16-neg/appset.yaml && cp testdata/fixtures/selector-drift-appset-mfm-crossplane/cluster-instance.yaml /tmp/r16-neg/ && ./xpc check --skip-render /tmp/r16-neg/ 2>&1 | grep -E 'selector-needs-ignore-diff|errors,' | head -5
```

```output
XPC.E.selector-needs-ignore-diff /tmp/r16-neg/cluster-instance.yaml:1
  docs:     https://xpc.dev/errors/XPC.E.selector-needs-ignore-diff
```

Good — R16 fires when coverage is genuinely absent. The fix didn't silently disable the rule, it just stopped lying about AppSet template coverage.

## Verify the demo

`showboat verify` re-runs every code block and diffs against recorded output. Useful before sharing — confirms the demo still reproduces against the current binary and fixture.
