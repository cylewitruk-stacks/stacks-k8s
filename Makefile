GO ?= go
GOVULNCHECK_VERSION ?= v1.7.0

MODULE_DIRS := \
	libs/stacks \
	apis/network \
	apis/network/tools \
	operators/network \
	operators/action \
	operators/observability \
	operators/observability/tools \
	tools/chart-policy \
	tools/module-policy \
	tools/local-cluster

.PHONY: api-verify docker-build docker-check fmt generate helm-verify module-policy-verify modules-verify rbac-verify test \
	test-integration test-race verify verify-chart-policy verify-network \
	verify-observability verify-action vuln

verify: verify-foundation verify-local-cluster modules-verify module-policy-verify verify-chart-policy verify-network verify-observability verify-action
	$(MAKE) -C charts/stacks-chaos-profile verify

api-verify:
	$(MAKE) -C apis/network verify

module-policy-verify:
	GOWORK=off $(GO) -C tools/module-policy vet ./...
	GOWORK=off $(GO) -C tools/module-policy test ./...
	GOWORK=off $(GO) -C tools/module-policy run ./cmd/module-policy-check --module ../../apis/network
	GOWORK=off $(GO) -C tools/module-policy run ./cmd/module-policy-check --module ../../libs/stacks --portable

verify-chart-policy:
	GOWORK=off $(GO) -C tools/chart-policy vet ./...
	GOWORK=off $(GO) -C tools/chart-policy vet -tags=integration,live ./internal/integration
	GOWORK=off $(GO) -C tools/chart-policy test ./...
	GOWORK=off $(GO) -C tools/chart-policy test -race ./...
	GOWORK=off $(GO) -C tools/chart-policy test -tags=integration -count=1 ./internal/integration

verify-network:
	$(MAKE) -C charts/stacks-network-operator verify

verify-action:
	$(MAKE) -C charts/stacks-action-operator verify

verify-observability:
	$(MAKE) -C charts/stacks-observability-operator verify

fmt:
	$(MAKE) -C apis/network fmt
	$(MAKE) -C operators/network fmt
	$(MAKE) -C operators/observability fmt
	$(MAKE) -C operators/action fmt

generate:
	$(MAKE) -C apis/network generate
	$(MAKE) -C operators/observability generate

test:
	$(MAKE) -C apis/network test
	$(MAKE) -C operators/network test
	$(MAKE) -C operators/observability test
	$(MAKE) -C operators/action test

test-race:
	$(MAKE) -C apis/network test-race
	$(MAKE) -C operators/network test-race
	$(MAKE) -C operators/observability test-race
	$(MAKE) -C operators/action test-race
	GOWORK=off $(GO) -C tools/chart-policy test -race ./...

test-integration:
	GOWORK=off $(GO) -C tools/chart-policy test -tags=integration -count=1 ./internal/integration
	$(MAKE) -C operators/network test-integration
	$(MAKE) -C operators/action test-integration

helm-verify:
	$(MAKE) -C charts/stacks-chaos-profile helm-verify
	$(MAKE) -C charts/stacks-network-operator helm-verify workload-verify
	$(MAKE) -C charts/stacks-observability-operator helm-verify workload-verify
	$(MAKE) -C charts/stacks-action-operator helm-verify workload-verify

rbac-verify:
	$(MAKE) -C charts/stacks-network-operator rbac-verify
	$(MAKE) -C charts/stacks-observability-operator rbac-verify
	$(MAKE) -C charts/stacks-action-operator rbac-verify

modules-verify:
	@set -eu; for module in $(MODULE_DIRS); do \
		echo "Verifying $$module"; \
		GOWORK=off $(GO) -C $$module mod tidy -diff; \
		GOWORK=off $(GO) -C $$module mod verify; \
	done

vuln:
	GOWORK=off $(GO) -C libs/stacks run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	GOWORK=off $(GO) -C tools/chart-policy run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	npm --prefix operators/network/transactions audit --omit=dev
	GOWORK=off $(GO) -C apis/network run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	GOWORK=off $(GO) -C operators/network run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	GOWORK=off $(GO) -C operators/observability run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	GOWORK=off $(GO) -C operators/action run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

docker-check:
	docker build --check --file operators/network/Dockerfile.foundation .
	docker build --check --file operators/network/transactions/Dockerfile .
	docker build --check --file operators/network/Dockerfile .
	docker build --check --file operators/observability/Dockerfile .
	docker build --check --file operators/action/Dockerfile .

docker-build:
	docker build --file operators/network/Dockerfile.foundation --tag stacks-network-foundation:verify .
	docker build --file operators/network/transactions/Dockerfile --tag stacks-transaction-worker:verify .
	docker build --file operators/network/Dockerfile --tag stacks-network-operator:verify .
	docker build --file operators/observability/Dockerfile --tag stacks-observability-operator:verify .
	docker build --file operators/action/Dockerfile --tag stacks-action-operator:verify .

CLUSTER_TARGETS := cluster-create cluster-start cluster-stop cluster-destroy cluster-chaos-install \
	cluster-headlamp-install cluster-headlamp-uninstall cluster-headlamp cluster-headlamp-token cluster-metrics-install
.PHONY: $(CLUSTER_TARGETS) verify-local-cluster
$(CLUSTER_TARGETS):
	$(MAKE) -C tools/local-cluster $(patsubst cluster-%,%,$@)

verify-local-cluster:
	GOWORK=off $(GO) -C tools/local-cluster vet ./...
	GOWORK=off $(GO) -C tools/local-cluster test ./...

.PHONY: verify-foundation
verify-foundation:
	GOWORK=off $(GO) -C libs/stacks vet ./...
	GOWORK=off $(GO) -C libs/stacks test -race ./...
	$(MAKE) -C charts/stacks-network-foundation verify
