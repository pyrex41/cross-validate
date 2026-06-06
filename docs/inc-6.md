# INC-6: the ArgoCD → Crossplane delete cascade

**INC-6** (fg-synapse, 2026-04-22, SEV-2) is the production incident that
motivates a large part of xpc. This page is the short, self-contained summary;
it replaces links to the internal postmortem.

## What happened

An ArgoCD **ApplicationSet** baked the cascading finalizer
`resources-finalizer.argocd.argoproj.io` into every generated Application's
template, but did **not** set `spec.syncPolicy.preserveResourcesOnDeletion: true`
on the AppSet itself.

When the change landed, ArgoCD cascaded a delete through an AppSet-owned
Application and reached roughly **70 Crossplane managed resources**. Because
state-bearing managed resources default to `deletionPolicy: Delete`, that
cascade turns into real destructive cloud calls — `DROP DATABASE`,
`DeleteDBCluster`, `DeleteVolume`, KMS key deletion, bucket deletion. The
data survived only by ordering luck.

The same cascade does **not** require an out-of-band delete to fire: any commit
that shrinks a generator's output (e.g. a filter change that drops a cluster)
removes the generated Applications and triggers the same path — and with
`syncPolicy.automated` on a prod AppSet there is no human gate in front of it.

## Why the two systems collide

ArgoCD and Crossplane don't share a model of *who owns a field or a lifecycle*.
ArgoCD treats the git manifest as the source of truth and will delete what is no
longer declared; Crossplane treats the managed resource as a live handle to an
external object. Without `deletionPolicy: Orphan`, deleting the CR deletes the
external object. INC-6 is what that gap looks like at scale.

## The failure ingredients

| Ingredient | Effect |
|---|---|
| AppSet bakes the cascading finalizer into the template, no `preserveResourcesOnDeletion` | a generator shrink or AppSet delete cascades to every owned resource |
| State-bearing managed resources default to `deletionPolicy: Delete` | the cascade issues real destructive cloud API calls |
| Prod AppSet with `syncPolicy.automated` | a destructive git change applies with no human gate |

## How xpc encodes it

xpc turns INC-6 into a **static floor** that runs in CI across every
environment — the static-analysis analog of fg-manifold's runtime
`crossplane-state-require-orphan` ValidatingAdmissionPolicy:

- **R23 — `XPC.S.crossplane-state-needs-orphan`**: every state-bearing managed
  resource (Aurora, DocDB, MySQL Database/User/Grant, KMS Key, S3 Bucket, EC2
  VPC, …) must set `deletionPolicy: Orphan`. *Limits blast radius.*
- **R24 — `XPC.E.appset-finalizer-without-preserve`**: an AppSet that bakes the
  cascading finalizer must set `preserveResourcesOnDeletion: true`. *Removes the
  cascade itself.*
- **R25 — `XPC.E.prod-appset-autosync`**: a prod-named AppSet must not enable
  `syncPolicy.automated`. *Limits how easily the cascade can be triggered.*

Run just this floor with `xpc check --focus=inc6-floor`.

The transition itself (a PR that actually deletes a protected resource) is
caught plan-side by `XPC.P.destructive-delete` and `XPC.P.cascade-risk`, which
compare two worlds rather than a single tip.

## Bypass

If destruction is genuinely intended (throwaway test, decommissioning), annotate
the resource:

```yaml
metadata:
  annotations:
    xpc.io/allow-delete: "true"                 # primary
    # policy.facilitygrid.io/allow-delete: "true"  # alias, matches the runtime VAP
```
