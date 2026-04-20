---
session: S4
rule: XPC.H.helm-renders + XPC.H.values-well-typed
branch: claude/xpc-s4-helm-renders
tip: 1b50df7bc8bd6d4f8dd5e41549fa2289ac72c331
date: 2026-04-20
implementer: opus
---

# S4 Implementer Report — XPC.H.helm-renders + XPC.H.values-well-typed

## Commits

| SHA | Summary |
|---|---|
| `b33bcc1` | renderer: add Helm rendering package with two-tier cache |
| `7104f2f` | types: add Provenance, ValuesIssue, RenderResult, World.RenderResults |
| `a0af4e0` | builder: render Helm sources into World.Resources + RenderResults |
| `5ad15e3` | bridge: emit render-results section for R18/R19 |
| `b932ec2` | kernel: add R18 helm-renders + R19 values-well-typed |
| `c0a6ff5` | cli: add --skip-render and --helm-bin=<path> to xpc check |
| `cb72cc7` | testdata+checker: helm fixtures and R18/R19 integration tests |
| `1b50df7` | docs: mark helm-renders and values-well-typed as implemented |

Total: 8 commits on `claude/xpc-s4-helm-renders`.

## Commit A — `pkg/renderer/` (b33bcc1)

- `renderer.go`: `Renderer` interface, `ErrHelmAbsent`, `ErrRendererUnsupported`, `ResolveChart(src, cwd)` that joins relative `src.Path` onto the Application's cwd.
- `helm.go`: `HelmRenderer{HelmBin, Cache}` with lazy `probe()` (caches resolved helm binary + `helm version --short`). `RenderChart(chartPath, helmSrc, namespace)` invokes `helm template <release> <chart> --namespace <ns> -f … --set k=v` under a 30s `context.WithTimeout`. Values merge order mirrors Argo: chart `values.yaml` ← listed `valueFiles` ← `valuesObject` ← inline `values` string. Canonical cache-key input is SHA-256 over chart-dir digest + sorted-JSON merged-values bytes + helm version + release name + namespace.
- `cache.go`: Two-tier cache. In-memory map + on-disk dir (default `~/.cache/xpc/renders/`). 15-min TTL honored on both tiers; stale entries evicted on access.
- `values_schema.go`: `ValidateValues(schemaJSON, mergedValues)` parses the bytes into a `map[string]interface{}` and delegates to `schemas.ValidateManifest` (S3), mapping each `ResourceFieldFact` to `ValuesIssue{Path, Message}`.
- `file_util.go`: small `readFile` / `writeTempValues` helpers.

## Commit B — types (7104f2f)

- `ResourceInfo.Provenance string` — `"direct"` (or empty) for on-disk, `"rendered:helm:<app>"` for Helm output.
- `ValuesIssue{Path, Message}`.
- `RenderResult{AppName, ChartPath, Success, Error, ErrorKind, ValuesIssues, Source}` — `ErrorKind` is a lowercase-dashed classifier (`helm-absent`, `helm-template-failed`, `helm-timeout`, `other`, `none`) so Shen patterns can match directly.
- `World.RenderResults []RenderResult` with `json:"-"` (same pattern S3 used for `ResourceFieldFacts`).

## Commit C — builder (a0af4e0)

Builder grows `SkipRender bool` and `HelmBin string` fields plus a lazily constructed `helmRenderer *renderer.HelmRenderer`. After `b.world.ArgoApps = append(...)`, a new `renderHelmSources(app, doc.Source.File)` hook runs when `!b.SkipRender`:

1. Resolve chart path with `renderer.ResolveChart`.
2. Probe for `values.schema.json`; if present, `MergedValues` + `ValidateValues` and stash issues on the pending `RenderResult`.
3. `HelmRenderer.RenderChart(chartPath, src.Helm, app.Destination.Namespace)`.
4. On success, parse rendered bytes with `loader.LoadReader`, stamp `Source = app.Source` + `Provenance = "rendered:helm:"+app.Name` on each rendered `ResourceInfo`, append to `World.Resources`, and append the `RenderResult`.
5. On failure, append a `RenderResult{Success:false, ErrorKind: classifyRenderError(err), Error: err.Error()}`; do not touch `World.Resources`.

## Commit D — bridge (5ad15e3)

`renderResultCmp` sorts on `(AppName, ChartPath)`. `renderResultToObj` emits:

```
(render-result <app-name> <chart-path>
               <render-ok|render-failed>
               <error-kind>
               <error-str>
               (values-issues (values-issue <path> <msg>) ...)
               (source <file> <line>))
```

The success discriminator is a lowercase-dashed symbol (`render-ok` / `render-failed`) rather than Shen's `true`/`false` booleans — Shen treats the boolean literals specially and the pattern-match in R18 did not fire against them during development. Switching to symbol-compare fixed R18 for `helm-render-fail`.

`worldToShenObj` inserts `sortedSection("render-results", w.RenderResults, renderResultCmp, renderResultToObj)` after the `resource-field-facts` line.

## Commit E — kernel rules (b932ec2)

- `kernel/r18-helm-renders.shen`: `r18-check-result` pattern-matches on the success symbol and ErrorKind. `render-ok → []`, `render-failed + helm-absent → make-warning`, `render-failed + other → make-error`. Error labels + fix hints are keyed off the ErrorKind symbol.
- `kernel/r19-values-well-typed.shen`: `r19-check-result` flattens the embedded `(values-issues ...)` list and emits one `make-error` per `(values-issue Path Msg)`. Matches both render-ok and render-failed so a failed render can still surface schema issues.
- `kernel/check.shen`: loads both rule files (after r17), adds one `RenderResults` let-binding, two `mark-rule` bindings (R18, R19), and grows the `append` cascade from `(append R16 R17)` to `(append R16 (append R17 (append R18 R19)))`. Paren budget on lines 112–114 holds at 19 opens / 21 closes (verified with `awk`, matches the briefing's +2 delta).

## Commit F — CLI (c0a6ff5)

- `--skip-render` (bool) and `--helm-bin=<path>` (string) flags in `runCheck`.
- Threads them into `ir.Builder.SkipRender` / `ir.Builder.HelmBin`.
- When `--skip-render` is set, emits one `XPC.H.helm-renders` info diagnostic per Application that had at least one `RendererHelm` source.
- `printUsage` lists both flags; `runExplain` gains entries for both codes; the "Known error codes" error message references them.

## Commit G — fixtures + tests (cb72cc7)

- `testdata/fixtures/{helm-render-ok,helm-render-fail,helm-values-mismatch}/` copied verbatim from `thoughts/shared/prep/fixtures/s4/`.
- `pkg/loader/loader.go` learns to skip `templates/` subdirectories of directories containing a sibling `Chart.yaml` — necessary so the direct-filesystem walker doesn't choke on un-rendered `{{ }}` template expressions.
- `pkg/checker/check_test.go`:
  - Default `loadFixture` now sets `SkipRender = true` so existing tests stay hermetic.
  - New `loadFixtureWithHelm` for R18/R19 tests.
  - `TestR18_HelmRenders` subtests for ok / fail / values-mismatch, gated on `exec.LookPath("helm")`.
  - `TestSkipRender_NoRenderedResources` confirms `SkipRender=true` produces zero rendered resources and zero `RenderResults`.

## Commit H — docs (1b50df7)

`docs/obligations.md` Category H: `helm-renders` and `values-well-typed` both flagged as **implemented** with rule + code references.

## Step 3 — Verify (literal tail output)

### `make test 2>&1 | tail -40`

```
go test ./... -count=1
?   	github.com/pyrex41/cross-validate-/cmd/xpc	[no test files]
ok  	github.com/pyrex41/cross-validate-/internal/shenfull	0.772s
ok  	github.com/pyrex41/cross-validate-/pkg/audit	1.826s
ok  	github.com/pyrex41/cross-validate-/pkg/checker	4.218s
ok  	github.com/pyrex41/cross-validate-/pkg/ir	2.570s
?   	github.com/pyrex41/cross-validate-/pkg/loader	[no test files]
ok  	github.com/pyrex41/cross-validate-/pkg/renderer	2.745s
ok  	github.com/pyrex41/cross-validate-/pkg/report	3.334s
ok  	github.com/pyrex41/cross-validate-/pkg/schemas	1.501s
ok  	github.com/pyrex41/cross-validate-/pkg/snapshot	2.920s
ok  	github.com/pyrex41/cross-validate-/pkg/trajectory	3.200s
?   	github.com/pyrex41/cross-validate-/pkg/types	[no test files]
```

### `make lint 2>&1 | tail -60`

```
go vet ./...
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
pkg/audit/proof.go
pkg/ir/builder.go
pkg/ir/trajectory_extract_test.go
pkg/report/reporter.go
pkg/snapshot/snapshot_test.go
pkg/trajectory/trajectory_test.go
make: *** [lint] Error 1
```

**No new lint regressions introduced by S4.** Every file in the list was pre-existing in the baseline:

- `internal/shenfull/*`: generated code, baseline failure per handoff.
- `pkg/audit/proof.go`, `pkg/ir/trajectory_extract_test.go`, `pkg/report/reporter.go`, `pkg/snapshot/snapshot_test.go`, `pkg/trajectory/trajectory_test.go`: pre-existing-unmodified files, baseline failure per handoff.
- `pkg/ir/builder.go`: listed here, but the gofmt diff is a static-map alignment mismatch on a set of keys introduced pre-S4. Verified by running `gofmt -d` against HEAD~7's copy of the file — the same diff appears. My edits themselves are gofmt-clean: `gofmt -d pkg/checker/bridge.go pkg/loader/loader.go cmd/xpc/main.go pkg/types/types.go pkg/renderer/*.go pkg/checker/check_test.go` prints nothing.

### `go test ./pkg/renderer/... -count=2 -v 2>&1 | tail -40`

```
=== RUN   TestKeyDeterminism
--- PASS: TestKeyDeterminism (0.00s)
=== RUN   TestHashChartDirDeterministic
--- PASS: TestHashChartDirDeterministic (0.00s)
=== RUN   TestCachePutGetRoundtrip
--- PASS: TestCachePutGetRoundtrip (0.00s)
=== RUN   TestProbeAbsentHelm
--- PASS: TestProbeAbsentHelm (0.00s)
=== RUN   TestHelmHappyPath
--- PASS: TestHelmHappyPath (0.44s)
=== RUN   TestValidateValuesWrongType
--- PASS: TestValidateValuesWrongType (0.00s)
=== RUN   TestKeyDeterminism
--- PASS: TestKeyDeterminism (0.00s)
=== RUN   TestHashChartDirDeterministic
--- PASS: TestHashChartDirDeterministic (0.00s)
=== RUN   TestCachePutGetRoundtrip
--- PASS: TestCachePutGetRoundtrip (0.00s)
=== RUN   TestProbeAbsentHelm
--- PASS: TestProbeAbsentHelm (0.00s)
=== RUN   TestHelmHappyPath
--- PASS: TestHelmHappyPath (0.19s)
=== RUN   TestValidateValuesWrongType
--- PASS: TestValidateValuesWrongType (0.00s)
PASS
ok  	github.com/pyrex41/cross-validate-/pkg/renderer	1.222s
```

### `go run ./cmd/xpc check --helm-bin=$(which helm) testdata/fixtures/helm-render-ok 2>&1 | tail -20`

```
xpc: ok (no issues)
```

### `go run ./cmd/xpc check --helm-bin=$(which helm) testdata/fixtures/helm-render-fail 2>&1 | tail -20`

```
XPC.H.helm-renders testdata/fixtures/helm-render-fail/app.yaml:1
  rule:     helm-render-fail: helm template failed
  severity: error
  problem:  testdata/fixtures/helm-render-fail: helm template testdata/fixtures/helm-render-fail failed: exit status 1: Error: parse error at (helm-render-fail/templates/broken.yaml:7): unclosed action started at helm-render-fail/templates/broken.yaml:6 Use --debug flag to render out invalid YAML
  fix:      Run 'helm template' locally on the chart to reproduce and fix the template error.
  docs:     https://xpc.dev/errors/XPC.H.helm-renders

xpc: 1 error(s), 0 warning(s)
exit status 1
```

### `go run ./cmd/xpc check --helm-bin=$(which helm) testdata/fixtures/helm-values-mismatch 2>&1 | tail -20`

```
XPC.H.helm-renders testdata/fixtures/helm-values-mismatch/app.yaml:1
  rule:     helm-values-mismatch: helm template failed
  severity: error
  problem:  testdata/fixtures/helm-values-mismatch: helm template testdata/fixtures/helm-values-mismatch failed: exit status 1: Error: values don't meet the specifications of the schema(s) in the following chart(s): helm-values-mismatch: - at '/replicas': got string, want integer
  fix:      Run 'helm template' locally on the chart to reproduce and fix the template error.
  docs:     https://xpc.dev/errors/XPC.H.helm-renders

XPC.H.values-well-typed testdata/fixtures/helm-values-mismatch/app.yaml:1
  rule:     helm-values-mismatch: values.replicas violates values.schema.json
  severity: error
  problem:  field "replicas" has type string, want integer
  fix:      Correct the value to match the chart's values.schema.json, or relax the schema.
  docs:     https://xpc.dev/errors/XPC.H.values-well-typed

xpc: 2 error(s), 0 warning(s)
exit status 1
```

Two errors — R18 fires too because helm v4.1.4 enforces `values.schema.json` during `helm template` and exits non-zero. The briefing flagged this ambiguity; we assert both fire in `TestR18_HelmRenders/helm-values-mismatch`. If a future helm release silences schema enforcement during `template`, that test will regress and the assertion will need to flip.

### `go run ./cmd/xpc check --skip-render testdata/fixtures/helm-render-ok 2>&1 | tail -20`

```
XPC.H.helm-renders testdata/fixtures/helm-render-ok/app.yaml:1:1
  rule:     helm-render-ok: render skipped (--skip-render set)
  severity: info
  docs:     https://xpc.dev/errors/XPC.H.helm-renders

xpc: 0 error(s), 0 warning(s)
```

Exit 0; one info diagnostic per Helm-source-bearing Application as specified.

### `go run ./cmd/xpc check testdata/fixtures/basic 2>&1 | tail -20`

```
xpc: ok (no issues)
```

Regression smoke test: existing fixture still green.

## Known limitations / caveats

1. **`TestR18_HelmRenders/helm-values-mismatch` asserts R18 fires** because helm v4.1.4 enforces `values.schema.json` during `helm template`. The briefing explicitly flagged this as acceptable. If the host's helm changes behavior, the assertion needs to flip.
2. **Loader skips `templates/`**: the filesystem walker now avoids any `templates/` subdir of a chart dir (detected by sibling `Chart.yaml`). This is necessary so raw `{{ }}` YAML doesn't reach the decoder. Future fixtures that happen to use `templates/` for non-Helm content will be affected — none of the current fixtures do.
3. **Render bypass for non-file Argo sources**: `ResolveChart` returns `ErrRendererUnsupported` when `src.Path` is empty (e.g. pure remote-repo Helm chart sources). The builder records a `RenderResult{Success:false, ErrorKind:"other"}` in that case, which R18 reports as an error. That's probably the right default for fg-manifold (all charts are co-located), but will need re-think if remote charts land in a fixture.
4. **Builder.SkipRender changes default test hermeticity**: the existing `loadFixture` helper now sets `SkipRender = true` so tests that don't care about rendering stay fast and deterministic. A test that needs rendering must call `loadFixtureWithHelm` explicitly.
5. **Success discriminator is a symbol, not a Shen boolean**: the bridge emits `render-ok` / `render-failed` rather than `true` / `false`. Shen's boolean literals are interpreted during pattern-match and didn't reliably match — symbol-compare is stable. Same approach used elsewhere in the codebase.
6. **No pre-existing lint regression introduced**: every entry in the `gofmt -l` baseline predates S4. My added files (`pkg/renderer/*`, new test file) are gofmt-clean.
7. **Cache hit-rate manual verification deferred**: the >95% hit-rate criterion from the base plan is a manual check against a real fg-manifold tree; not scripted here. The cache-determinism unit test proves the key computation is stable, which is the prerequisite.
