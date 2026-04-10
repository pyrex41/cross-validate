---
name: xpc-commit
description: Use this skill before committing Crossplane changes and opening
  an MR. Performs a more thorough check than xpc-edit, including snapshot
  refresh and proof generation, so the resulting MR has audit evidence
  attached.
---

# Workflow

1. Run `xpc snapshot --output=.xpcsnap` to capture the current state of the
   user's cluster environment.
2. Run `xpc check --proof --snapshot=.xpcsnap` to verify well-typedness and
   emit a signed proof.
3. If errors are present at severity `error`, do not commit. Fix them first
   using xpc-edit and try again.
4. If only warnings are present, summarize them in the commit message under
   "xpc warnings:".
5. Commit the manifest changes. The proof is uploaded automatically by the
   xpc binary; do not commit the .xpcproof file unless the user has asked
   for proofs to live in the repo (regulated mode).

# Proof output

After a successful check with `--proof`, the proof file is written to
`check.xpcproof`. The proof contains:
- A Merkle tree of all type-checking judgments
- Content-addressed digests of the IR and snapshot
- Kernel and ruleset versions for reproducibility

# Regulated mode

If the user wants proofs committed to the repo (for SOC 2, audit trails, etc.),
commit the `.xpcproof` file alongside the manifests. Otherwise, the proof is
ephemeral and only used for MR comments.
