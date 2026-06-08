# syntax=docker/dockerfile:1.7
#
# xpcd container image — the runtime policy daemon (admission webhook + the
# observe-only controller sweep). Built from this repo; pure-Go, CGO disabled,
# so the embedded Shen kernel travels in the binary.
#
# Targets:
#   serve  — minimal distroless image for `xpcd serve` (admission webhook).
#            No kubectl: the admission path never shells out to a cluster.
#   watch  — distroless/base + kubectl for `xpcd watch` (the controller sweep
#            captures the live cluster via `kubectl get -o yaml`).
#   all    — DEFAULT (last) stage: the watch image, usable for BOTH modes (the
#            unused kubectl in serve mode is harmless). One tag for both
#            Deployments; pick the slim `serve` target if you deploy the webhook
#            separately and want kubectl off the webhook's attack surface.
#
# Build (FacilityGrid deploys from ECR; ghcr is the placeholder default):
#   docker build -t "$ECR_REGISTRY/xpcd:$TAG"        --build-arg VERSION=$TAG -f cmd/xpcd/Dockerfile .
#   docker build -t "$ECR_REGISTRY/xpcd:$TAG-serve"  --target serve --build-arg VERSION=$TAG -f cmd/xpcd/Dockerfile .
#
# The runtime expects a writable /tmp (the embedded kernel materializes there);
# deploy/runtime mounts an emptyDir at /tmp alongside readOnlyRootFilesystem.

ARG GO_VERSION=1.25
ARG KUBECTL_VERSION=1.31.4
ARG DISTROLESS_STATIC=gcr.io/distroless/static-debian12:nonroot
ARG DISTROLESS_BASE=gcr.io/distroless/base-debian12:nonroot

# ---- build: compile the static binary --------------------------------------
FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION} AS build
WORKDIR /src
# Module cache layer: copy manifests first so deps cache across source edits.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/xpcd ./cmd/xpcd

# ---- kubectl: fetch a pinned client for the watch variant -------------------
FROM debian:12-slim AS kubectl
ARG KUBECTL_VERSION
ARG TARGETARCH
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && curl -fsSL -o /kubectl \
      "https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/linux/${TARGETARCH:-amd64}/kubectl" \
 && chmod 0755 /kubectl

# ---- serve: admission webhook, no kubectl (smallest, least surface) ---------
FROM ${DISTROLESS_STATIC} AS serve
COPY --from=build /out/xpcd /usr/local/bin/xpcd
USER 65532:65532
EXPOSE 8443 9090
ENTRYPOINT ["/usr/local/bin/xpcd"]
CMD ["serve"]

# ---- watch: controller sweep, carries kubectl on PATH -----------------------
FROM ${DISTROLESS_BASE} AS watch
COPY --from=kubectl /kubectl /usr/local/bin/kubectl
COPY --from=build /out/xpcd /usr/local/bin/xpcd
USER 65532:65532
EXPOSE 9090
ENTRYPOINT ["/usr/local/bin/xpcd"]
CMD ["watch"]

# ---- all: DEFAULT — combined image for both modes ---------------------------
# Both deploy/runtime Deployments reference one image tag; this stage backs it.
# The serve Deployment overrides the command to `serve …`; watch uses `watch …`.
FROM watch AS all
