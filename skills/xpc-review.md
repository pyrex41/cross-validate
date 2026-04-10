---
name: xpc-review
description: Use this skill when a user asks "is this MR ready to merge,"
  "what does this MR change," or "explain this Crossplane change." Fetches
  the MR's xpc proofs (from the MR environment and from prod) and produces
  a plain-English summary of how well-typedness changed.
---

# Workflow

1. Identify the MR's commit SHA.
2. Run `xpc proof show <proof-file>` to view the most recent proofs for that
   commit. There may be two: one against the MR environment, one against
   prod.
3. Run `xpc proof diff <before.xpcproof> <after.xpcproof>` to get the
   structured diff of judgments between main and the MR.
4. Summarize for the user:
     - Number of judgments unchanged, newly satisfied, newly violated.
     - Any rules that flipped state, by name and resource.
     - Any drift between the MR-environment proof and the prod proof
       (this is the bug class where the MR works in staging but breaks
       in prod).
5. If the user asks follow-up questions, use `xpc proof show --rule=<id>`
   to drill into specific judgments.

# Interpreting proof diffs

- **Newly satisfied**: A rule that was violated before now passes. Good news.
- **Newly violated**: A rule that was passing now fails. Needs attention.
- **Unchanged**: No change in judgment. Expected for most rules.
- **MR env vs prod drift**: If the same rule has different results against
  the MR environment and prod, this is the most important finding — it means
  the MR may work in staging but break in production.

# Example output to the user

"This MR changes 3 of 47 type judgments:
- R2 (webhook-conversion): now satisfied — you switched to the storage version
- R5 (patch-typecheck): newly constrains spec.parameters.region → spec.forProvider.region
- R6 (wave-ordering): unchanged

Against prod: R2 is violated because prod still runs provider-aws v2.0
(storage version v1beta1). Recommend upgrading prod before merging."
