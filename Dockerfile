# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.8

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine3.24 AS build
WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ARG EIF_VERSION=dev
ARG EIF_BACKEND_COMMIT=unknown
ARG EIF_FRONTEND_COMMIT=unknown
ARG EIF_ORCHESTRATOR_COMMIT=unknown
ARG EIF_BUILD_TIME=unknown

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build \
      -trimpath \
      -buildvcs=false \
      -ldflags="-s -w \
        -X github.com/yunotools/eif/internal/core/buildinfo.Version=${EIF_VERSION} \
        -X github.com/yunotools/eif/internal/core/buildinfo.BackendCommit=${EIF_BACKEND_COMMIT} \
        -X github.com/yunotools/eif/internal/core/buildinfo.FrontendCommit=${EIF_FRONTEND_COMMIT} \
        -X github.com/yunotools/eif/internal/core/buildinfo.OrchestratorCommit=${EIF_ORCHESTRATOR_COMMIT} \
        -X github.com/yunotools/eif/internal/core/buildinfo.BuildTime=${EIF_BUILD_TIME}" \
      -o /out/eif \
      ./cmd/eif

FROM --platform=$BUILDPLATFORM alpine:3.24.1 AS certs
RUN apk add --no-cache ca-certificates && mkdir -p /out/runtime

FROM scratch
WORKDIR /yuno

ARG EIF_VERSION=dev
ARG EIF_BACKEND_COMMIT=unknown
ARG EIF_FRONTEND_COMMIT=unknown
ARG EIF_ORCHESTRATOR_COMMIT=unknown
ARG EIF_BUILD_TIME=unknown
ARG EIF_SOURCE_URL=https://github.com/yunotools/eif

LABEL org.opencontainers.image.title="EIF" \
      org.opencontainers.image.description="Etax Invoice Fast" \
      org.opencontainers.image.version="${EIF_VERSION}" \
      org.opencontainers.image.revision="${EIF_BACKEND_COMMIT}" \
      org.opencontainers.image.created="${EIF_BUILD_TIME}" \
      org.opencontainers.image.source="${EIF_SOURCE_URL}" \
      io.eif.backend.revision="${EIF_BACKEND_COMMIT}" \
      io.eif.frontend.revision="${EIF_FRONTEND_COMMIT}" \
      io.eif.orchestrator.revision="${EIF_ORCHESTRATOR_COMMIT}"

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/eif /yuno/eif
COPY --chown=65532:65532 --from=certs /out/runtime /yuno/runtime

ENV EIF_HOST=0.0.0.0 \
    EIF_OPEN_BROWSER=false \
    EIF_UPDATE_ENABLED=false

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/yuno/eif"]
