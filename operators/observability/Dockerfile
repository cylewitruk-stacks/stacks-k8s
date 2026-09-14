# syntax=docker/dockerfile:1.7
FROM golang:1.27.1 AS builder
WORKDIR /src
COPY operators/observability/go.mod operators/observability/go.sum ./operators/observability/
COPY apis/network/go.mod apis/network/go.sum ./apis/network/
RUN --mount=type=cache,target=/go/pkg/mod cd operators/observability && go mod download
COPY apis/network ./apis/network
COPY operators/observability ./operators/observability
WORKDIR /src/operators/observability
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /manager ./cmd/manager && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /recorder ./cmd/recorder && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /backend-init ./cmd/backend-init

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /manager /manager
COPY --from=builder /recorder /recorder
COPY --from=builder /backend-init /backend-init
USER 65532:65532
ENTRYPOINT ["/manager"]
