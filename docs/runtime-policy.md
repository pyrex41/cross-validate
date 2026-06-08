# Runtime policy: xpcd

`xpcd` is the always-on cluster companion to the static `xpc` analyzer. Where
`xpc` runs offline at lint/CI time over a directory of YAML, `xpcd` runs the
**same Shen kernel** — restricted to the runtime-decidable subset — as a
Kubernetes `ValidatingWebhook`, deciding on each live object at admission time.

It does not replace `xpc`-in-CI. CI catches violations pre-merge; `xpcd`
catches whatever reaches the cluster by any path (out-of-band `kubectl`, a repo
that does not run `xpc`, a controller write) and makes runtime admission
behavior observable. This is the two-layer defense [INC-6](inc-6.md) calls for:
a static floor in CI plus a runtime floor at admission. The design rationale is
[ADR-005](adr/005-runtime-decidable-subset.md).

## What it is

- A single binary, `cmd/xpcd`, run as `xpcd serve`.
- A plain `net/http` server speaking `AdmissionReview` v1 JSON. No client-go in
  the admission path.
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

```
                          xpc-system namespace
  ┌──────────────┐        ┌───────────────────────────────────────────┐
  │ kube-apiserver│ admit  │  xpcd  (xpcd serve)                        │
  │  CREATE /     │──────► │  :8443/admit  (TLS, AdmissionReview v1)    │
  │  UPDATE /     │ JSON   │        │                                   │
  │  DELETE       │ ◄──────│        ▼                                   │
  └──────────────┘ allow/  │  runtime-decidable subset (Shen kernel)   │
                   deny    │  R23 R24 R25 · R22 R29 R31 R33            │
                          │        │                                   │
                          │        ├──► JSONL decision event ──► ClickHouse
                          │        └──► /metrics (:9090) ──────► Prometheus
                          │  :9090/healthz  :9090/readyz             │
                          └───────────────────────────────────────────┘

  audit  : evaluate → emit event + metrics → ALWAYS admit
  enforce: evaluate → emit event + metrics → deny on error-severity diagnostic
  failurePolicy: Ignore (both modes) — a down/slow xpcd never wedges the cluster.
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

This creates the `xpc-system` namespace, the `xpcd` Deployment (2 replicas),
the webhook + metrics Services, a least-privilege read-only ServiceAccount /
ClusterRole, the cert-manager `Certificate`, and the
`ValidatingWebhookConfiguration`.

### Flip to enforce

```sh
kubectl apply -k deploy/runtime/overlays/enforce
```

The overlay changes exactly one flag, `--mode=audit` → `--mode=enforce`.
Everything else — image, ports, RBAC, webhook, serving cert — is the base. To
roll back, re-apply the base.

## Flags and ports

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

Every admission emits one JSONL line — to stdout, and to ClickHouse when
`XPCD_CLICKHOUSE_URL` is set. Fields:

| Field | Type | Meaning |
|-------|------|---------|
| `ts` | string (RFC3339Nano) | Event timestamp. |
| `decision` | string | `allow`, `deny`, or `would-deny` (audit mode, when enforce *would* have denied). |
| `mode` | string | `audit` or `enforce`. |
| `group` | string | Admitted object apiGroup. |
| `version` | string | Admitted object apiVersion. |
| `kind` | string | Admitted object kind. |
| `name` | string | Object name. |
| `namespace` | string | Object namespace. |
| `uid` | string | AdmissionReview request UID. |
| `operation` | string | `CREATE`, `UPDATE`, or `DELETE`. |
| `ruleCodes` | string[] | Diagnostic codes that fired (e.g. `["XPC.S.crossplane-state-needs-orphan"]`). |
| `errors` | int | Count of error-severity diagnostics. |
| `warnings` | int | Count of warning-severity diagnostics. |
| `evalNanos` | int | Kernel evaluation time, nanoseconds. |
| `message` | string | Human-readable summary (the deny reason, when denied). |
| `source` | string | Emitter: `admission` for the webhook (a future controller loop would emit `controller`). |

Sample line:

```json
{"ts":"2026-06-08T14:03:21.118432Z","decision":"would-deny","mode":"audit","group":"rds.aws.upbound.io","version":"v1beta1","kind":"Cluster","name":"aurora-prod-cluster","namespace":"crossplane-system","uid":"7c1f...","operation":"CREATE","ruleCodes":["XPC.S.crossplane-state-needs-orphan"],"errors":1,"warnings":0,"evalNanos":214503,"message":"xpcd would deny in enforce mode: XPC.S.crossplane-state-needs-orphan","source":"admission"}
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
    source     LowCardinality(String) DEFAULT 'admission'
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, namespace, kind)
TTL toDateTime(ts) + INTERVAL 90 DAY;
```

`XPCD_CLICKHOUSE_URL` points at the HTTP interface, e.g.
`https://clickhouse.internal:8443/?database=policy&query=INSERT%20INTO%20xpcd_decisions%20FORMAT%20JSONEachRow`.
The JSONL field names line up 1:1 with the column names for `FORMAT
JSONEachRow`.

## Metrics

Exposed at `:9090/metrics`:

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `xpcd_decisions_total` | counter | `decision`, `mode`, `kind` | Admission verdicts (`decision` ∈ `allow`/`deny`/`would-deny`). |
| `xpcd_eval_errors_total` | counter | — | Error-severity diagnostics observed across all evaluations. |
| `xpcd_events_dropped_total` | counter | — | Observability events dropped before delivery (sink backpressure). |
| `xpcd_eval_seconds` | histogram | — | Kernel evaluation latency in seconds (`_bucket`/`_sum`/`_count`). |

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

| | `xpc` (CI) | `xpcd` (runtime) |
|--|-----------|------------------|
| When | pre-merge, lint/CI | admission time, always-on |
| Input | a directory of YAML / two Worlds | one live AdmissionReview object |
| Rules | all 30+ across 14 categories | runtime-decidable subset (R23/R24/R25 + R22/R29/R31/R33) |
| Context | full: cross-repo joins, trajectory simulation, two-Worlds diffs | single object (+ cached cluster types) |
| Failure mode | blocks the MR | audit: logs; enforce: denies — fail-open if down |

Both run the same `kernel/*.shen`. A change to a rule's logic in `kernel/`
changes both tools with no second edit. `xpcd` deliberately runs the **subset**
that is sound to decide on one object — the reference-resolution (B),
trajectory (F), cross-Application (G), rendering (H), and plan-mode (`XPC.P.*`)
rules stay in `xpc`-in-CI, where the two-Worlds and cross-repo context they need
actually exists. See [ADR-005](adr/005-runtime-decidable-subset.md).
