GO ?= go
GOVULNCHECK_VERSION ?= v1.7.0

MODULE_DIRS := \
	operators/network \
	operators/network/tools \
	operators/observability \
	operators/observability/tools \
	tools/chart-policy

.PHONY: docker-check fmt generate helm-verify modules-verify rbac-verify test \
	test-integration test-race verify verify-chart-policy verify-network \
	verify-observability vuln

verify: modules-verify verify-chart-policy verify-network verify-observability

verify-chart-policy:
	GOWORK=off $(GO) -C tools/chart-policy vet ./...
	GOWORK=off $(GO) -C tools/chart-policy test ./...
	GOWORK=off $(GO) -C tools/chart-policy test -race ./...

verify-network:
	$(MAKE) -C charts/stacks-network-operator verify

verify-observability:
	$(MAKE) -C charts/stacks-observability-operator verify

fmt:
	$(MAKE) -C operators/network fmt
	$(MAKE) -C operators/observability fmt

generate:
	$(MAKE) -C operators/network generate
	$(MAKE) -C operators/observability generate

test:
	$(MAKE) -C operators/network test
	$(MAKE) -C operators/observability test

test-race:
	$(MAKE) -C operators/network test-race
	$(MAKE) -C operators/observability test-race
	GOWORK=off $(GO) -C tools/chart-policy test -race ./...

test-integration:
	$(MAKE) -C operators/network test-integration

helm-verify:
	$(MAKE) -C charts/stacks-network-operator helm-verify workload-verify
	$(MAKE) -C charts/stacks-observability-operator helm-verify workload-verify

rbac-verify:
	$(MAKE) -C charts/stacks-network-operator rbac-verify
	$(MAKE) -C charts/stacks-observability-operator rbac-verify

modules-verify:
	@set -eu; for module in $(MODULE_DIRS); do \
		echo "Verifying $$module"; \
		GOWORK=off $(GO) -C $$module mod tidy -diff; \
		GOWORK=off $(GO) -C $$module mod verify; \
	done

vuln:
	GOWORK=off $(GO) -C operators/network run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	GOWORK=off $(GO) -C operators/observability run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

docker-check:
	docker build --check operators/network
	docker build --check operators/observability
