# syntax=docker/dockerfile:1.7
FROM golang:1.27.1 AS builder
WORKDIR /src
COPY apis/network/go.mod apis/network/go.sum ./apis/network/
COPY operators/network/go.mod operators/network/go.sum ./operators/network/
RUN --mount=type=cache,target=/go/pkg/mod cd operators/network && go mod download
COPY apis/network ./apis/network
COPY operators/network ./operators/network
WORKDIR /src/operators/network
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /manager ./cmd/manager

FROM node:24-bookworm-slim
WORKDIR /opt/stacks-transactions
COPY operators/network/transactions/package.json operators/network/transactions/package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts && npm cache clean --force
COPY operators/network/transactions/sign.mjs ./
COPY --from=builder /manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
