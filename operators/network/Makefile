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

verify: fmt vet test test-race test-integration test-cli test-sdk

.PHONY: test-sdk
test-sdk:
	npm --prefix transactions ci --ignore-scripts
	npm --prefix transactions test
	$(GO_ISOLATED) test -tags=sdk ./internal/environment ./internal/bootstrap
	$(GO_ISOLATED) test -race -tags=sdk ./internal/environment ./internal/bootstrap

.PHONY: test-cli
test-cli:
	@set -eu; for command in stacks-environment stacks-bootstrap stacks-maintain-signers; do \
		$(GO_ISOLATED) run ./cmd/$$command --help >/dev/null 2>&1; \
	done
