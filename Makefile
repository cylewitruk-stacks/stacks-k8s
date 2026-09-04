GO ?= go
GOVULNCHECK_VERSION ?= v1.7.0

MODULE_DIRS := \
	apis/network \
	apis/network/tools \
	operators/network \
	operators/observability \
	operators/observability/tools \
	tools/chart-policy \
	tools/module-policy

.PHONY: api-verify docker-build docker-check fmt generate helm-verify module-policy-verify modules-verify rbac-verify test \
	test-integration test-race verify verify-chart-policy verify-network \
	verify-observability vuln

verify: modules-verify module-policy-verify verify-chart-policy verify-network verify-observability

api-verify:
	$(MAKE) -C apis/network verify

module-policy-verify:
	GOWORK=off $(GO) -C tools/module-policy vet ./...
	GOWORK=off $(GO) -C tools/module-policy test ./...
	GOWORK=off $(GO) -C tools/module-policy run ./cmd/module-policy-check --module ../../apis/network

verify-chart-policy:
	GOWORK=off $(GO) -C tools/chart-policy vet ./...
	GOWORK=off $(GO) -C tools/chart-policy test ./...
	GOWORK=off $(GO) -C tools/chart-policy test -race ./...

verify-network:
	$(MAKE) -C charts/stacks-network-operator verify

verify-observability:
	$(MAKE) -C charts/stacks-observability-operator verify

fmt:
	$(MAKE) -C apis/network fmt
	$(MAKE) -C operators/network fmt
	$(MAKE) -C operators/observability fmt

generate:
	$(MAKE) -C apis/network generate
	$(MAKE) -C operators/observability generate

test:
	$(MAKE) -C apis/network test
	$(MAKE) -C operators/network test
	$(MAKE) -C operators/observability test

test-race:
	$(MAKE) -C apis/network test-race
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
	GOWORK=off $(GO) -C apis/network run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	GOWORK=off $(GO) -C operators/network run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	GOWORK=off $(GO) -C operators/observability run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

docker-check:
	docker build --check --file operators/network/Dockerfile .
	docker build --check --file operators/observability/Dockerfile .

docker-build:
	docker build --file operators/network/Dockerfile --tag stacks-network-operator:verify .
	docker build --file operators/observability/Dockerfile --tag stacks-observability-operator:verify .
