---
date: 2026-04-27
branch: claude/standalone-xpc-cli
pr: https://github.com/pyrex41/cross-validate/pull/5
status: OPEN — review core fixes committed locally; push/update PR next
---

# Handoff — PR #5 core review fixes

## TL;DR

PR #5 is the "standalone CLI first" branch. The initial review was broadly
valid: audit/proof provenance, proof rule coverage, `xpc bisect`, plan JSON,
snapshot/proof verification, and xpc.yaml validation had real correctness
holes. This pickup fixed the core blockers and re-ran tests.

The remaining posture is still CLI-first, not production CI/gating. The next
session should push this commit to PR #5, then decide whether to address the
remaining polish items before merge.

## Current branch state

Branch: `claude/standalone-xpc-cli`

Open PR: https://github.com/pyrex41/cross-validate/pull/5

Base branch on GitHub is currently `claude/build-xpc-type-checker-TfgsT`
(repo default), not `main`. PR #5 targets that base.

## Fixes landed in this pickup

### Audit/proof

- `RulesetDigest` now hashes the resolved `kernel/*.shen` files plus the
  Go-side known rule inventory. It is no longer a static hash of
  `XPC001..XPC011`.
- The CLI computes the digest from the same `--kernel-path` / discovery path
  used by `xpc check`.
- `RuleSubtrees` now include all active known rules, including `XPC012`,
  `XPC014`, `XPC.A/D/E/H/S/P.*`, and `XPC000`.
- Observed unknown/future rule codes are unioned into `RuleSubtrees` so proofs
  remain representable even before the static inventory is updated.
- `Proof.Verify()` now recomputes into locals and does not mutate
  `RootDigest` on failed verification.

### CLI behavior

- `xpc bisect` now exits nonzero and writes an explicit "not implemented yet"
  message to stderr instead of printing a plan and returning success.
- `xpc check --proof --snapshot` reuses the already-loaded snapshot and no
  longer reloads while ignoring the error.
- `runCheck` handles `os.Getwd()` errors instead of swallowing them.

### Plan JSON

- `plan.WriteJSON` now emits structured destructive rows:
  `code`, `message`, `apiVersion`, `kind`, `namespace`, `name`, `app`,
  `reason`, `source`.
- The row is joined back to the `ResourceDelta` via the diagnostic source
  location instead of stuffing `Source.File` into `apiVersion` and the
  diagnostic message into `name`.

### Snapshot/proof integrity

- `Snapshot.Verify()` no longer mutates `Digest`.
- `snapshot.Diff` now truncates digest strings safely and no longer panics on
  malformed/short digests.

### xpc.yaml validation

- `immutable-fields` entries now require `paths` even when `suppress: true`;
  suppress-without-paths was previously a silent no-op.
- Ambiguous two-segment grouped GVKs like `apps/StatefulSet` are rejected with
  guidance to use `group/version/Kind`.
- Core API two-segment GVKs like `v1/ConfigMap` remain accepted.

## Verification

All passed after the fixes:

```bash
go test -count=1 ./pkg/audit ./pkg/config ./pkg/plan ./pkg/snapshot ./cmd/xpc
go test -run TestPlan_WriteJSON -count=1 ./pkg/plan
go test -count=1 ./...
go vet ./...
```

## Remaining review items

These were intentionally not fixed in this pass:

- `pkg/checker/bridge.go` still uses a bounded `os.Chdir(absKernel)` during
  one-time Shen kernel load. Risk is medium for current CLI, higher for
  concurrent library usage. Better fix is to teach the Shen loader absolute
  paths or guard all cwd-sensitive code behind a shared process-global mutex.
- `xpc plan` config semantics still differ from `xpc check` by design:
  explicit `--config` / `XPC_CONFIG_PATH` is loaded once; absent explicit
  config discovers per variant worktree. The behavior is right, but deserves
  a focused test/comment if reviewers keep tripping over it.
- `plan.Diff` still uses strict `reflect.DeepEqual` for modified detection.
  R27 documents the type-sensitive choice; `Diff` should either document this
  more explicitly or normalize if we want Kubernetes-semantic equality.
- `kernel/r24-appset-finalizer-without-preserve.shen` still has mixed named
  vs `_` pattern slots. Tests load today; naming the branch-2 slots would be
  cheap hygiene.
- Top-level YAML unknown-key warnings still print before nested strict-parse
  errors. Valid minor UX issue.

## Next recommended steps

1. Push this commit to `origin/claude/standalone-xpc-cli` so PR #5 updates.
2. Ask reviewer/Bugbot for a second pass focused on the fixed high-impact
   items.
3. Decide whether to handle the remaining medium/polish items before merge or
   track them as follow-ups.
4. Land the separate fg-manifold `xpc.yaml` alias commit upstream before
   deploying the cross-validate chunk that removes the FacilityGrid alias from
   compile-time defaults.
5. After PR #5 lands, the next major chunks are still:
   - release archive packaging (`xpc` binary + `kernel/`)
   - CI/GitHub Action wrapper and sticky PR comment
   - provider registry split / `cloud-providers`
   - optional R12 external-secret/filter story for fg-manifold gating

## Production-readiness note

The standalone CLI path is now in credible near-production shape for manual
checks and replay-backed validation. It is not yet a production CI product:
release packaging, action integration, gating policy, and some audit-contract
hardening remain.
