---
session: post-S5 Track 2 (R21)
rule: XPC.E.late-init-needs-ignore-diff
branch: claude/build-xpc-type-checker-TfgsT
tip: (current HEAD — see git log for SHA at merge)
date: 2026-04-21
---

# R21 Implementer Report — XPC.E.late-init-needs-ignore-diff

**Summary:** R21 shipped end-to-end. Vertical slice mirrors S2/R16 exactly (types → registry → extraction → bridge → Shen rule → kernel wiring → fixtures → integration test). Registry seeded with 7 rows across 3 `(Group, Kind)` buckets from fg-manifold MRs !1048, !893, !1502 discovered via `glab`. `make test` green. Basic-fixture smoke passes (kernel loads, paren budget correct at 23 closers on the append-chain line).

## Registry seed (7 rows)

Discovered 2026-04-21 via `glab mr view/diff` from inside `/Users/reuben/fg/fg-manifold`.

| Group | Kind | FieldPath | FixPattern | MR |
|---|---|---|---|---|
| elbv2.aws.upbound.io | LB | spec.forProvider.clientKeepAlive | ignoreDifferences | !1048 |
| elbv2.aws.upbound.io | LB | spec.forProvider.idleTimeout | ignoreDifferences | !1048 |
| elbv2.aws.upbound.io | LB | spec.forProvider.enableHttp2 | ignoreDifferences | !1048 |
| elbv2.aws.upbound.io | LB | spec.forProvider.ipAddressType | ignoreDifferences | !1048 |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.name | ignoreDifferences | !893 |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.tags | ignoreDifferences | !893 |
| ecs.aws.upbound.io | Service | spec.forProvider.iamRole | omit-late-initialize | !1502 |

### Candidates dropped and why

- **!1172** (RDS Cluster `spec.initProvider`): ArgoCD SSA null-serialization bug, not late-init observed-field drift. Different class.
- **!1247** (DMS Endpoint `managementPolicies: [Observe]`): policy-matching fix — provider rejected `[Create Delete]` without Observe. Not late-init.
- **!1147** (S3 Bucket `managementPolicies: ["*"]`): explicit ownership override vs. an imperative patch. Not late-init.
- **!892** (LaunchTemplate `securityGroups` field-name rename): selector-resolution fix (R16 territory). The renamed path is populated from `securityGroupSelector`.

Target was ≥4 viable rows; shipped 7.

## Files modified

### Go side

- `pkg/types/types.go:734–776` — new `LateInitMapping`, `LateInitUsage` structs; two new `World` fields `LateInitMappings`, `LateInitUsages`.
- `pkg/ir/late_init_registry.go` — NEW; `LateInitRegistry()` returning the 7 rows with docstring documenting discovery method and scope decisions.
- `pkg/ir/trajectory_extract.go:41–44, 47–81` — wire registry population and `extractLateInitUsages()` parallel to the selector extraction. Reuses `walkScalarPath` (line 60–78 prior to edit); skips array-indexed paths with same TODO as R16.
- `pkg/checker/bridge.go:397–400, 725–768` — two new `sortedSection` calls (`late-init-mappings`, `late-init-usages`) and matching `cmp` + `toObj` helper pairs. `toObj` emits `late-init-mapping-fact` and `late-init-usage-fact` tuples.
- `pkg/checker/check_test.go` — `TestR21_LateInitDrift` inserted before `TestR17_FieldValidation`; same two-fixture pattern as `TestR16_SelectorDrift`.

### Shen side

- `kernel/r21-late-init-needs-ignore-diff.shen` — NEW; 5-helper pattern copied from r16 with names swapped: `r21-leaf-of`, `r21-last-seg`, `r21-entry-covers?`, `r21-covered?`, `r21-check-usage`, plus the top-level `check-r21`.
- `kernel/check.shen:38` — `(load "r21-late-init-needs-ignore-diff.shen")`.
- `kernel/check.shen:82–83` — two new `extract-section` bindings for `late-init-mappings` and `late-init-usages`.
- `kernel/check.shen:115` — `R21 (mark-rule "XPC.E.late-init-needs-ignore-diff" (check-r21 LateInitUsages IgnoreDiffEntries))`.
- `kernel/check.shen:121` — extended append chain with one extra `(append R20 R21)`, closer count went 22 → 23.

### Fixtures

- `testdata/fixtures/late-init-drift/{app,lb}.yaml` — NEW; `elbv2.aws.upbound.io/LB` with `idleTimeout` and `clientKeepAlive` set, no `ignoreDifferences` on the owning Application. Triggers 2 R21 diagnostics.
- `testdata/fixtures/late-init-drift-ok/{app,lb}.yaml` — NEW; same resource, Application declares `ignoreDifferences` with `jsonPointers` covering both fields. Triggers 0 diagnostics.

### Docs

- `docs/obligations.md:96` — ticked `late-init-needs-ignore-diff` as implemented with R21 citation and registry provenance.

## Verification

```
$ sed -n '121p' kernel/check.shen | tr -cd ')' | wc -c
      23

$ go run ./cmd/xpc check testdata/fixtures/basic
xpc: ok (no issues)

$ go run ./cmd/xpc check testdata/fixtures/late-init-drift | grep -c XPC.E.late-init-needs-ignore-diff
2

$ go run ./cmd/xpc check testdata/fixtures/late-init-drift-ok
xpc: ok (no issues)

$ make test
ok  	github.com/pyrex41/cross-validate-/pkg/checker	4.927s
ok  	github.com/pyrex41/cross-validate-/pkg/ir	0.337s
... (all packages green)
```

## Coverage scoreboard update

Research-doc taxonomy (`thoughts/shared/research/2026-04-18-fg-manifold-target-study.md`) sized the late-init bucket at ~15% of fg-manifold MR traffic. R21 primary coverage lands that bucket on the scoreboard.

| Wave | Rule(s) | Target-study bucket | Coverage |
|---|---|---|---|
| S1 | R17 (`XPC.A.resource-field-valid`) | schema-violation | 40% |
| S2 | R16 (`XPC.E.selector-needs-ignore-diff`) | selector-resolution | 20% |
| S3 | R15 (`XPC.D.kind-whitelisted`) | whitelist | 2% |
| S4–S5 | R18–R20 (render/values/determinism) | rendering | structural, not in MR bucket |
| **post-S5** | **R21 (`XPC.E.late-init-needs-ignore-diff`)** | **late-init** | **15%** |

**Primary coverage:** 62% → **77%** (40 + 20 + 2 + 15).

Remaining ~23% distributes across smaller MR buckets (CRD cost/deprecation edge cases, sync-wave ordering specifics, and idiosyncratic per-service fixes). No single follow-on rule would push coverage significantly higher; further work is a long tail.

## Caveats

1. **Replay hasn't validated the 77% claim.** Track 1's replay against 3 known-good tips produced identical diagnostic counts per tip but did NOT exercise R21 — we hadn't shipped it yet. A follow-on replay with R21 online will produce the actual fg-manifold-observed late-init error count; expect it to be meaningful given the registry's small size (7 rows covering 3 kinds) vs. the MR history's ~75 fixes in this class.
2. **Registry is thin (7 rows, 3 kinds).** Each new fg-manifold MR that touches `ignoreDifferences` or `managementPolicies` on a forProvider late-init is a candidate new row. Append-only; keep the MR citation.
3. **First-pass substring match shared with R16.** `r21-entry-covers?` checks whether the jsonPointer/jqPath string-contains the field's leaf segment. False positives possible if an Application's `ignoreDifferences` entry happens to mention the leaf for a different reason. Tighter per-app joins are deferred, mirroring the R16 roadmap.
4. **Array-indexed registry entries would be dropped.** Matches the R16 deferral policy; none of the 7 seed rows use `[]` paths, so this is moot for the shipped registry but relevant if a future row like `spec.forProvider.blockDeviceMappings[].volumeType` is added.
