---
name: xpc
description: Validate Crossplane + ArgoCD manifests with xpc; fix errors or record accepted-risk waivers with a written reason. Use whenever editing GitOps YAML in a repo that has xpc installed.
license: MIT
compatibility: Requires the xpc binary on PATH. Verify with `xpc version`.
allowed-tools: Bash(xpc:*)
---

# xpc — agent skill

xpc statically validates Crossplane + ArgoCD manifests against a kernel of named rules (R1–R27, codes `XPC.<Cat>.<generator>` or legacy `XPC###`). Findings are deterministic from the file tree; running it twice produces identical output.

You are most likely here because:

- The user just edited a manifest and wants it checked.
- The user is reviewing a PR/MR and wants the delta against `main` analysed.
- The user wants you to triage a backlog of findings and either fix them or record waivers.

This skill teaches you when to do which.

## Status

```
xpc version
```

If the binary isn't found, stop and tell the user. Don't try to install it yourself — installation choice (`go install`, packaged binary, vendored) is a project decision.

## Subcommand cheat sheet

| Verb | When |
|---|---|
| `xpc check [path]` | After any manifest edit. Default verb. |
| `xpc check --focus=inc6-floor` | Fast (~7s, no renderers needed for these rules), high-signal: state-needs-orphan + AppSet finalizer + prod auto-sync. |
| `xpc plan --base=<ref> --head=<ref>` | PR-time. Reports only what *changed* — destructive deletes, immutable-field changes. Less noisy than `check`. |
| `xpc explain <code>` | Show docs for a rule, e.g. `xpc explain XPC.S.crossplane-state-needs-orphan`. Read this BEFORE deciding whether to fix or waive. |
| `xpc snapshot [--output=.xpcsnap]` | Capture cluster type-environment. Run before `check --snapshot=` if rules need cluster CRDs. |
| `xpc skill install [target]` | What you used to get this file. |

Full flag reference: `xpc help`.

## Default workflow on edit

1. Edit the manifest the user asked you to edit.
2. `xpc check <path>` (the directory of the edit, or repo root if the edit crosses dirs).
3. Read the output. Each error block has structured fields: `code`, `severity`, `message`, `source`, `detail`, `fix`. Read `fix` first — that's almost always what to do.
4. If a rule fires that you don't recognise, run `xpc explain <code>` before acting.
5. Apply the fix; re-run `xpc check` until errors are zero.
6. Warnings are not blocking; summarise them in your reply and let the user decide.
7. **Never respond to the user with unfixed `error`-severity findings unless you've explicitly waived them — see below.**

## When `xpc check` is too noisy to be actionable

The full `xpc check` against a real repo can return hundreds of findings — some real, some that the team has knowingly accepted. If you're triaging a backlog, prefer:

- `xpc check --focus=inc6-floor` first. Returns only R23/R24/R25 (state-needs-orphan, AppSet cascading-finalizer, prod auto-sync). These are the "blast radius" rules. If any fire, fix them before anything else.
- Then `xpc check` (default) to see the rest.

## Fix vs. waive: the decision tree

For each `error`-severity finding:

1. **Read the `fix` field.** If it's mechanical and the user's task is consistent with applying it → apply it.
2. **Read `xpc explain <code>` if you don't already know the rule.** This is non-negotiable when the rule code is unfamiliar.
3. **Decide whether the finding is real or accepted-risk:**
   - **Real bug** → fix it. Default action.
   - **Accepted risk** (the user has explicitly told you it's fine, or there's a tracked external dependency that means we can't fix it now) → record a waiver (next section).
   - **Uncertain** → ask the user. Never silently skip a finding.
4. **Re-run `xpc check`** and confirm zero remaining `error`-severity findings before reporting done.

The pattern that gets you in trouble: ignoring a finding because it "looks fine" without either fixing or recording it. Future runs will surface it again, the user will ask why, and you'll have no record.

## Recording accepted findings (waivers)

When a finding is genuinely OK, do not just leave it firing. Record it in `.xpc-waivers.yaml` at the repo root with a written reason. The waiver file is the authoritative record of what's been intentionally accepted; it must travel with the manifest.

### File format

```yaml
# .xpc-waivers.yaml — accepted-risk register for xpc findings.
#
# Comments above each entry are the durable record of the decision.
# Treat them as production code: they survive edits, they're read by
# humans, and they should explain *why* the team accepts the finding.

waivers:
  - rule: XPC.S.crossplane-state-needs-orphan
    file: deploy/facilitygrid/ops/.../docdb-prod-cluster.yaml
    kind: Cluster
    name: docdb-prod-cluster
    reason: |
      Awaiting INC-6 follow-up MR to add deletionPolicy: Orphan to all
      DocDB resources in one batch. Tracked in ENG-1234. Until then we
      rely on the runtime VAP (commit 7589728b5) to block deletion.
    added_by: reuben
    added_at: 2026-05-04
    expires_at: 2026-06-01    # required: forces re-review

  - rule: XPC012  # no-dangling-mount
    file: deploy/.../e2e-pool-replenish/cronjob.yaml
    kind: CronJob
    name: e2e-pool-replenish
    reason: |
      pool-seed ConfigMap is created lazily by the bootstrap job that
      runs in a sibling Application; the dangling mount is intentional
      and the pod retries until ready.
    added_by: reuben
    added_at: 2026-05-04
    expires_at: 2026-08-04
```

### How to add a waiver

1. **You must read `xpc explain <code>` first.** Do not waive a rule you can't articulate.
2. Confirm with the user that the finding is accepted. Quote the message + source. *Get explicit confirmation* — never invent a waiver.
3. Append an entry to `.xpc-waivers.yaml`. If the file doesn't exist, create it.
4. The `reason:` field must be at least one full sentence explaining *why this is acceptable*. "Because" is not a reason; "because we rely on runtime VAP and INC-6 follow-up is tracked in ENG-1234" is.
5. Always set `expires_at` (suggest +90 days unless the user gives a different horizon). Permanent waivers age into lies; expiry forces re-justification.
6. Re-run `xpc check`. The waived finding should drop out of the report.

### Identity

A waiver matches a finding when its `rule` equals the diagnostic code, its `file` matches the source path (suffix / basename), and the diagnostic message contains `name` (and `kind`, when given). At least one of `file` or `name` is required, so a waiver can never blanket-suppress a whole rule. `namespace` is documentary.

`xpc check` honors `.xpc-waivers.yaml` automatically (upward search from cwd; override with `--waivers=<path>` or `XPC_WAIVERS_PATH`, disable with `--no-waivers`):

- A matched, **non-expired** waiver drops its finding from the report and the exit code (CI passes).
- An **expired** waiver does NOT suppress — the finding re-fires and a `XPC.W.waiver-expired` warning is emitted, so accepted-risk can't silently become permanent.
- A waiver matching **no** finding surfaces a `XPC.W.waiver-unused` info — remove it.
- Suppressed findings stay auditable: `xpc check` prints `waivers: N suppressed, …` to stderr, and `--show-waived` lists them.
- A malformed waiver file (missing `rule` / `reason` / `expires_at`, or a bad date) is a hard error.

> **Not yet implemented:** the `xpc waive <code> <file>:<resource> --reason=…` convenience command (hand-edit the file for now) and the content-hash *staleness warning* for a resource that changed after the waiver was written.

## PR / MR review

For pre-merge review, prefer `xpc plan` over `xpc check`. The plan-mode delta is the right gate for CI:

```
xpc plan --base=<merge-target-ref> --head=HEAD <path>
```

It emits `XPC.P.*` codes — `destructive-delete`, `cascade-risk`, `immutable-change` — for things that **changed** from base to head. A `check`-clean tree can still have a `plan`-flagging change (e.g., a state-bearing resource that was OK at base is now being deleted at head).

Treat any `XPC.P.destructive-delete` or `XPC.P.cascade-risk` as an automatic *stop and confirm with the user* — these are the INC-6 class of changes that have caused production incidents.

## Output formats

`xpc check --format=<fmt>`:

| Format | When |
|---|---|
| `agent` (default) | What you read. Dense key:value blocks. |
| `json` | Programmatic post-processing. |
| `sarif` | GitLab/GitHub SAST artifact. |
| `human` | Human terminal output. |
| `junit` | CI test-report consumers. |

If the user pipes output to `jq`, switch to `--format=json`.

## Common gotchas

- **Helm/Kustomize render failures** show up as `XPC.H.helm-renders` errors with severity `error`. These often mean *your local environment* can't render the chart (missing repo, private auth) — not that the manifest is broken. If you see a render error and the file looks well-formed, surface it to the user with the diagnostic, don't try to "fix" the chart.
- **`crossplane` CLI absent** → `XPC.H.composition-renders` warnings. Composition rendering is skipped; not blocking.
- **Snapshot stale** → run `xpc snapshot --output=.xpcsnap` against the user's current kubectl context, then re-check with `--snapshot=.xpcsnap`.
- **`XPC000` errors** are infrastructure failures (kernel bootstrap, etc.), not manifest bugs. Surface them; don't try to fix the manifest.

## Don't

- Don't waive a finding without an explicit user confirmation.
- Don't write a waiver `reason:` shorter than a full sentence.
- Don't omit `expires_at`.
- Don't disable rules globally (no support for that, by design).
- Don't run `xpc skill install` against arbitrary directories without the user's instruction.
- Don't silently `--skip-render` to suppress noise — the renderer findings are diagnostic, not safety-critical, but skipping them changes coverage. Surface the issue instead.

## When to ask before acting

- Two or more findings on the same resource where the fixes might conflict.
- Any `XPC.P.destructive-delete` or `XPC.P.cascade-risk`.
- A waiver decision (always — never invent one).
- A render failure that requires installing or configuring a tool on the user's machine.
