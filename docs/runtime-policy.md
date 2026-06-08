# Runtime policy: xpcd

`xpcd` is the always-on cluster companion to the static `xpc` analyzer. Where
`xpc` runs offline at lint/CI time over a directory of YAML, `xpcd` runs the
**same Shen kernel** — restricted to the runtime-decidable subset — inside the
cluster. It has two complementary modes:

- **`xpcd serve`** — a Kubernetes `ValidatingWebhook` that decides on each live
  object **at admission time** (the original mode; gates one object as it
  arrives).
- **`xpcd watch`** — a periodic **controller** that captures the whole live
  cluster (via `kubectl`, the same path as `xpc snapshot`) every interval and
  evaluates the **ambient** decidable subset over everything already running.
  It is observe-only (never mutates the cluster) and continuously sweeps state
  that admission cannot reach. See [Controller mode](#controller-mode-xpcd-watch).

Both modes feed the same JSONL decision-event stream and the same Prometheus
`/metrics` surface; events are tagged by `source` (`admission` vs
`controller`).

It does not replace `xpc`-in-CI. CI catches violations pre-merge; `xpcd`
catches whatever reaches the cluster by any path (out-of-band `kubectl`, a repo
that does not run `xpc`, a controller write) and makes runtime admission
behavior observable. This is the two-layer defense [INC-6](inc-6.md) calls for:
a static floor in CI plus a runtime floor at admission. The design rationale is
[ADR-005](adr/005-runtime-decidable-subset.md).

## What it is

- A single binary, `cmd/xpcd`, run as `xpcd serve` (admission webhook) or
  `xpcd watch` (controller sweep).
- The webhook is a plain `net/http` server speaking `AdmissionReview` v1 JSON.
  No client-go in the admission path.
- Evaluates the **runtime-decidable subset** of `kernel/*.shen` via the same
  `RuleAllowlist` mechanism `xpc check --focus=inc6-floor` uses. The default
  subset is the INC-6 floor (R23/R24/R25) plus the self-contained rules R22,
  R29, R31, R33. See [ADR-005](adr/005-runtime-decidable-subset.md) for the
  formal property and the full tier table.
- Two modes: **audit** (log-only, never blocks — the rollout default) and
  **enforce** (deny non-compliant objects).
- Emits structured JSONL decision events (for ClickHouse / log shipping) and
  Prometheus metrics.

## Architecture

Two modes run in the same namespace and feed one event/metrics pipeline. The
**admission webhook** gates a single object as it arrives; the **controller**
periodically captures and sweeps everything already live.

```
                              xpc-system namespace
  ┌──────────────┐        ┌─────────────────────────────────────────────────┐
  │ kube-apiserver│ admit  │  xpcd serve  (Deployment, 2 replicas)           │
  │  CREATE /     │──────► │  :8443/admit  (TLS, AdmissionReview v1)          │
  │  UPDATE /     │ JSON   │        │                                        │
  │  DELETE       │ ◄──────│        ▼                                        │
  └──────────────┘ allow/  │  runtime-decidable subset (Shen kernel)         │
                   deny    │  R23 R24 R25 · R22 R29 R31 R33                  │
                          │        │  source:"admission"                    │
                          │        │                                        │
  ┌──────────────┐ kubectl│  ┌─────┴───────────────────────────────────┐    │
  │ kube-apiserver│  get   │  │ xpcd watch  (Deployment, 1 replica)     │    │
  │  whole live   │◄───────│  │ every --interval: capture cluster →     │    │
  │  cluster      │──yaml─►│  │ run AMBIENT subset over captured world  │    │
  └──────────────┘        │  │ (single-object subset + selector /      │    │
                          │  │  late-init ignore-diff ambient rules)   │    │
                          │  │ observe-only · source:"controller"      │    │
                          │  └─────┬───────────────────────────────────┘    │
                          │        │                                        │
                          │        ├──► JSONL decision events ──► ClickHouse │
                          │        └──► /metrics (:9090) ────────► Prometheus│
                          │  :9090/healthz  :9090/readyz                    │
                          └─────────────────────────────────────────────────┘

  admission audit  : evaluate → emit event + metrics → ALWAYS admit
  admission enforce: evaluate → emit event + metrics → deny on error-severity
  failurePolicy: Ignore (both admission modes) — a down/slow xpcd never wedges
                 the cluster.
  controller       : observe-only — emits only violations (would-deny / warn),
                     never mutates the cluster, has no admission failure mode.
```

The webhook only matches the groups the subset acts on: Crossplane managed
resources (`*.aws.upbound.io`, `*.gcp.upbound.io`, `*.azure.upbound.io`,
`apiextensions.crossplane.io`) and Argo CD `argoproj.io` Applications /
ApplicationSets, on CREATE+UPDATE+DELETE. `kube-system` and `xpc-system` are
excluded via `namespaceSelector`.

## Modes and the audit-first rollout path

`xpcd` is built to be turned on safely. Always roll out through audit first:

1. **Install in audit mode** (the base; `--mode=audit`). Nothing is ever denied.
   Every admission still produces a decision event and updates metrics.
2. **Watch the audit stream.** Query the JSONL events / Prometheus counters for
   `decision="would-deny"` (audit-mode events record what *would* have been
   denied). Confirm the only hits are genuine violations, not false positives.
3. **Fix or bypass the real hits.** For genuinely-intended destruction, the same
   bypass annotations `xpc` honors apply (`xpc.io/allow-delete: "true"`, alias
   `policy.facilitygrid.io/allow-delete: "true"` — see [inc-6.md](inc-6.md)).
4. **Flip to enforce** once the stream is clean (below). Even then,
   `failurePolicy: Ignore` keeps the cluster fail-open if `xpcd` is unavailable.

## Controller mode (`xpcd watch`)

`xpcd watch` is the second xpcd mode: a periodic, observe-only **controller**
that sweeps the whole cluster on an interval. It is the runtime counterpart to
`xpc check` (offline/CI) and `xpcd serve` (admission). Admission gates a single
object as it *arrives*; the controller continuously sweeps everything that is
*already live*, including objects that were applied before xpcd existed, written
out-of-band, or admitted while the webhook was fail-open.

### What it does

Each `--interval` (default `60s`) the controller:

1. **Captures the live cluster** via `kubectl get ... -o yaml` — the exact same
   path `xpc snapshot` uses (`pkg/clustersrc`). It lists the fixed Crossplane /
   Argo kinds, **discovers managed-resource CRDs dynamically** from the cluster
   CRD list (the `*.aws.upbound.io` / `*.sql.crossplane.io` /
   `external-secrets.io` provider groups), and lists each one cluster-wide.
2. **Builds one captured world** from the parsed objects (render is skipped —
   cluster objects are already concrete; ApplicationSet expansion is skipped —
   the cluster already contains the Applications Argo materialized).
3. **Runs the same Shen kernel** over that world, restricted to the **ambient
   decidable subset**.
4. **Emits one structured event per resource verdict** with `source:"controller"`,
   and updates metrics.

Because it captures the *whole* world before evaluating, every referenced object
is present — which is exactly what lets the ambient-tier rules run soundly here
when they could not at admission. It **never mutates the cluster**: it only
reports on how Argo CD / Crossplane resources are actually configured at runtime.

### The ambient subset it unlocks (vs admission)

The webhook runs the **single-object** subset: rules whose verdict depends only
on the object under review (R23/R24/R25 + R22/R29/R31/R33). The controller runs
that **same single-object subset, PLUS the ambient-tier rules, PLUS the live
tier (R32)** — `ControllerSubset()`. The ambient rules (selector / late-init
ignore-diff) need the cluster type-environment to resolve, and could **not** run
at admission time on a single object (their referents are not in the
AdmissionReview payload). See
[ADR-005 §"Controller sweep (ambient tier)"](adr/005-runtime-decidable-subset.md#5-controller-sweep-ambient-tier)
for why the whole-cluster capture is what makes them sound.

**R32 (observed-desired fixed-point) is the live tier the controller unlocks.**
It diffs `spec.forProvider` against the live `status.atProvider` to fingerprint a
reconcile storm — desired values the provider never echoes back. That needs the
observed `status`, which the admission payload lacks and CI has no cluster for,
so only the controller's capture can evaluate it. A divergence on a
registry-known field is an `error` (a single snapshot is conclusive); an
unknown-field divergence is a `warn` to confirm against a later sweep.

### Flags

`xpcd watch` (matches `deploy/runtime/controller-deployment.yaml`):

| Flag / env | Default | Purpose |
|------------|---------|---------|
| `--interval` | `60s` | Time between reconcile sweeps. |
| `--cluster` | `default` | Cluster-name label stamped on every event (the `cluster` field). Set per environment, e.g. `prod`. |
| `--kube-context` | *(empty)* | Passed to `kubectl --context`. Empty = kubeconfig current-context (in-cluster SA). |
| `--metrics-addr` | `:9090` | `/metrics`, `/healthz`, `/readyz` listener. |
| `--clickhouse-url` / `XPCD_CLICKHOUSE_URL` (env) | unset | Optional ClickHouse sink for JSONL events. If unset, events go to stdout as JSONL. |
| `--kernel-path` | *(embedded)* | Override the embedded Shen kernel (debug). |
| `--once` | `false` | Run exactly one reconcile sweep and exit. For CronJob / debug (below). |

Endpoints are the metrics trio only — there is **no** `/admit` and **no** TLS
listener (the controller has no webhook server):

| Endpoint | Port | Purpose |
|----------|------|---------|
| `/metrics` | 9090 | Prometheus exposition. |
| `/healthz` | 9090 | Liveness. |
| `/readyz` | 9090 | Readiness. |

### Image requirement: kubectl

The controller image **must have a `kubectl` binary on `PATH`** — `xpcd watch`
captures the cluster by shelling out to `kubectl` (`pkg/clustersrc`). The
admission `serve` path needs no kubectl. The simplest approach is a single image
variant that bundles kubectl alongside xpcd (add a kubectl stage to
`cmd/xpcd/Dockerfile` so `ghcr.io/pyrex41/xpcd:latest` already contains it); an
initContainer that copies kubectl into a shared `PATH` `emptyDir` also works.
See the header comment in `deploy/runtime/controller-deployment.yaml`.

### Events and metrics

Controller events use the **identical JSONL schema** as the webhook, with:

- `source`: `"controller"`.
- `operation`: `""` (empty — a sweep is not an admission operation).
- `decision` ∈ {`would-deny` (an error-severity rule fired), `warn` (a
  warning-severity rule fired)}. The controller emits **only violations** — it
  does **not** emit `allow` — to keep the stream signal-rich. (The webhook, by
  contrast, emits `allow`/`deny`/`would-deny` for every admission.)
- `cluster`: the `--cluster` label, present on every controller event.

Controller-specific Prometheus metrics (in addition to the shared
`xpcd_decisions_total{decision,mode,kind}` counter and `xpcd_eval_seconds`
histogram, which the controller also updates):

| Metric | Type | Meaning |
|--------|------|---------|
| `xpcd_controller_runs_total` | counter | Reconcile sweeps completed. |
| `xpcd_controller_resources_scanned` | gauge | Resources captured + evaluated in the last sweep. |
| `xpcd_controller_violations` | gauge | Violations (would-deny + warn) found in the last sweep. |
| `xpcd_controller_last_run_unixtime` | gauge | Unix timestamp of the last completed sweep (alert on staleness). |

### Install

The controller ships in the base kustomization
(`deploy/runtime/controller-deployment.yaml`), so `kubectl apply -k
deploy/runtime` deploys it (1 replica) alongside the webhook. Set its
`CLUSTER_NAME` env (default `prod` in the manifest) per environment. It reuses
the webhook's `xpcd` ServiceAccount / ClusterRole (read-only `get,list` on the
swept kinds) and the same optional `xpcd-clickhouse` Secret.

The enforce overlay only touches the webhook's `--mode`; the controller is
observe-only and is unaffected by audit-vs-enforce.

### `--once` and the CronJob alternative

For environments that prefer a scheduled sweep over a long-running Deployment,
run `xpcd watch --once`: it performs exactly one reconcile and exits. Wrap it in
a `CronJob` (still using the `xpcd` ServiceAccount and a kubectl-bearing image):

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: xpcd-sweep
  namespace: xpc-system
spec:
  schedule: "*/5 * * * *" # every 5 minutes
  concurrencyPolicy: Forbid # never overlap two sweeps
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: xpcd
          restartPolicy: Never
          containers:
            - name: xpcd-controller
              image: ghcr.io/pyrex41/xpcd:latest # MUST include kubectl on PATH
              args: ["watch", "--once", "--cluster=prod"]
```

`--once` is also the easiest way to debug a single sweep locally:
`xpcd watch --once --kube-context=my-cluster`.

### Example queries (controller events)

ClickHouse — violations by rule code over time, controller only:

```sql
SELECT toStartOfHour(ts) AS hour, code, count() AS n
FROM xpcd_decisions
ARRAY JOIN ruleCodes AS code
WHERE source = 'controller'
  AND ts >= now() - INTERVAL 7 DAY
GROUP BY hour, code
ORDER BY hour, n DESC;
```

ClickHouse — current would-deny violations per namespace/kind from the most
recent sweep:

```sql
SELECT namespace, kind, count() AS violations
FROM xpcd_decisions
WHERE source = 'controller' AND decision = 'would-deny'
  AND ts >= now() - INTERVAL 10 MINUTE
GROUP BY namespace, kind
ORDER BY violations DESC;
```

Prometheus — violations found in the last controller sweep:

```promql
xpcd_controller_violations
```

Prometheus — alert if the controller has not completed a sweep recently (stale
or wedged loop):

```promql
time() - xpcd_controller_last_run_unixtime > 600
```

Prometheus — controller-attributed verdicts by kind (the shared counter is also
updated by the controller; filter via its `mode`/`kind` labels and join against
the controller run rate):

```promql
sum by (kind) (increase(xpcd_decisions_total{decision="would-deny"}[1h]))
```

## Install

Requires [cert-manager](https://cert-manager.io) for the serving cert and
caBundle injection (manual-cert fallback documented in
`deploy/runtime/certificate.yaml`).

```sh
# Build and push the image (built from cmd/xpcd in this repo):
docker build -t ghcr.io/pyrex41/xpcd:latest -f cmd/xpcd/Dockerfile .
docker push ghcr.io/pyrex41/xpcd:latest

# Apply the base — deploys xpcd in audit mode:
kubectl apply -k deploy/runtime
```

This creates the `xpc-system` namespace, the `xpcd` webhook Deployment (2
replicas), the `xpcd-controller` Deployment (1 replica — the sweeper; see
[Controller mode](#controller-mode-xpcd-watch)), the webhook + metrics Services,
a least-privilege read-only ServiceAccount / ClusterRole shared by both, the
cert-manager `Certificate`, and the `ValidatingWebhookConfiguration`.

### Flip to enforce

```sh
kubectl apply -k deploy/runtime/overlays/enforce
```

The overlay changes exactly one flag, `--mode=audit` → `--mode=enforce`.
Everything else — image, ports, RBAC, webhook, serving cert — is the base. To
roll back, re-apply the base.

## Flags and ports

This section covers the admission webhook, `xpcd serve`. For the controller's
flags (`xpcd watch`), see the
[Controller mode flags table](#flags) above.

`xpcd serve` (matches `deploy/runtime/deployment.yaml`):

| Flag / env | Default | Purpose |
|------------|---------|---------|
| `--mode` | `audit` | `audit` (never blocks) or `enforce` (deny on error). |
| `--addr` | `:8443` | TLS admission listener (the `/admit` webhook). |
| `--metrics-addr` | `:9090` | `/metrics`, `/healthz`, `/readyz` listener. |
| `--cert-dir` | `/etc/xpcd/tls` | Directory with `tls.crt` + `tls.key`. |
| `XPCD_CLICKHOUSE_URL` (env) | unset | Optional ClickHouse sink for JSONL events. If unset, events go to stdout as JSONL for log shipping. |

Endpoints:

| Endpoint | Port | Purpose |
|----------|------|---------|
| `/admit` | 8443 (TLS) | AdmissionReview v1 webhook. |
| `/metrics` | 9090 | Prometheus exposition. |
| `/healthz` | 9090 | Liveness. |
| `/readyz` | 9090 | Readiness (cert loaded, kernel initialized). |

## Decision events (JSONL)

Both modes emit the **same** JSONL line — to stdout, and to ClickHouse when
`XPCD_CLICKHOUSE_URL` is set. The webhook emits one per admission; the
controller emits one per violating resource per sweep. Fields:

| Field | Type | Meaning |
|-------|------|---------|
| `ts` | string (RFC3339Nano) | Event timestamp. |
| `decision` | string | `allow`, `deny`, or `would-deny`. Webhook emits all three; controller emits only `would-deny`/`warn` (violations only — no `allow`). |
| `mode` | string | `audit` or `enforce` (admission). Not meaningful for the controller. |
| `group` | string | Object apiGroup. |
| `version` | string | Object apiVersion. |
| `kind` | string | Object kind. |
| `name` | string | Object name. |
| `namespace` | string | Object namespace. |
| `uid` | string | AdmissionReview request UID (admission). |
| `operation` | string | `CREATE`, `UPDATE`, or `DELETE` (admission). `""` for the controller — a sweep is not an admission operation. |
| `ruleCodes` | string[] | Diagnostic codes that fired (e.g. `["XPC.S.crossplane-state-needs-orphan"]`). |
| `errors` | int | Count of error-severity diagnostics. |
| `warnings` | int | Count of warning-severity diagnostics. |
| `evalNanos` | int | Kernel evaluation time, nanoseconds. |
| `message` | string | Human-readable summary (the deny / violation reason). |
| `source` | string | Emitter: `admission` for the webhook, `controller` for the `xpcd watch` sweep. |
| `cluster` | string | The controller's `--cluster` label (empty for admission events). |

Sample admission line:

```json
{"ts":"2026-06-08T14:03:21.118432Z","decision":"would-deny","mode":"audit","group":"rds.aws.upbound.io","version":"v1beta1","kind":"Cluster","name":"aurora-prod-cluster","namespace":"crossplane-system","uid":"7c1f...","operation":"CREATE","ruleCodes":["XPC.S.crossplane-state-needs-orphan"],"errors":1,"warnings":0,"evalNanos":214503,"message":"xpcd would deny in enforce mode: XPC.S.crossplane-state-needs-orphan","source":"admission"}
```

Sample controller line (note `source:"controller"`, empty `operation`, and the
`cluster` label):

```json
{"ts":"2026-06-08T14:05:00.421907Z","decision":"would-deny","mode":"","group":"rds.aws.upbound.io","version":"v1beta1","kind":"Cluster","name":"aurora-prod-cluster","namespace":"crossplane-system","uid":"","operation":"","ruleCodes":["XPC.S.crossplane-state-needs-orphan"],"errors":1,"warnings":0,"evalNanos":198317,"message":"crossplane-state-needs-orphan: deletionPolicy must be Orphan","source":"controller","cluster":"prod"}
```

### ClickHouse table

DDL matching the event shape:

```sql
CREATE TABLE IF NOT EXISTS xpcd_decisions
(
    ts         DateTime64(9, 'UTC'),
    decision   LowCardinality(String),
    mode       LowCardinality(String),
    group      LowCardinality(String),
    version    LowCardinality(String),
    kind       LowCardinality(String),
    name       String,
    namespace  LowCardinality(String),
    uid        String,
    operation  LowCardinality(String),
    ruleCodes  Array(String),
    errors     UInt16,
    warnings   UInt16,
    evalNanos  UInt64,
    message    String,
    source     LowCardinality(String) DEFAULT 'admission',
    cluster    LowCardinality(String) DEFAULT ''
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, namespace, kind)
TTL toDateTime(ts) + INTERVAL 90 DAY;
```

The `source` and `cluster` columns carry defaults so existing admission-only
deployments insert unchanged; controller events populate both.

`XPCD_CLICKHOUSE_URL` points at the HTTP interface, e.g.
`https://clickhouse.internal:8443/?database=policy&query=INSERT%20INTO%20xpcd_decisions%20FORMAT%20JSONEachRow`.
The JSONL field names line up 1:1 with the column names for `FORMAT
JSONEachRow`.

## Metrics

Exposed at `:9090/metrics`:

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `xpcd_decisions_total` | counter | `decision`, `mode`, `kind` | Verdicts (`decision` ∈ `allow`/`deny`/`would-deny`). Updated by both the webhook and the controller. |
| `xpcd_eval_errors_total` | counter | — | Error-severity diagnostics observed across all evaluations. |
| `xpcd_events_dropped_total` | counter | — | Observability events dropped before delivery (sink backpressure). |
| `xpcd_eval_seconds` | histogram | — | Kernel evaluation latency in seconds (`_bucket`/`_sum`/`_count`). Updated by both modes. |

Controller-only metrics (exposed by the `xpcd watch` Deployment's `:9090`; see
[Controller mode](#events-and-metrics)):

| Metric | Type | Meaning |
|--------|------|---------|
| `xpcd_controller_runs_total` | counter | Reconcile sweeps completed. |
| `xpcd_controller_resources_scanned` | gauge | Resources captured + evaluated in the last sweep. |
| `xpcd_controller_violations` | gauge | Violations (would-deny + warn) found in the last sweep. |
| `xpcd_controller_last_run_unixtime` | gauge | Unix timestamp of the last completed sweep. |

Per-rule fire counts are available from the event stream (`ruleCodes`) in
ClickHouse; the Prometheus surface is intentionally low-cardinality.

### Example Prometheus queries

What audit mode *would* block, over the last day:

```promql
sum(increase(xpcd_decisions_total{decision="would-deny",mode="audit"}[1d]))
```

Real denials in enforce mode, per kind, last hour:

```promql
sum by (kind) (increase(xpcd_decisions_total{decision="deny",mode="enforce"}[1h]))
```

p99 kernel evaluation latency:

```promql
histogram_quantile(0.99, sum by (le) (rate(xpcd_eval_seconds_bucket[5m])))
```

Event-sink backpressure (dropped decision events):

```promql
sum(rate(xpcd_events_dropped_total[5m]))
```

## Relationship to `xpc` in CI

`xpcd` has two runtime modes; both share the kernel with `xpc`-in-CI:

| | `xpc` (CI) | `xpcd serve` (admission) | `xpcd watch` (controller) |
|--|-----------|--------------------------|---------------------------|
| When | pre-merge, lint/CI | admission time, always-on | every `--interval`, always-on |
| Input | a directory of YAML / two Worlds | one live AdmissionReview object | the whole live cluster (captured via kubectl) |
| Rules | all 30+ across 14 categories | single-object subset (R23/R24/R25 + R22/R29/R31/R33) | single-object subset **+ ambient tier** (selector / late-init ignore-diff) |
| Context | full: cross-repo joins, trajectory simulation, two-Worlds diffs | single object (+ cached cluster types) | the full captured world (every referenced object present) |
| Mutates? | no | gates admission (audit: no; enforce: denies) | **no — observe-only** |
| Failure mode | blocks the MR | audit: logs; enforce: denies — fail-open if down | logs violations; never blocks anything |

All three run the same `kernel/*.shen`. A change to a rule's logic in `kernel/`
changes every tool with no second edit. Both xpcd modes deliberately run a
**subset** of the kernel: the reference-resolution (B), trajectory (F),
cross-Application (G), rendering (H), and plan-mode (`XPC.P.*`) rules stay in
`xpc`-in-CI, where the two-Worlds and cross-repo context they need actually
exists. The controller widens the runtime subset to the **ambient tier** and the
**live tier** — sound because it captures the whole world (including observed
`status`) before evaluating — so R32 (observed-vs-desired), which no static or
admission path can reach, runs there. See
[ADR-005](adr/005-runtime-decidable-subset.md).
