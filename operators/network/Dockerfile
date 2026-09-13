# syntax=docker/dockerfile:1.7
FROM golang:1.27.1 AS builder
WORKDIR /src
COPY libs/stacks/go.mod libs/stacks/go.sum ./libs/stacks/
COPY apis/network/go.mod apis/network/go.sum ./apis/network/
COPY operators/action/go.mod operators/action/go.sum ./operators/action/
COPY operators/network/go.mod operators/network/go.sum ./operators/network/
RUN --mount=type=cache,target=/go/pkg/mod cd operators/network && go mod download
COPY libs/stacks ./libs/stacks
COPY apis/network ./apis/network
COPY operators/network ./operators/network
WORKDIR /src/operators/network
# Keep the independently licensed upstream contracts unmodified and hash-verified.
RUN <<'EOF'
set -eu
git init -q /tmp/sbtc
git -C /tmp/sbtc remote add origin https://github.com/stacks-network/sbtc.git
git -C /tmp/sbtc fetch -q --depth=1 origin d31b0780cf1ae91b6ef4ed7ee89400c796aea913
git -C /tmp/sbtc checkout -q --detach FETCH_HEAD
mkdir -p /protocol/sbtc /licenses/sbtc
for name in sbtc-registry sbtc-token sbtc-bootstrap-signers sbtc-deposit sbtc-withdrawal; do
    cp "/tmp/sbtc/contracts/contracts/$name.clar" /protocol/sbtc/
done
cp /tmp/sbtc/LICENSE /licenses/sbtc/LICENSE
cp internal/protocolcontracts/sbtc-contracts.json /licenses/sbtc/provenance.json
rm -rf /tmp/sbtc
EOF
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go run ./cmd/stacks-worker --verify-contracts=/protocol/sbtc
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /foundation ./cmd/foundation
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /stacks-worker ./cmd/stacks-worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /foundation /foundation
COPY --from=builder /stacks-worker /stacks-worker
COPY --from=builder /protocol /protocol
COPY --from=builder /licenses /licenses
USER 65532:65532
ENTRYPOINT ["/foundation"]
