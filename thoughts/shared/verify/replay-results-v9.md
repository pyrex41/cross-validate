---
title: fg-manifold replay v9 — xpc.yaml alias gate
date: 2026-04-27
author: Reuben / GPT-5.5
binary: /tmp/xpc-replay/xpc-v9 built from claude/build-xpc-type-checker-TfgsT @ 36739fe plus uncommitted xpc.yaml changes
binary_sha256: d95af0f9456439f2b1255aa4292eb89f7741a827055f1b59d87d3cecc2b2afb4
predecessor: replay-results-v8.md
---

## TL;DR

Replay v9 clears the chunk-B' gate. A root `xpc.yaml` was added to the
local fg-manifold checkout with:

```yaml
version: 1
bypass-annotations:
  allow-delete:
    aliases:
      - "policy.facilitygrid.io/allow-delete"
```

With `xpc check` run from `/Users/reuben/fg/fg-manifold`, single-tip counts
are line-for-line identical to replay v8. R23 stays `69 / 69 / 69 / 69 / 24`
instead of rising, so the new config path preserves the FG alias. Plan-mode
was also run with `--config=/Users/reuben/fg/fg-manifold/xpc.yaml`; all three
pairs exit 0 with empty stderr and zero R26/R27 plan diagnostics.

Important caveat: current fg-manifold tips contain no
`policy.facilitygrid.io/allow-delete` annotations outside the new `xpc.yaml`
itself, so this replay is a sanity check that the config plumbing is stable,
not a migration-list generator.

## Setup

- cross-validate: `claude/build-xpc-type-checker-TfgsT @ 36739fe` with the
  uncommitted chunk A + B' working tree.
- fg-manifold: `/Users/reuben/fg/fg-manifold @ 44698ba640` on
  `feat/e2e-otel-enable-autoload`, already dirty before this session.
- New fg-manifold file: `/Users/reuben/fg/fg-manifold/xpc.yaml`.
- Binary: `/tmp/xpc-replay/xpc-v9`.
- Kernel fallback: `/tmp/xpc-replay/kernel -> /Users/reuben/projects/cross-validate/kernel`.

Validation before replay:

```bash
go test -count=1 ./...
go vet ./...
```

Both passed.

## Single-tip `xpc check`

Executed from `/Users/reuben/fg/fg-manifold` so cwd-upward discovery finds
the new root `xpc.yaml`.

| code                                      | tip-441 | tip-2ca | tip-4dd | tip-main | tip-postrem | v8 delta |
|-------------------------------------------|:-------:|:-------:|:-------:|:--------:|:-----------:|:--------:|
| XPC.D.kind-whitelisted                    |   700   |   700   |   700   |    701   |     711     |    =     |
| XPC.E.appset-finalizer-without-preserve   |    23   |    23   |    23   |     23   |      23     |    =     |
| XPC.E.late-init-needs-ignore-diff         |    12   |    12   |    12   |     12   |      12     |    =     |
| XPC.E.prod-appset-autosync                |     2   |     2   |     2   |      2   |       0     |    =     |
| XPC.E.selector-needs-ignore-diff          |   435   |   435   |   435   |    440   |     440     |    =     |
| XPC.H.appset-unsupported-generator        |     7   |     7   |     7   |      7   |       7     |    =     |
| XPC.H.composition-renders                 |    10   |    10   |    10   |     10   |      10     |    =     |
| XPC.H.helm-renders                        |    34   |    34   |    34   |     35   |      35     |    =     |
| XPC.S.crossplane-state-needs-orphan       |    69   |    69   |    69   |     69   |      24     |    =     |
| XPC006                                    |    30   |    30   |    30   |     30   |      30     |    =     |
| XPC012                                    |    12   |    12   |    12   |     12   |      12     |    =     |
| **total diagnostics**                     |  1334   |  1334   |  1334   |   1341   |    1304     |    =     |

Raw outputs:

- `/tmp/xpc-replay/2026-04-23-phase1/tip-{441,2ca,4dd,main,postrem}-v9.json`
- matching `.stderr` files are empty.

## Plan-mode pairs

Executed from `/Users/reuben/fg/fg-manifold` with explicit config override:

```bash
/tmp/xpc-replay/xpc-v9 plan \
  --config=/Users/reuben/fg/fg-manifold/xpc.yaml \
  --format=json ./deploy/facilitygrid/ops
```

| pair           | base total | head total | delta added/removed/modified | destructive | immutable | stderr |
|----------------|:----------:|:----------:|:----------------------------:|:-----------:|:---------:|:------:|
| 441 → 2ca      |    1290    |    1290    |          0 / 0 / 0           |      0      |     0     | empty  |
| 2ca → 4dd      |    1290    |    1290    |          0 / 0 / 0           |      0      |     0     | empty  |
| main → postrem |    1296    |    1259    |          14 / 0 / 52         |      0      |     0     | empty  |

These match replay v8's plan-mode counts.

Raw outputs:

- `/tmp/xpc-replay/2026-04-23-phase1/plan-{441-2ca,2ca-4dd,main-postrem}-v9.json`
- matching `.stderr` files are empty.

## Decision

Proceed with option (1) from the handoff: land the fg-manifold `xpc.yaml`
before deploying chunk B'. Counts stay flat, and replay v9 becomes the
expected sanity check rather than a migration-list exercise.
