---
title: P5.d — externally-managed secret handling for R12 (XPC012)
date: 2026-04-24
author: Reuben / Claude
status: design (options appraised, recommendation, not yet scheduled)
predecessor: thoughts/shared/verify/replay-results-v8.md
related: thoughts/shared/plans/2026-04-23-variant-diff-and-composition.md (xpc.yaml open question)
---

## Context

Replay v8 (`thoughts/shared/verify/replay-results-v8.md:41`) confirmed that
after P5.a's dedup, tip-main emits exactly 12 XPC012 findings. The v8
TL;DR explicitly marks P5.d as optional
(`thoughts/shared/verify/replay-results-v8.md:19`), and the "What this
unblocks" section frames the 12 tuples as actionable drift rather than
noise (`thoughts/shared/verify/replay-results-v8.md:120-123`). The 12
tuples are enumerated at `replay-results-v8.md:80-93` and every one is a
real mount of a Secret/ConfigMap that xpc's trajectory state doesn't
contain — produced by SealedSecrets (bitnami-labs), External Secrets
Operator (ESO), or manual/out-of-band provisioning for the oneuptime,
gitlab, khoj, and usertour stacks.

This doc appraises three options for how xpc should handle the
externally-managed class:

(a) **Silence via registry.** Ship a name-pattern registry that
    suppresses R12 when the "missing" target matches; `xpc.yaml` extends
    it.
(b) **Demote to info-severity.** Introduce `XPC012.I.externally-sourced`
    (info severity) alongside the existing error, triggered by new
    SealedSecret/ExternalSecret facts.
(c) **Punt to fg-manifold.** xpc stays as-is; downstream tooling marks
    each finding.

## Current code: what R12 sees today

**Kernel rule.** R12-cross is defined at
`kernel/r12-no-dangling-mount.shen:110-134`. The violation test on line
116 is `(not (mount-ref-violates? OK ON ONs TK TN TNs Trajectory))`; the
emission constructor `r12-cross-emit` at lines 91-104 builds an
`XPC012` error via `make-error`. There is exactly one semantic hinge:
line 69, `(not (key-in? TK TN TNs State))` — "target is absent from
State." That's the single place where an "externally managed" early-exit
would plug in. An optional `(externally-managed? TK TN TNs)` guard would
sit in `check-r12-cross-fold` between the `mount-ref-violates?` check
(line 116) and the dedup check (line 118).

**Fact shape.** `mount-ref-fact` is serialised by
`pkg/checker/bridge.go:737-745` from `types.MountRef`
(`pkg/types/types.go:640-650`). It carries OwnerKind/Name/Namespace,
TargetKind/Name/Namespace, MountKind, Optional, and Source — no notion
of "sourced from SealedSecret" yet. Extraction runs per
Pod/Deployment/StatefulSet/DaemonSet/ReplicaSet/Job/CronJob in
`pkg/ir/trajectory_extract.go:20-39`, calling `extractFromPodSpec`
(`pkg/ir/trajectory_extract.go:256-410`).

**Diagnostic metadata.** `objToObligationRef` at
`pkg/checker/bridge.go:1271` maps XPC012 to `XPC.F.no-dangling-mount`.
No sibling `.I.` or `.W.` codes exist today for R12.

**Severity.** `types.SeverityInfo` exists at
`pkg/types/types.go:20` and is plumbed through `objToSeverity` in
`pkg/checker/bridge.go:1310-1311`, but the kernel prelude only
exposes `make-error` (prelude.shen:87-90) and `make-warning` (92-95).
A new `make-info` constructor would be a trivial addition
(three lines of shen).

## Current code: what xpc ingests today (SealedSecret / ExternalSecret)

**None.** A repo-wide grep for `SealedSecret`, `ExternalSecret`,
`sealed-secrets`, `bitnami`, `external-secrets` turns up zero matches
outside `thoughts/`. `pkg/ir/builder.go:1509-1549` treats every loaded
document uniformly — it reads `metadata.name`, `metadata.namespace`,
annotations, labels, and stores the raw map on `ResourceInfo`. There is
no special-casing per kind, and no derived fact of the form "a
SealedSecret named X will produce a Secret named X in namespace Y".
`pkg/ir/state_bearing_registry.go:26` and the registry-style files
alongside it could grow a third sibling (`sealed_secret_registry.go`
and/or `external_secret_registry.go`), but neither exists today.

This is the load-bearing fact for option (a) and (b) cost estimates:
**any "externally managed" awareness needs new fact extraction from
scratch.**

## xpc.yaml status

Open question #1 from the P1 plan notes that xpc currently has no
config file and proposes `xpc.yaml` as the natural solution
(`thoughts/shared/plans/2026-04-23-variant-diff-and-composition.md:314-321`).
That work isn't done, and it's the obvious host for any user-facing
pattern list. Nothing in the tree loads a YAML config yet — introducing
one is a prerequisite for option (a)'s "users extend via xpc.yaml" clause.

## Option (a) — silence via registry

**Implementation cost.** Three touchpoints, roughly 180-250 LOC:

1. `pkg/ir/sealed_secret_registry.go` (new, ~40 LOC) — built-in name
   patterns, mirrors `state_bearing_registry.go`'s shape.
2. `pkg/ir/trajectory_extract.go` — a new `extractSealedSecretTargets`
   that walks `w.Resources` for kind `SealedSecret` (bitnami) and
   `ExternalSecret` (ESO), pulls `spec.template.metadata.name` /
   `spec.target.name` or falls back to `metadata.name`, and stashes a
   set on `World`. ~50-80 LOC plus test fixture.
3. `kernel/r12-no-dangling-mount.shen` — add a pre-emission guard in
   `check-r12-cross-fold` (line 116-124) that skips when the target is
   in the "externally managed" set. This requires plumbing a new fact
   list through `check-r12-cross`'s arity, which cascades to
   `pkg/checker/bridge.go:423` (the `mount-refs` section) and the R12
   call site in the `check-r12-cross` wrapper. ~40 LOC across Go and
   Shen, plus the pattern-match hazards in MEMORY.md
   (`feedback_shen_kernel_gotchas.md`).
4. `xpc.yaml` support for extension requires the config-loader work
   that's already pending from P1.open-question-1 — a separate ~150 LOC
   chunk. Without it, option (a) is "hardcoded registry only."

**False-positive risk: high.** This is the decisive problem. Three of
the current 12 (fg-claude-bot-secrets, gitlab-secrets,
gitlab-ssh-host-keys) are manually provisioned — they have no
SealedSecret or ExternalSecret resource to key off. A pattern-based
registry would either miss them (option (a) doesn't silence them, so
it only partially solves the stated problem) or overreach by silencing
any Secret named `*-secrets`, which would silence legitimate missing
secrets too. The replay-v8 conclusion explicitly says
"silencing them would hide legitimate drift"
(`replay-results-v8.md:122-123`); a pattern registry is exactly the
silencing mechanism that note warns against.

**User visibility.** A fg-manifold reviewer sees 0-3 XPC012 instead of
12. There is no signal that xpc saw the mount and decided to suppress —
the finding is simply absent from the PR comment. The reviewer would
need to consult xpc.yaml or the hardcoded registry to know why.

**Forward compatibility.** Poor. A future rule like "Pod mounts Secret
X, SealedSecret X exists, but SealedSecret is in a different namespace"
would be awkward to wire into a registry whose only output is a
silencing bit. Positive-assertion rules want the inverse: facts
extracted and exposed to the kernel, then rules that match on them.

## Option (b) — demote to info via `XPC012.I.externally-sourced`

**Implementation cost.** Broader than (a) but cleaner: ~250-350 LOC.

1. Fact extraction as in (a) step 2 (~80 LOC), but the output is a
   proper `SealedSecretFact` / `ExternalSecretFact` type on `World`
   rather than a silencing set. This lives next to
   `CPDeletionPolicyFacts` (`pkg/types/types.go`, alongside the
   `MountRef` block at 640). Serialised in bridge.go next to
   `mountRefToObj` at 737.
2. `kernel/prelude.shen` — add `make-info` constructor, ~4 LOC, mirror
   of `make-warning` (lines 92-95).
3. `kernel/r12-no-dangling-mount.shen` — in `check-r12-cross-fold`,
   instead of skipping matched tuples, branch to a new emitter
   `r12-cross-emit-info` that produces `XPC012.I.externally-sourced`
   with `make-info`. ~30-40 LOC of Shen. The original error stays for
   genuinely dangling mounts.
4. `pkg/checker/bridge.go:1271` — add
   `"XPC012.I.externally-sourced": {"I", "externally-sourced"}` plus
   help text in `cmd/xpc/main.go` next to the existing detail strings
   (~30 LOC).
5. Tests in `pkg/checker/check_test.go` — one fixture per class
   (SealedSecret, ExternalSecret, manual = still error). ~60 LOC.

**False-positive risk: low-to-medium.** The error still fires for
mounts with no matching SealedSecret/ExternalSecret — so manual cases
(gitlab, khoj, usertour, fg-claude-bot) keep erroring. Only tuples
that xpc can positively prove are externally sourced get downgraded.
The risk is that a misnamed SealedSecret silently downgrades a real
drift, but the reviewer still sees an info-severity line naming the
exact (owner, target) pair, so it is visible rather than hidden.

**User visibility.** Reviewer sees (for tip-main today) 4 errors + 8
info lines, or whatever the split turns out to be. The info lines
carry the same (owner, target, source) coordinates as the current
error, plus a distinct code. PR comment rendering needs to handle
info severity if it doesn't already — worth checking against
`pkg/report/` but out of scope for this doc.

**Forward compatibility.** Good. Once SealedSecret/ExternalSecret
facts exist, future rules can pattern-match on them — "SealedSecret X
exists but is in a different namespace than the mount,"
"ExternalSecret X has no SecretStore in scope," etc. The fact
extraction in step 1 is the reusable primitive; R12's info-variant is
just one consumer.

## Option (c) — punt to fg-manifold

**Implementation cost.** Zero xpc code. Downstream work in fg-manifold
to either (i) tag known-externally-managed mounts in a manifest comment
that fg-manifold's tooling reads, or (ii) post-process xpc's JSON
output to drop known pairs. Either is ~30-100 LOC of bash/Python in
fg-manifold, not in this repo.

**False-positive risk: zero** — xpc's output is already correct by
construction; it reports what it sees.

**User visibility.** Same as today: 12 XPC012 lines in the PR
comment. fg-manifold reviewers either learn to scan-and-ignore, or
fg-manifold's comment renderer suppresses the known set before
rendering. The quality of the signal depends entirely on how
fg-manifold handles it.

**Forward compatibility.** Excellent for xpc — no assumptions baked
in. But moderate overall: every downstream consumer repeats the same
work. If cross-validate grows a second consumer beyond fg-manifold,
they rebuild the filter.

## Recommendation: (c), with the door open to (b)

**Do (c) now.** The replay-v8 conclusion is right — these 12 are real
findings, and xpc's job is to surface drift, not to second-guess it.
The three manual-provision cases (gitlab/khoj/usertour/fg-claude-bot)
cannot be covered by a pattern registry (option a) without
unacceptable false-positive risk, and the SealedSecret/ExternalSecret
fact-extraction work (option b) is ~300 LOC of new machinery for a
12-item residue that is already actionable. The cost/benefit ratio for
(b) at 12 findings doesn't justify the complexity today.

**Keep option (b) on the shelf.** If the tuple count grows past ~30
across the fleet, or if a second downstream consumer appears, reopen
this doc. The fact-extraction primitive (step 1 of option b) is
genuinely useful beyond R12 — a SealedSecret-namespace-mismatch rule
or an ESO SecretStore scoping rule would both reuse it. When that
rule gets scheduled, fold the SealedSecret/ExternalSecret fact work
into its plan.

**Reject option (a).** It has the worst false-positive profile of the
three and relies on `xpc.yaml` infrastructure that hasn't shipped.
Registry-only silencing is the mechanism the v8 note warns against.

## Concrete implementation plan for the recommendation (option c)

Since (c) is "do nothing in xpc," the plan is documentation- and
operator-facing, not code. Phrased as follow-up chunks:

- **P5.d.1 — documentation.** Extend `cmd/xpc/main.go`'s XPC012
  detail string (near line 1200 in the big help-text block) with one
  paragraph: "If the target is managed by SealedSecrets, External
  Secrets Operator, or is provisioned out-of-band, xpc will still flag
  it because xpc only observes the checked-in manifest state. Filter
  or annotate these downstream." ~20 LOC, one file.

- **P5.d.2 — fg-manifold filter list.** In fg-manifold (separate
  repo), add a `ci/xpc-known-external-mounts.yaml` that enumerates the
  12 tuples, and update the PR-comment renderer to demote matches to
  a folded "known external" section. Out of scope for cross-validate;
  tracked as a fg-manifold ticket.

- **P5.d.3 — watch for drift.** Next replay (v9 or later) should
  spot-check that the 12-tuple list hasn't grown. If it crosses ~30,
  reopen this doc and schedule option (b).

- **P5.d.4 (conditional, do not schedule yet) — SealedSecret /
  ExternalSecret fact extraction.** The work described in option (b)
  step 1. Skeleton for reference: new `types.SealedSecretFact` struct;
  `extractSealedSecretFacts` in `pkg/ir/trajectory_extract.go`
  alongside `extractCPDeletionPolicyFacts`; serialisation in
  `pkg/checker/bridge.go` alongside `mountRefToObj`. Only triggered
  when a downstream rule needs it.

- **P5.d.5 (conditional) — `XPC012.I.externally-sourced` variant.**
  Option (b) steps 2-5. Only after P5.d.4 lands.

That's five steps; the first three are the real ones, the last two
sit behind a trigger ("tuple count or second consumer").

## Open questions for the user

1. **Is fg-manifold's PR-comment renderer under our control, or is it
   a third-party that we'd have to fork?** This directly affects how
   cheap P5.d.2 is. If it's ours, (c) is genuinely zero-effort on the
   xpc side. If it's not, the case for (b) strengthens because we'd
   otherwise be paying the same downstream cost repeatedly.

2. **How stable is the 12-tuple list?** If it churns every week as
   teams add/retire externally-managed secrets, a static
   `ci/xpc-known-external-mounts.yaml` becomes a maintenance burden
   and the case for (b)'s fact-extraction (which auto-discovers
   SealedSecret/ExternalSecret CRDs) gets stronger. If it's stable
   month-to-month, (c) is fine.

3. **Does the fg-manifold trajectory actually include the
   SealedSecret / ExternalSecret CRD manifests?** The v8 notes say
   "fg-manifold's state doesn't include them"
   (`replay-results-v8.md:136`). Confirming this is the fulcrum of
   the entire argument: if they're not in the trajectory, option (b)
   can't find them either, and (c) is the only viable path. If they
   are — or could easily be included — then (b)'s cost estimate is
   tighter than I've written it here, and the recommendation should
   be revisited.
