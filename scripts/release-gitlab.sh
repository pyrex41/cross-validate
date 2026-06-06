#!/usr/bin/env bash
#
# Build the xpc release binaries and publish them as a GitLab release on the
# internal instance (facility-grid/cross-validate). Mirrors the asset layout of
# the old GitHub releases and exposes the stable permalink that the fg-manifold
# flake / CI / Earthfile depend on:
#
#   https://<host>/<project>/-/releases/<tag>/downloads/xpc-linux-amd64
#
# Mechanism: upload each binary to the project's generic Package Registry, then
# create a Release whose asset links set direct_asset_path=/<name> so the
# /downloads/<name> permalink resolves. No buildkit / ECR needed — those are
# only for the xpc-lint *container image*; the xpc binary is a plain release
# asset, which self-hosted GitLab serves natively via Packages + Releases.
#
# Usage:   make release-gitlab          (version is read from cmd/xpc/main.go)
# Auth:    GITLAB_TOKEN env var, else falls back to the glab-cli stored token.
# Config:  GITLAB_HOST    (default lab.facilitygrid.net)
#          GITLAB_PROJECT (default facility-grid/cross-validate)
#
set -euo pipefail

GITLAB_HOST="${GITLAB_HOST:-lab.facilitygrid.net}"
PROJECT="${GITLAB_PROJECT:-facility-grid/cross-validate}"
PROJECT_ENC="${PROJECT//\//%2F}"
API="https://${GITLAB_HOST}/api/v4"

# Cross-compile matrix. linux/amd64 (CI gate + lint image) and darwin/arm64
# (local dev) match the historical GitHub assets; linux/arm64 is published too
# so the Earthfile's "arm64 runners" note (lib/build-config/lint/Earthfile) is
# already covered. All targets are pure-Go (CGO disabled), so embeds travel.
TARGETS=("linux/amd64" "linux/arm64" "darwin/arm64")

# --- version + tag ---------------------------------------------------------
VERSION="$(grep -oE 'const version = "[0-9]+\.[0-9]+\.[0-9]+"' cmd/xpc/main.go \
  | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"
[ -n "${VERSION}" ] || { echo "FATAL: could not read version from cmd/xpc/main.go" >&2; exit 1; }
TAG="v${VERSION}"

# --- token -----------------------------------------------------------------
TOKEN="${GITLAB_TOKEN:-}"
if [ -z "${TOKEN}" ]; then
  TOKEN="$(glab auth status -t --hostname "${GITLAB_HOST}" 2>&1 \
    | grep -oE 'glpat-[A-Za-z0-9._-]+' | head -1 || true)"
fi
[ -n "${TOKEN}" ] || {
  echo "FATAL: no token. Set GITLAB_TOKEN or run 'glab auth login --hostname ${GITLAB_HOST}'." >&2
  exit 1
}
AUTH=(--header "PRIVATE-TOKEN: ${TOKEN}")

# --- preflight: the tag must already exist on the GitLab repo --------------
if ! curl -sf -o /dev/null "${AUTH[@]}" \
  "${API}/projects/${PROJECT_ENC}/repository/tags/${TAG}"; then
  echo "FATAL: tag ${TAG} not found on ${PROJECT}." >&2
  echo "       Bump cmd/xpc/main.go, commit, then:" >&2
  echo "         git tag ${TAG} && git push origin ${TAG}" >&2
  exit 1
fi

echo "Publishing xpc ${TAG} -> ${GITLAB_HOST}/${PROJECT}"

# --- build -----------------------------------------------------------------
OUT="$(mktemp -d)"; trap 'rm -rf "${OUT}"' EXIT
ASSETS=()
for t in "${TARGETS[@]}"; do
  os="${t%/*}"; arch="${t#*/}"; name="xpc-${os}-${arch}"
  echo "  build  ${name}"
  CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" \
    go build -trimpath -ldflags="-s -w" -o "${OUT}/${name}" ./cmd/xpc
  ASSETS+=("${name}")
done

# --- upload to the generic package registry --------------------------------
# Tolerate re-runs: 201 = created, 400/409 = this version's file already there.
PKGBASE="${API}/projects/${PROJECT_ENC}/packages/generic/xpc/${VERSION}"
for name in "${ASSETS[@]}"; do
  echo "  upload ${name}"
  ucode="$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" \
    --upload-file "${OUT}/${name}" "${PKGBASE}/${name}")"
  case "${ucode}" in
    200|201) ;;
    400|409) echo "         (already published for ${VERSION})" ;;
    *) echo "FATAL: upload of ${name} returned HTTP ${ucode}" >&2; exit 1 ;;
  esac
done

# --- assemble asset links (clean /downloads/<name> permalinks) -------------
links=""
for name in "${ASSETS[@]}"; do
  [ -n "${links}" ] && links="${links},"
  links="${links}{\"name\":\"${name}\",\"url\":\"${PKGBASE}/${name}\",\"direct_asset_path\":\"/${name}\",\"link_type\":\"package\"}"
done

# --- create the release (or refresh links if it already exists) ------------
code="$(curl -s -o "${OUT}/resp" -w '%{http_code}' --request POST \
  "${AUTH[@]}" --header "Content-Type: application/json" \
  --data "{\"tag_name\":\"${TAG}\",\"name\":\"${TAG}\",\"description\":\"xpc ${TAG}\",\"assets\":{\"links\":[${links}]}}" \
  "${API}/projects/${PROJECT_ENC}/releases")"

case "${code}" in
  201)
    echo "  release ${TAG} created"
    ;;
  409)
    echo "  release ${TAG} exists — refreshing asset links"
    existing="$(curl -sf "${AUTH[@]}" \
      "${API}/projects/${PROJECT_ENC}/releases/${TAG}/assets/links" 2>/dev/null || echo '[]')"
    for name in "${ASSETS[@]}"; do
      lid="$(printf '%s' "${existing}" \
        | grep -oE "\{[^}]*\"name\":\"${name}\"[^}]*\}" \
        | grep -oE '"id":[0-9]+' | grep -oE '[0-9]+' | head -1 || true)"
      [ -n "${lid}" ] && curl -sf "${AUTH[@]}" --request DELETE \
        "${API}/projects/${PROJECT_ENC}/releases/${TAG}/assets/links/${lid}" >/dev/null || true
      curl -sf "${AUTH[@]}" --header "Content-Type: application/json" --request POST \
        --data "{\"name\":\"${name}\",\"url\":\"${PKGBASE}/${name}\",\"direct_asset_path\":\"/${name}\",\"link_type\":\"package\"}" \
        "${API}/projects/${PROJECT_ENC}/releases/${TAG}/assets/links" >/dev/null
    done
    ;;
  *)
    echo "FATAL: release create returned HTTP ${code}" >&2
    cat "${OUT}/resp" >&2; echo >&2
    exit 1
    ;;
esac

# --- verify the permalink the consumers actually use -----------------------
PERMA="https://${GITLAB_HOST}/${PROJECT}/-/releases/${TAG}/downloads/xpc-linux-amd64"
vcode="$(curl -s -L -o /dev/null -w '%{http_code}' "${AUTH[@]}" "${PERMA}")"
[ "${vcode}" = "200" ] \
  && echo "OK: ${PERMA} -> 200" \
  || echo "WARN: permalink check returned HTTP ${vcode} (${PERMA})" >&2

echo "Done: https://${GITLAB_HOST}/${PROJECT}/-/releases/${TAG}"
