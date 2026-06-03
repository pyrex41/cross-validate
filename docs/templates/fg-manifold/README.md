# fg-manifold adoption templates

Ready-to-copy files for wiring `xpc` into the `fg-manifold` GitOps repo. They
are kept here (not written into fg-manifold) so they can be reviewed first.
Every value has been validated against the real fg-manifold tree; the CI
snippets fold in the fixes from the adoption planning review.

## Files

| File | Goes to (fg-manifold) | What it is |
|------|-----------------------|------------|
| `xpc.yaml` | repo root | Tuned config: D5 store allowlist, D1/D3 left at verified defaults, postgresql state-bearing append, `alb-logs` carve-out, fg bypass aliases. Each entry is commented with why. |
| `.yamllint` | repo root | Layer-1 ruleset; ignores Helm-templated `*-values.yaml`. |
| `gitlab-ci.validate-manifests.yml` | merge into `.gitlab-ci.yml` | Report-only MR gate: yamllint (Layer 1) + `xpc check` (Layer 3). |
| `gitlab-ci.xpc-baseline.yml` | merge into `.gitlab-ci.yml` | Nightly `.xpcsnap` baseline artifact (Part B). |
| `.xpc-waivers.yaml` | repo root | Accepted-risk register (example). Lets you flip the gate to blocking while known findings sit waived with a reason + expiry. |

## Prerequisite: publish an xpc release

CI pulls a **pinned** xpc binary (decision: pinned release artifact). Nothing
runs until that exists:

1. Merge the adoption branch (`feat/fg-manifold-adoption`) — it carries the
   rule wave (D1/D2/D3/D5) and the `--from-cluster` capture.
2. Tag + publish a GitHub release of `pyrex41/cross-validate` with a
   `xpc-linux-amd64` asset.
3. Set `XPC_VERSION` in both CI snippets to that tag.

Heads-up (latent papercut): the Go module path is `…/cross-validate-` (trailing
hyphen) while the repo is `…/cross-validate`. That breaks `go install`, which is
why CI curls a release asset instead. Worth reconciling the module/repo names
eventually, but not required for this flow.

## Rollout

1. Drop `xpc.yaml` + `.yamllint` at fg-manifold root; merge the two CI jobs.
2. Run **report-only** (the validate job has `allow_failure: true`). Read the
   `xpc.sarif` artifact; expect the ~50 real INC-6-class findings.
3. Triage: fix or carve-out. Populate `allowed-provider-configs` from any
   `XPC.B.providerconfig-resolves` findings that are bootstrap/terraform-created
   (real but uncommitted) rather than typos.
4. For findings you accept but can't fix now (e.g. the ~29 Postgres orphan
   findings), add entries to `.xpc-waivers.yaml` with a reason + tracking ticket
   + expiry. Waived findings drop out of the failing set, so you can go blocking
   without fixing everything first — and expired waivers re-fire to force review.
5. When the *unwaived* set is clean, follow the CUTOVER note in
   `gitlab-ci.validate-manifests.yml` to make it blocking.

## Open decisions (in-file)

- **SARIF inline annotations:** GitLab `reports:sast` wants GitLab SAST JSON,
  not raw SARIF — the job ships `xpc.sarif` as a plain artifact. Decide whether
  to add a SARIF integration or a Code Quality conversion for inline MR UX.
- **Bare `aws-secrets-manager` store:** allowed in `xpc.yaml`; drop it to forbid
  bare (namespaced) store references.
- **Baseline render fidelity:** the nightly job is manifest-only; add
  helm/kustomize to the image for rendered-resource coverage.
