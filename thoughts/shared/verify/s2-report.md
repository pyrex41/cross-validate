# S2 Verify — XPC.E.selector-needs-ignore-diff

**Date**: 2026-04-19
**Branch**: `claude/xpc-s2-selector-ignore-diff`
**Tip**: 9dda0c9
**Verifier**: haiku agent s2-verify

## Success criteria

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | `make test` passes | ✅ | All 10 test packages pass: `ok` shown for pkg/audit, pkg/checker, pkg/ir, pkg/report, pkg/snapshot, pkg/trajectory, pkg/types, internal/shenfull |
| 2 | `TestR16_SelectorDrift` exists + passes | ✅ | `go test ./pkg/checker/ -run TestR16 -v` output: `=== RUN   TestR16_SelectorDrift` followed by `--- PASS: TestR16_SelectorDrift (1.65s)` |
| 3 | Registry ≥20 entries, cited | ✅ | 53 total entries via `grep -c '^\s*{'`. Sample 1 (lines 20-26): `Group: autoscaling.aws.upbound.io, Kind: AutoscalingGroup, Reason: "Crossplane resolves selector to subnet-ID list; Argo sees it as 'added' and fights forever. commit 3ea4b28d0 (!1344)"`. Sample 2 (lines 28-34): `Reason: "Sibling Refs array also populated by Crossplane; same drift as the primitive. commit 037988f54 (!1250)"`. Sample 3 (lines 252-258): `Group: elbv2.aws.upbound.io, Kind: LB, Reason: "Selector → plural SG IDs; Argo sees list as added. commit 3ea4b28d0 ALB block (!1344)"` |
| 4 | `selector-drift` fixture → non-zero + ≥1 `XPC.E.*` | ✅ | Exit code 1. Output shows 2 `XPC.E.selector-needs-ignore-diff` diagnostics for vpcZoneIdentifierSelector→vpcZoneIdentifier and vpcZoneIdentifierSelector→vpcZoneIdentifierRefs (both registry entries matched and reported correctly) |
| 5 | `selector-drift-ok` fixture → exit 0 | ✅ | `go run ./cmd/xpc check testdata/fixtures/selector-drift-ok` output: `xpc: ok (no issues)` with exit code 0 |
| 6 | `make lint` — no new regressions | ✅ | `make lint` fails on pre-existing files in internal/shenfull/*, pkg/audit/proof.go, pkg/ir/builder.go, pkg/report/reporter.go, etc. Modified files in this branch (cmd/xpc/main.go, kernel/check.shen, pkg/checker/bridge.go, pkg/checker/check_test.go, pkg/ir/selector_registry.go, pkg/ir/trajectory_extract.go, pkg/types/types.go) do not appear in the lint output — no new regressions |

## Gating-rule audits

| Rule | Status | Evidence |
|---|---|---|
| No proof version bump | ✅ | `grep -r 'v4\|version.*4' pkg/audit/` returns no output (clean) |
| No `t.Parallel()` | ✅ | `git diff claude/phase1-cleanup..HEAD -- pkg/checker/check_test.go | grep -i 'parallel'` returns no output; TestR16_SelectorDrift does not call t.Parallel() |
| Shen path only, no obligation-framework entry | ✅ | `git diff claude/phase1-cleanup..HEAD -- pkg/checker/bridge.go | grep -E 'obligationRefForCode\|ObligationRef'` returns no output — no obligation-framework mappings added |

## Spot-checks

- **Sorted determinism**: bridge.go lines 411-413 confirm all three new sections use `sortedSection`:
  - `sortedSection("selector-mappings", w.SelectorMappings, selectorMappingCmp, selectorMappingToObj)`
  - `sortedSection("selector-usages", w.SelectorUsages, selectorUsageCmp, selectorUsageToObj)`
  - `sortedSection("ignore-diff-entries", buildIgnoreDiffEntries(w.ArgoApps), ignoreDiffEntryCmp, ignoreDiffEntryToObj)`

- **Kernel loads R16**: kernel/check.shen line 33 loads r16-selector-needs-ignore-diff.shen; line 102 binds R16 to `(check-r16 SelectorUsages IgnoreDiffEntries)`; line 106 appends R16 into the check pipeline.

- **Array-path TODO documented**: 31 registry entries contain "[]" (array-indexed paths). pkg/ir/trajectory_extract.go lines 82 and 104 document the TODO: "Array-indexed paths (containing "[]") are skipped with a TODO" and "TODO: implement array-path walking (spec step 3 note)". This is consistent with the spec's deliberate deferral of array-element handling.

## Verdict

**PASS** — ready for human gate and merge into `claude/phase1-cleanup`.

All 6 success criteria met. All 3 gating rules satisfied. Spot-checks confirm architectural decisions (sorted determinism, kernel wiring, array-path deferral). The rule XPC.E.selector-needs-ignore-diff (R16) is correctly implemented as a Shen-based direct rule with 53 registry entries, fixtures validate behavior (negative case fails with 2 diagnostics per selector, positive case passes), and test coverage is present and passing.
