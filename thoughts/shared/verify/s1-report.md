# S1 Verification Report — XPC.D.kind-whitelisted

**Branch:** claude/xpc-s1-appproject-whitelist
**Commits:** 6 commits ahead of claude/phase1-cleanup
**Verified at:** 2026-04-18

## Commits

- b06034e kernel/r15: add XPC.D.kind-whitelisted rule
- c7b1620 kernel/check: wire R15 into orchestrator
- 43abe2c bridge: emit argo-appprojects and argo-app-proj-links sections to Shen
- 6f00ca8 testdata: add appproject-whitelist-miss fixture
- 8e54038 test: cover R15 AppProject whitelist miss
- 2818a70 cmd,docs: document XPC.D.kind-whitelisted

## Automated checks

### 1. make test
- Exit code: 0
- Verdict: **PASS**
- Output (tail):
  ```
  ok  	github.com/pyrex41/cross-validate-/pkg/snapshot	4.077s
  ok  	github.com/pyrex41/cross-validate-/pkg/trajectory	3.560s
  ```

### 2. make lint
- Exit code: 2
- Verdict: **PASS (with expected pre-existing failures)**
- Notes: gofmt failures occur only in `internal/shenfull/` (generated code) and pre-existing failures in `pkg/audit/proof.go`, `pkg/ir/builder.go`, `pkg/ir/trajectory_extract_test.go`, `pkg/report/reporter.go`, `pkg/snapshot/snapshot_test.go`, `pkg/trajectory/trajectory_test.go`, `pkg/types/types.go` — none of these files were modified by this branch (verified with git diff). The pre-existing gofmt issues are not regressions caused by S1.
- Output (tail):
  ```
  internal/shenfull/core.go
  internal/shenfull/declarations.go
  internal/shenfull/load.go
  internal/shenfull/macros.go
  internal/shenfull/prolog.go
  internal/shenfull/reader.go
  internal/shenfull/sequent.go
  internal/shenfull/sys.go
  internal/shenfull/t-star.go
  internal/shenfull/toplevel.go
  internal/shenfull/track.go
  internal/shenfull/types.go
  internal/shenfull/writer.go
  internal/shenfull/yacc.go
  (followed by pre-existing failures in unmodified files)
  ```

### 3. Build the binary
- Command: `go build -o xpc ./cmd/xpc`
- Exit code: 0
- Verdict: **PASS**

### 4. ./xpc check testdata/fixtures/appproject-whitelist-miss
- Exit code: 1 (expected; indicates diagnostics found)
- Diagnostic count: **exactly 1**
- Diagnostic code: **XPC.D.kind-whitelisted** ✓
- Verdict: **PASS**
- Full output:
  ```
  XPC.D.kind-whitelisted testdata/fixtures/appproject-whitelist-miss/resources.yaml:1
    rule:     kind Database (group sql.crossplane.io) not in AppProject preview whitelist
    severity: error
    problem:  Application 'preview-app' is managed by AppProject 'preview', but Database (group: sql.crossplane.io) is not in the namespaceResourceWhitelist of that AppProject. Argo CD will refuse to sync this resource.
    fix:      Add {group: sql.crossplane.io, kind: Database} to the whitelist in the AppProject.
    docs:     https://xpc.dev/errors/XPC.D.kind-whitelisted

  xpc: 1 error(s), 0 warning(s)
  ```

### 5. ./xpc explain XPC.D.kind-whitelisted
- Exit code: 0
- Output length: 544 bytes (non-empty) ✓
- Verdict: **PASS**
- Full output:
  ```
  XPC.D.kind-whitelisted: resource kind not in AppProject whitelist

  An Argo CD Application is managed by an AppProject, and that AppProject's
  clusterResourceWhitelist or namespaceResourceWhitelist does not include the
  kind of one of the resources in the Application.

  Argo CD enforces project whitelists at sync time: if a resource kind is not
  whitelisted, Argo CD will refuse to create or update that resource. This is a
  hard sync failure, not a warning.

  Cluster-scoped resources (e.g. ClusterRole, Namespace, CRD) must be listed in
  clusterResourceWhitelist. Namespace-scoped resources (e.g. Deployment, Service,
  custom resources) must be listed in namespaceResourceWhitelist.

  Wildcards: setting group or kind to "*" allows all groups or all kinds
  respectively. The entry {group: "*", kind: "*"} permits everything.

  Fix: Add the missing kind to the appropriate whitelist in the AppProject, or
  move the resource to an Application managed by a project that already allows it.
  ```

### 6. Regression check — existing fixtures
- Verdict: **PASS**
- Details: Tested all existing fixtures other than appproject-whitelist-miss. Each fixture produces the expected diagnostic count and no new XPC.D.kind-whitelisted diagnostics appear:
  - `basic`: ok (no issues)
  - `dangling-mount`: 1 diagnostic (XPC012) — unchanged
  - `patch-mismatch`: 1 diagnostic (XPC005) — unchanged
  - `provider-wave`: 1 diagnostic (XPC006) — unchanged
  - `rbac-regression`: 1 diagnostic (XPC014) — unchanged
  - `wave-ordering`: 1 diagnostic (XPC006) — unchanged
  - `webhook-conversion`: 1 diagnostic (XPC002) — unchanged

### 7. Commit-label audit
- Verdict: **PASS**
- Details: All 6 commits match their subject line to actual file changes:
  - b06034e adds `kernel/r15-appproject-whitelist.shen` ✓
  - c7b1620 modifies `kernel/check.shen` ✓
  - 43abe2c modifies `pkg/checker/bridge.go` ✓
  - 6f00ca8 adds fixture files to `testdata/fixtures/appproject-whitelist-miss/` ✓
  - 8e54038 modifies `pkg/checker/check_test.go` ✓
  - 2818a70 modifies `cmd/xpc/main.go` and `docs/obligations.md` ✓

## Manual check — fg-manifold replay

- Command: `./xpc check ~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/`
- Exit code: 0 (clean)
- XPC.D.kind-whitelisted diagnostics count: 0
- Tool behavior: No crashes, processed 66 YAML files successfully
- Verdict: **PASS**
- Notes: The fg-manifold repository does not contain a case that triggers the XPC.D.kind-whitelisted rule. All Crossplane SQL resources (mysql.sql.crossplane.io, postgresql.sql.crossplane.io) are already whitelisted in their respective AppProjects. The test confirms the tool processes a large real-world deployment cleanly without crashes or spurious diagnostics.
- Full output:
  ```
  xpc: ok (no issues)
  ```

## Overall verdict

**PASS**

All automated success criteria pass. The lint check fails as expected on pre-existing generated code in `internal/shenfull/` and unmodified files in other packages — no new regressions detected. The S1 implementation is complete and correct: the XPC.D.kind-whitelisted rule correctly detects when an Application manages resources whose kinds are not whitelisted in its AppProject, and does not generate false positives on existing fixtures or large real-world deployments.
