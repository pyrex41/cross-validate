---
name: xpc-edit
description: Use this skill whenever editing Crossplane Compositions, XRDs,
  Functions, managed resources, or any YAML in a directory containing .xpcir
  files. Run `xpc` after every meaningful change. Fix all errors before
  responding to the user. Warnings can be acknowledged with the user's consent.
---

# Workflow

1. Make your edit.
2. Run `xpc check` in the working directory. (No flags needed; it autodetects.)
3. If output contains lines starting with `XPC`-prefixed codes at severity
   `error`, fix them and re-run. Do not respond to the user with unfixed
   errors present.
4. For warnings, summarize them in your response and ask the user how to
   proceed.

# When `xpc` reports "snapshot stale" or "no snapshot found"

Run `xpc snapshot` first to refresh against the user's current kubectl context.
This is fast (a few seconds).

# Output format

Each error block has fields: `rule`, `severity`, `problem`,
`source`, `fix`, `ack`, `docs`. Read the `fix` field first; that's usually
what you want to do. The `ack` field is the escape hatch — only use it if
the user explicitly says the bug is intentional.

# Common fixes by error code

- **XPC001**: Fix version coherence — ensure exactly one storage version, all versions served.
- **XPC002**: Change apiVersion to the storage version shown in the `fix` field.
- **XPC003**: Ensure the XRD exists and the referenced version is referenceable.
- **XPC004**: Ensure the Function resource exists and input apiVersion matches.
- **XPC005**: Add a transform to convert types, or change field types.
- **XPC006**: Adjust sync-wave annotations to respect dependency ordering.
- **XPC007**: Switch Argo tracking to annotation mode.
- **XPC008**: Move machinery fields under spec.crossplane for v2 XRDs.
- **XPC009**: Ensure required resources are produced by earlier pipeline steps.
- **XPC010**: Route secrets through SecretRef fields.
- **XPC011**: Migrate to non-deprecated API versions.
