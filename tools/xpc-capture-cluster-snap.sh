#!/usr/bin/env bash
# tools/xpc-capture-cluster-snap.sh
#
# Wraps xpc-capture-cluster.sh and pipes the dump through
# `xpc snapshot --include-resources` to produce a single
# content-addressed .xpcsnap file.
#
# Runtime requirements: kubectl, jq, xpc, mktemp.
set -euo pipefail
trap 'echo "error: command failed at line $LINENO" >&2' ERR

usage() {
  cat <<'EOF'
Usage: tools/xpc-capture-cluster-snap.sh [options] <snap-output-path>

Wraps xpc-capture-cluster.sh and pipes the dump through
`xpc snapshot --include-resources` to produce a single .xpcsnap.

Same options as xpc-capture-cluster.sh, plus:
  --cluster-name=<name>    Cluster name embedded in the snapshot.
                           Default: kubectl config current-context.
  --xpc-bin=<path>         Path to the xpc binary. Default: xpc on PATH.
  -h, --help               Show usage and exit.

Exit codes:
  0  success (digest printed to stdout by xpc snapshot)
  1  dependency missing or kubectl/xpc failure
  2  bad flags
EOF
}

# Flags forwarded verbatim to the capture script.
fwd=()
cluster_name=""
xpc_bin=""
dry_run=false
snap_path=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --providers=*|--skip-namespaces=*|--kubeconfig=*) fwd+=("$1") ;;
    --include-argo|--no-argo|--quiet)                 fwd+=("$1") ;;
    --dry-run)            dry_run=true; fwd+=("$1") ;;
    --cluster-name=*)     cluster_name="${1#*=}" ;;
    --xpc-bin=*)          xpc_bin="${1#*=}" ;;
    -h|--help)            usage; exit 0 ;;
    -*)                   echo "error: unknown flag: $1" >&2; exit 2 ;;
    *)
      if [[ -n "$snap_path" ]]; then
        echo "error: multiple positional args; expected exactly one <snap-output-path>" >&2
        exit 2
      fi
      snap_path="$1"
      ;;
  esac
  shift
done

if [[ -z "$snap_path" ]]; then
  echo "error: missing <snap-output-path>; see --help" >&2
  exit 2
fi

# Resolve xpc binary path.
if [[ -z "$xpc_bin" ]]; then
  xpc_bin="xpc"
fi
if ! $dry_run; then
  command -v "$xpc_bin" >/dev/null \
    || { echo "error: xpc not on PATH (or pass --xpc-bin=<path>)" >&2; exit 1; }
fi

# Resolve cluster name. Skip the kubectl call in --dry-run so offline
# smokes work; print a placeholder instead.
if [[ -z "$cluster_name" ]]; then
  if $dry_run; then
    cluster_name="<from-current-context>"
  else
    command -v kubectl >/dev/null \
      || { echo "error: kubectl not on PATH" >&2; exit 1; }
    cluster_name=$(kubectl config current-context 2>/dev/null) || {
      echo "error: kubectl config current-context failed; pass --cluster-name=<name>" >&2
      exit 1
    }
  fi
fi

# Sibling capture script.
script_dir=$(cd "$(dirname "$0")" && pwd)
capture="$script_dir/xpc-capture-cluster.sh"
if [[ ! -x "$capture" ]]; then
  echo "error: capture script not executable: $capture" >&2
  exit 1
fi

# Temp dir for the intermediate directory dump; cleaned up on exit.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if $dry_run; then
  echo "[dry-run] $capture ${fwd[*]:-} <tmpdir>"
  echo "[dry-run] $xpc_bin snapshot --include-resources --cluster=$cluster_name --output=$snap_path <tmpdir>"
  exit 0
fi

"$capture" ${fwd[@]+"${fwd[@]}"} "$tmp"

"$xpc_bin" snapshot --include-resources \
  --cluster="$cluster_name" \
  --output="$snap_path" \
  "$tmp"
