HELM ?= helm
GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)
OPERATOR_DIR ?= ../../operators/network
API_DIR ?= ../../apis/network
CHART_POLICY_DIR ?= ../../tools/chart-policy

.PHONY: verify helm-verify rbac-verify workload-verify

verify:
	$(MAKE) -C $(API_DIR) verify
	$(MAKE) -C $(OPERATOR_DIR) verify
	$(MAKE) helm-verify
	$(MAKE) rbac-verify
	$(MAKE) workload-verify

helm-verify:
	@if $(HELM) template verify . --set bitcoinReorganization.enabled=true --set bitcoinProduction.enabled=false >/dev/null 2>&1; then echo "reorganization without executor unexpectedly rendered" >&2; exit 1; fi
	@if $(HELM) template verify . --set bitcoinGeneration.enabled=true --set bitcoinProduction.enabled=false >/dev/null 2>&1; then echo "generation without executor unexpectedly rendered" >&2; exit 1; fi
	$(HELM) lint . --kube-version 1.37.0
	$(HELM) template verify . --kube-version 1.32.0 >/dev/null
	@if $(HELM) template verify . --kube-version 1.31.0 >/dev/null 2>&1; then \
		echo "unsupported Kubernetes version unexpectedly rendered" >&2; exit 1; \
	fi
	$(HELM) template verify . --namespace stacks-network-system >/dev/null
	@if $(HELM) template verify . --namespace stacks-network-system --set-json image=null >/dev/null 2>&1; then \
		echo "null image configuration unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-network-system --set replicaCount=6 >/dev/null 2>&1; then \
		echo "replicaCount above the supported bound unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-network-system --set controller.maxConcurrentReconciles=33 >/dev/null 2>&1; then \
		echo "controller concurrency above the supported bound unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-network-system --set replicaCount=2 --set controller.leaderElection=false >/dev/null 2>&1; then \
		echo "unsafe multi-replica render unexpectedly succeeded" >&2; exit 1; \
	fi
	$(HELM) template verify . --namespace stacks-network-system --set replicaCount=2 --set controller.leaderElection=true >/dev/null
	@$(HELM) template verify . --namespace stacks-network-system --set controller.leaderElection=false | grep -q 'type: Recreate'

rbac-verify:
	$(HELM) template verify . --namespace stacks-network-system --set controller.watchNamespace=experiments | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true --set stacksTransactions.enabled=true --set stacksOperation.enabled=true | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-network-system | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true --set bitcoinGeneration.enabled=true | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true --set bitcoinReorganization.enabled=true | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true --set bitcoinGeneration.enabled=true --set bitcoinReorganization.enabled=true | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check
	$(HELM) template verify . --namespace stacks-network-system --set stacksTransactions.enabled=true | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check

workload-verify:
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true --set stacksTransactions.enabled=true --set stacksOperation.enabled=true | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-network-system | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true --set bitcoinGeneration.enabled=true | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true --set bitcoinReorganization.enabled=true | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-network-system --set bitcoinProduction.enabled=true --set bitcoinGeneration.enabled=true --set bitcoinReorganization.enabled=true | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-network-system --set stacksTransactions.enabled=true | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-network-system --set serviceAccount.create=false --set serviceAccount.name=existing.account >/dev/null
	@if $(HELM) template verify . --namespace stacks-network-system --set-json 'image.pullSecrets=["bad\nhostNetwork: true"]' >/dev/null 2>&1; then \
		echo "invalid image pull Secret name unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-network-system --set-string 'serviceAccount.name=bad\nhostPID: true' >/dev/null 2>&1; then \
		echo "invalid service-account name unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-network-system --set-string serviceAccount.name=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa >/dev/null 2>&1; then \
		echo "overlong service-account label unexpectedly rendered" >&2; exit 1; \
	fi
