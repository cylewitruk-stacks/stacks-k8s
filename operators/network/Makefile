GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)

.PHONY: fmt test test-integration test-live test-race vet verify

fmt:
	@test -z "$$(gofmt -l .)" || { echo "Go sources are not formatted" >&2; exit 1; }

test:
	$(GO_ISOLATED) test ./...

test-integration:
	$(GO_ISOLATED) test -p=1 -tags=integration -count=1 ./internal/foundationintegration ./internal/publicintegration ./internal/participantworkload ./internal/bitcoincontrol ./internal/stacksworker ./internal/faucetrequest ./internal/networkruntime ./internal/stacksoperation

test-live: test-live-public

test-race:
	$(GO_ISOLATED) test -race ./...

vet:
	$(GO_ISOLATED) vet ./...

verify: fmt vet test test-race test-integration test-public-contract test-cli test-sdk

.PHONY: test-sdk
test-sdk:
	npm --prefix transactions ci --ignore-scripts
	npm --prefix transactions test

.PHONY: test-cli
test-cli:
	@set -eu; for command in foundation stacks-worker; do \
		$(GO_ISOLATED) run ./cmd/$$command --help >/dev/null 2>&1; \
	done

.PHONY: test-live-public test-public-integration test-public-contract
# Explicit STACKS_PUBLIC_* configuration is required; this target is never part of verify.
test-live-public:
	$(GO_ISOLATED) test -tags=live -timeout=90m -count=1 -v ./internal/publicintegration -run '^TestPublicBaselineLifecycle$$'

test-public-integration:
	$(GO_ISOLATED) test -tags=integration -count=1 ./internal/publicintegration

# Compile every live helper, but execute only local contracts with live access disabled.
test-public-contract:
	STACKS_PUBLIC_LIVE=0 $(GO_ISOLATED) test -race -tags=live -count=1 ./internal/publicintegration
