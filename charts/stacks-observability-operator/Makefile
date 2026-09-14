HELM ?= helm
GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)
OPERATOR_DIR ?= ../../operators/observability
CHART_POLICY_DIR ?= ../../tools/chart-policy

.PHONY: verify helm-verify rbac-verify workload-verify dependencies

verify: dependencies
	$(MAKE) -C $(OPERATOR_DIR) verify
	$(MAKE) helm-verify
	$(MAKE) rbac-verify
	$(MAKE) workload-verify

helm-verify: dependencies
	$(GO_ISOLATED) -C $(CHART_POLICY_DIR) test ./internal/telemetry
	$(HELM) lint . --kube-version 1.37.0
	$(HELM) template verify . --namespace stacks-observation-system >/dev/null
	@if $(HELM) template verify . --namespace stacks-observation-system --set controller.networkAPIVersion=v1alpha1 >/dev/null 2>&1; then \
		echo "retired network API option unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-observation-system --set-json image=null >/dev/null 2>&1; then \
		echo "null image configuration unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-observation-system --set replicaCount=6 >/dev/null 2>&1; then \
		echo "replicaCount above the supported bound unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-observation-system --set controller.maxConcurrentReconciles=33 >/dev/null 2>&1; then \
		echo "controller concurrency above the supported bound unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-observation-system --set replicaCount=2 --set controller.leaderElection=false >/dev/null 2>&1; then \
		echo "unsafe multi-replica render unexpectedly succeeded" >&2; exit 1; \
	fi
	$(HELM) template verify . --namespace stacks-observation-system --set replicaCount=2 --set controller.leaderElection=true >/dev/null
	@$(HELM) template verify . --namespace stacks-observation-system --set controller.leaderElection=false | grep -q 'type: Recreate'

rbac-verify: dependencies
	$(HELM) template verify . --namespace stacks-observation-system --values values-local.yaml | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-observation-system --set-json 'watchNamespaces=["lab-a","lab-b"]' | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-observation-system | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check

workload-verify: dependencies
	$(HELM) template verify . --namespace stacks-observation-system | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-observation-system --set serviceAccount.create=false --set serviceAccount.name=existing.account >/dev/null
	@if $(HELM) template verify . --namespace stacks-observation-system --set-string 'serviceAccount.name=bad\nhostPID: true' >/dev/null 2>&1; then \
		echo "invalid service-account name unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-observation-system --set-string serviceAccount.name=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa >/dev/null 2>&1; then \
		echo "overlong service-account label unexpectedly rendered" >&2; exit 1; \
	fi

# Restore locked dependencies without consulting or changing the user's repository configuration.
dependencies:
	@set -eu; repo_dir="$$(mktemp -d)"; trap 'rm -rf "$$repo_dir"' EXIT; \
		$(HELM) repo add greptime https://greptimeteam.github.io/helm-charts/ \
			--repository-config "$$repo_dir/repositories.yaml" --repository-cache "$$repo_dir"; \
		$(HELM) dependency build . --skip-refresh \
			--repository-config "$$repo_dir/repositories.yaml" --repository-cache "$$repo_dir"
