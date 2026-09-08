# syntax=docker/dockerfile:1.7
FROM golang:1.27.1 AS builder
COPY main.go /fixture/main.go
RUN CGO_ENABLED=0 go build -trimpath -o /fixture/bitcoind /fixture/main.go

FROM bitcoin/bitcoin:31.1@sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63
ARG RECEIPT_FAULT=delay
ARG RECEIPT_METHOD=generatetoaddress
ARG RECEIPT_AFTER_INVALIDATION=false
ENV RECEIPT_FAULT=${RECEIPT_FAULT} RECEIPT_METHOD=${RECEIPT_METHOD} RECEIPT_AFTER_INVALIDATION=${RECEIPT_AFTER_INVALIDATION}
USER root
RUN mv /opt/bitcoin-31.1/bin/bitcoind /opt/bitcoin-31.1/bin/bitcoind-real
COPY --from=builder /fixture/bitcoind /opt/bitcoin-31.1/bin/bitcoind
USER 65532:65532
