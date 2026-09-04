# syntax=docker/dockerfile:1.7
FROM golang:1.27.1 AS builder
WORKDIR /src
COPY apis/network/go.mod apis/network/go.sum ./apis/network/
COPY operators/network/go.mod operators/network/go.sum ./operators/network/
RUN --mount=type=cache,target=/go/pkg/mod cd operators/network && go mod download
COPY apis/network ./apis/network
COPY operators/network ./operators/network
WORKDIR /src/operators/network
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /manager ./cmd/manager

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
