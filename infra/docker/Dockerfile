# syntax=docker/dockerfile:1.7
#
# One image definition for every Go service. `SERVICE` selects which binary is
# built and which one the entrypoint runs, because api, gitd, aigw and mcp share
# a Go workspace and differ only in their cmd package.
#
#   docker build -f infra/docker/Dockerfile --build-arg SERVICE=api -t halyard-api .
#
# Build context is the REPOSITORY ROOT, not this directory: go.work spans
# packages/ and services/, so a narrower context cannot resolve the workspace.
#
# Deliberately host-agnostic. SPEC §21 decision 1 (Cloudflare Containers, or a
# container host behind Cloudflare) is still open, and every candidate consumes
# an OCI image, so this file does not need to know the answer. Nothing in here
# may grow a host-specific assumption.

ARG GO_VERSION=1.27.1

# --- build -------------------------------------------------------------------
FROM golang:${GO_VERSION}-alpine AS build

ARG SERVICE
ARG VERSION=dev
ARG COMMIT=unknown

RUN test -n "${SERVICE}" || (echo "SERVICE build-arg is required" >&2; exit 1)

WORKDIR /src

# Copy the workspace's module graph first so `go mod download` caches
# independently of source edits. go.work is what makes the replace-free
# cross-module imports resolve.
# The trailing `*` on each go.sum is load-bearing: aigw, gitd and mcp have no
# third-party dependencies yet and therefore no go.sum, and a bare COPY of a
# missing file fails the build. When they grow one it is picked up with no edit
# here.
COPY go.work go.work.sum* ./
COPY agent/agentd/go.mod agent/agentd/go.sum* ./agent/agentd/
COPY packages/chassis/go.mod packages/chassis/go.sum* ./packages/chassis/
COPY packages/db/go.mod packages/db/go.sum* ./packages/db/
COPY packages/schema/gen/go/go.mod packages/schema/gen/go/go.sum* ./packages/schema/gen/go/
COPY packages/schema/tools/go.mod packages/schema/tools/go.sum* ./packages/schema/tools/
COPY services/aigw/go.mod services/aigw/go.sum* ./services/aigw/
COPY services/api/go.mod services/api/go.sum* ./services/api/
COPY services/gitd/go.mod services/gitd/go.sum* ./services/gitd/
COPY services/mcp/go.mod services/mcp/go.sum* ./services/mcp/

RUN --mount=type=cache,target=/go/pkg/mod \
    go work sync && go mod download all

COPY . .

# CGO_ENABLED=0 so the binary is static and the runtime stage can be distroless.
# The ldflags match the Makefile's exactly: the version and commit live in
# package main because that is where -X can reach them.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/service ./services/${SERVICE}/cmd/${SERVICE}

# --- runtime -----------------------------------------------------------------
# distroless/static: no shell, no package manager, no libc. Nothing in the image
# can be exec'd if the service is ever compromised, which is the point.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

ARG SERVICE
ARG VERSION=dev
ARG COMMIT=unknown

LABEL org.opencontainers.image.source="https://github.com/anasatwork01/cofound" \
      org.opencontainers.image.title="halyard-${SERVICE}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"

COPY --from=build /out/service /service

# Non-root by default in this base image (uid 65532). Stated anyway, because a
# future edit that adds a writable path should have to think about it.
USER nonroot:nonroot

# The chassis binds HTTP_ADDR, defaulting to :8080 (packages/chassis/config).
# It is NOT `PORT` — a host that injects PORT (Cloud Run, Railway) needs
# HTTP_ADDR set explicitly in its service config, which the runbook says.
#
# ADMIN_ADDR is deliberately unset: it exposes pprof and the log-level control,
# and the chassis refuses a non-loopback ADMIN_ADDR in production.
ENV HTTP_ADDR=:8080
EXPOSE 8080

ENTRYPOINT ["/service"]
