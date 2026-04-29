#!/usr/bin/env bash
# tools/xpc-capture-cluster.sh
#
# Dumps live cluster resources into a directory layout consumable by
# `xpc plan --base=<dir>` or wrappable via xpc-capture-cluster-snap.sh.
#
# Runtime requirements: kubectl >= 1.24, jq >= 1.6.
set -euo pipefail
trap 'echo "error: command failed at line $LINENO" >&2' ERR

usage() {
  cat <<'EOF'
Usage: tools/xpc-capture-cluster.sh [options] <output-dir>

Dumps live cluster resources into a directory layout consumable by
`xpc plan --base=<dir>` or wrappable via xpc-capture-cluster-snap.sh.

Options:
  --providers=<csv>        CRD group patterns (suffix match) to dump.
                           Default: aws.upbound.io,gcp.upbound.io,
                                    azure.upbound.io,crossplane.io
  --skip-namespaces=<csv>  Skip resources in these namespaces.
                           Default: kube-system,kube-public,kube-node-lease
                           (No-op for v1; documented for forward compat.)
  --include-argo           Capture ArgoApplications/AppSets/AppProjects (default).
  --no-argo                Skip Argo objects.
  --kubeconfig=<path>      Override KUBECONFIG (otherwise inherits env).
  --dry-run                Print kubectl invocations; do not write files.
  --quiet                  Suppress per-kind progress messages on stderr.
  -h, --help               Show usage and exit.

Output layout (relative to <output-dir>):
  argo/applications.yaml
  argo/applicationsets.yaml
  argo/appprojects.yaml
  crossplane/<group>/<kind>.yaml

Exit codes:
  0  success
  1  dependency missing (kubectl, jq) or kubectl auth failure
  2  bad flags

Runtime requirements: kubectl >= 1.24, jq >= 1.6.
EOF
}

# Defaults
providers="aws.upbound.io,gcp.upbound.io,azure.upbound.io,crossplane.io"
skip_namespaces="kube-system,kube-public,kube-node-lease"  # accepted; no-op in v1
include_argo=true
kubeconfig_override=""
dry_run=false
quiet=false
output_dir=""

# `skip_namespaces` is currently unused at runtime but is retained so that
# CI templates can pass --skip-namespaces= today without breaking when real
# filtering lands.
: "${skip_namespaces}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --providers=*)        providers="${1#*=}" ;;
    --skip-namespaces=*)  skip_namespaces="${1#*=}" ;;
    --include-argo)       include_argo=true ;;
    --no-argo)            include_argo=false ;;
    --kubeconfig=*)       kubeconfig_override="${1#*=}" ;;
    --dry-run)            dry_run=true ;;
    --quiet)              quiet=true ;;
    -h|--help)            usage; exit 0 ;;
    -*)                   echo "error: unknown flag: $1" >&2; exit 2 ;;
    *)
      if [[ -n "$output_dir" ]]; then
        echo "error: multiple positional args; expected exactly one <output-dir>" >&2
        exit 2
      fi
      output_dir="$1"
      ;;
  esac
  shift
done

if [[ -z "$output_dir" ]]; then
  echo "error: missing <output-dir>; see --help" >&2
  exit 2
fi

if [[ -n "$kubeconfig_override" ]]; then
  export KUBECONFIG="$kubeconfig_override"
fi

# Dependency probes
command -v kubectl >/dev/null || { echo "error: kubectl not on PATH" >&2; exit 1; }
command -v jq >/dev/null      || { echo "error: jq not on PATH" >&2; exit 1; }

# Auth probe — skip in dry-run so offline smokes work.
if ! $dry_run; then
  kubectl auth can-i get crd >/dev/null 2>&1 \
    || { echo "error: kubectl unauthenticated; check KUBECONFIG / context" >&2; exit 1; }
fi

log() { $quiet || echo "$@" >&2; }
count_items() { grep -c '^kind:' "$1" 2>/dev/null || true; }

# 1. Argo dump
if $include_argo; then
  for short in applications applicationsets appprojects; do
    crd="${short}.argoproj.io"
    out_file="$output_dir/argo/${short}.yaml"
    if $dry_run; then
      echo "[dry-run] kubectl get $crd -A -o yaml > $out_file"
    else
      mkdir -p "$output_dir/argo"
      if kubectl get "$crd" -A -o yaml > "$out_file" 2>/dev/null; then
        log "dump: $crd ($(count_items "$out_file") items)"
      else
        rm -f "$out_file"
        log "skipped: $crd (CRD not installed?)"
      fi
    fi
  done
fi

# 2. Crossplane MR discovery.
# Build a regex from the providers CSV. Each pattern is suffix-matched
# against the CRD spec.group with a strict anchor: (^|\.)pattern$ so
# `aws.upbound.io` matches `aws.upbound.io` AND `rds.aws.upbound.io`
# but NOT `foo-aws.upbound.io`.
build_regex() {
  local IFS=','
  local pats=()
  read -ra pats <<< "$providers"
  local parts=()
  local p_esc
  for p in "${pats[@]}"; do
    p_esc=$(printf '%s' "$p" | sed 's/\./\\./g')
    parts+=("(^|\\.)${p_esc}\$")
  done
  ( IFS='|'; echo "${parts[*]}" )
}

regex=$(build_regex)

if $dry_run; then
  echo "[dry-run] kubectl get crd -o json | jq -r 'group/plural/kind tuples matching regex: $regex'"
  echo "[dry-run]   for each matched (group, plural, kind):"
  echo "[dry-run]     kubectl get <plural>.<group> -A -o yaml > $output_dir/crossplane/<group>/<kind>.yaml"
else
  mapfile -t crd_lines < <(
    kubectl get crd -o json \
      | jq -r --arg pat "$regex" \
          '.items[] | select(.spec.group | test($pat)) | "\(.spec.group)|\(.spec.names.plural)|\(.spec.names.kind)"'
  )
  for line in "${crd_lines[@]}"; do
    [[ -z "$line" ]] && continue
    IFS='|' read -r group plural kind <<< "$line"
    out_subdir="$output_dir/crossplane/$group"
    out_file="$out_subdir/${kind}.yaml"
    mkdir -p "$out_subdir"
    if kubectl get "${plural}.${group}" -A -o yaml > "$out_file" 2>/dev/null; then
      log "dump: ${plural}.${group} ($(count_items "$out_file") items)"
    else
      rm -f "$out_file"
      log "skipped: ${plural}.${group}"
    fi
  done
fi

log "done: capture written to $output_dir"
