GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)

.PHONY: fmt test test-integration test-live test-race vet verify

fmt:
	@test -z "$$(gofmt -l .)" || { echo "Go sources are not formatted" >&2; exit 1; }

test:
	$(GO_ISOLATED) test ./...

test-integration:
	$(GO_ISOLATED) test -tags=integration -count=1 ./internal/integration

test-live:
	$(GO_ISOLATED) test -tags=live -count=1 -v ./internal/integration

test-race:
	$(GO_ISOLATED) test -race ./...

vet:
	$(GO_ISOLATED) vet ./...

verify: fmt vet test test-race test-integration
