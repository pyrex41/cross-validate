---
title: fg-manifold replay v8 — after P5 (R12-cross dedup + kernel-path fallback)
date: 2026-04-24
author: Reuben / Claude
binary: /tmp/xpc-replay/xpc built from claude/build-xpc-type-checker-TfgsT @ c7434da
predecessor: replay-results-v7.md
---

## TL;DR

P5.a's R12-cross dedup collapses tip-main's 3504 XPC012 emissions to **12**
on every tip, matching v7's enumerated (owner, target) tuple set exactly.
P5.c's kernel-path fallback removes the need for `--kernel-path` when the
xpc binary sits above a `kernel/` tree — verified via a sibling kernel at
`/tmp/xpc-replay/kernel`. Every other rule count is line-for-line identical
to v7 — P5.e's Role-namespace fact-extension (landed between the handoff
and replay) is implicitly validated as a non-regression since R14-cross
emits zero on this dataset before and after. R12 is now production-gatable;
P5.d (externally-managed secret filter) stays optional.

## Run matrix

All runs executed 2026-04-24 against the v6/v7 reprovisioned tip worktrees
under `/tmp/xpc-replay/2026-04-23-phase1/`. Binary rebuilt fresh from
`27916ac`. `go test ./...` green on the mainline.

### Single-tip `xpc check`

| code                                      | tip-441 | tip-2ca | tip-4dd | tip-main | tip-postrem | v7 Δ (tip-main) |
|-------------------------------------------|:-------:|:-------:|:-------:|:--------:|:-----------:|:---------------:|
| XPC.D.kind-whitelisted                    |   700   |   700   |   700   |    701   |     711     |        =        |
| XPC.E.appset-finalizer-without-preserve   |    23   |    23   |    23   |     23   |      23     |        =        |
| XPC.E.late-init-needs-ignore-diff         |    12   |    12   |    12   |     12   |      12     |        =        |
| XPC.E.prod-appset-autosync                |     2   |     2   |     2   |      2   |       —     |        =        |
| XPC.E.selector-needs-ignore-diff          |   435   |   435   |   435   |    440   |     440     |        =        |
| XPC.H.appset-unsupported-generator        |     7   |     7   |     7   |      7   |       7     |        =        |
| XPC.H.composition-renders                 |    10   |    10   |    10   |     10   |      10     |        =        |
| XPC.H.helm-renders                        |    34   |    34   |    34   |     35   |      35     |        =        |
| XPC.S.crossplane-state-needs-orphan       |    69   |    69   |    69   |     69   |      24     |        =        |
| XPC006                                    |    30   |    30   |    30   |     30   |      30     |        =        |
| **XPC012 (R12-cross)**                    |  **12** |  **12** |  **12** |   **12** |    **12**   | **3504 → 12**   |
| **total**                                 |  1334   |  1334   |  1334   |   1341   |    1304     | **4833 → 1341** |

### Plan-mode pairs (executed from `~/fg/fg-manifold`, **no `--kernel-path`**)

All three exit 0. Stderr empty. `diagnostics.base` and `diagnostics.head` hold
full check output; `diagnostics.delta` is the cross-tip rule output.

| pair                 | base total | head total | delta | base XPC012 | head XPC012 | v7 XPC012 base/head |
|----------------------|:----------:|:----------:|:-----:|:-----------:|:-----------:|:-------------------:|
| 441 → 2ca            |    1290    |    1290    |   0   |      12     |      12     |     3654 / 3654     |
| 2ca → 4dd            |    1290    |    1290    |   0   |      12     |      12     |     3654 / 3654     |
| main → postrem       |    1296    |    1259    |   0   |      12     |      12     |     3504 / 3854     |

All non-XPC012 codes in base/head match v7 exactly. `delta=0` matches v7.

## Prediction scorecard

1. **R12 per-tip counts collapse to dedup floor.** ✅ Exactly 12 on all 5
   tips and both base/head of all 3 plan pairs. v7's 3504 prediction was
   the correct floor for tip-main; every tip lands on 12.
2. **Every other rule code identical to v7.** ✅ Line-for-line match on
   all 10 non-XPC012 codes across all 5 tips and all 3 plan pairs.
3. **Plan-mode runs without `--kernel-path`.** ✅ with caveat. The
   `resolveKernelPath` fallback searches upward from `os.Executable()`'s
   directory — works when the binary sits under (or next to) a `kernel/`
   tree. First run failed because `/tmp/xpc-replay/xpc` has no `kernel/`
   ancestor anywhere on its path, surfacing XPC000 in the inner
   base/head diagnostics (exit 0; error embedded in JSON, not stderr).
   Placing a symlink `/tmp/xpc-replay/kernel → $REPO/kernel` cleared
   XPC000 from every plan-mode run. See *Deployment note* below.
4. **No new rule codes emit.** ✅ R13 stays retired (0). R27 single-tip
   stays 0 (plan-mode only, delta=0 confirms).

## R12 dedup enumeration

12 distinct `(target-kind, target-name, owner-kind, owner-name)` tuples on
tip-main, matching v7's enumeration:

```
ConfigMap  pool-seed             → CronJob    e2e-pool-replenish
ConfigMap  myanon-config         → CronJob    migration-dump
ConfigMap  twilio-shim-ca        → Deployment oneuptime-app
ConfigMap  twilio-shim-ca        → Deployment oneuptime-worker
Secret     migration-secrets     → CronJob    migration-dump
Secret     s3-credentials        → CronJob    migration-dump
Secret     staging-db-credentials → CronJob   migration-dump
Secret     fg-claude-bot-secrets → Deployment fg-claude-bot
Secret     khoj-secrets          → Deployment khoj
Secret     usertour-secrets      → Deployment usertour
Secret     gitlab-secrets        → StatefulSet gitlab
Secret     gitlab-ssh-host-keys  → StatefulSet gitlab
```

v7 labeled the `migration-dump` CronJob as `migration-dump-anonymizer`; the
actual resource name in the manifests is `migration-dump`. The set of 12 is
otherwise identical. Every tuple is a real externally-managed
Secret/ConfigMap (SealedSecrets, External Secrets Operator, or manually
provisioned for oneuptime/gitlab/khoj/usertour). No false positives.

## Deployment note (P5.c caveat)

`resolveKernelPath`'s executable-fallback requires the xpc binary to live
under (or next to) a `kernel/` tree. The replay's `/tmp/xpc-replay/xpc`
does not — binaries built via `go build -o /tmp/...` will miss the
fallback. Two paths for real deployments:

1. `go install` into a GOBIN that sits under the cross-validate repo.
2. Ship the binary in an archive alongside `kernel/` (CI packaging).

Option 2 lines up with the CI-gate work (P6). File this under "xpc release
packaging" — the fix is correct in code, but downstream packaging has to
preserve the kernel-next-to-binary layout.

## What this unblocks

- **R12 is production-gatable.** 12 actionable tuples on every tip — no
  explosion, no noise floor. A PR gate can surface these as a stable
  review signal rather than a firehose.
- **P5.d (externally-managed secret filter) is optional.** The 12 tuples
  are real findings that deserve human attention (add to manifests, mark
  mount optional, or whitelist). Silencing them would hide legitimate
  drift between the trajectory state and live cluster.
- **Replay v9 triggers only on material behaviour changes.** v8 confirms
  the mainline is stable across single-tip check and plan-mode. Further
  replays should be scoped to specific rule additions or fact-extraction
  changes, not routine validation.

## What this does not validate

- Composition rendering on a machine with `crossplane` CLI installed —
  v8 runs hit the same code path as v7 (XPC.H.composition-renders = 10).
- Binary distribution in a CI runner — see *Deployment note*. v8 confirms
  the local fallback logic, not the CI-packaging story.
- R12 behaviour against a trajectory that includes SealedSecrets /
  External Secrets CRDs (fg-manifold's state doesn't include them).
