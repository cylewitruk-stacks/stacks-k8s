HELM ?= helm
GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)
OPERATOR_DIR ?= ../../operators/network
CHART_POLICY_DIR ?= ../../tools/chart-policy

.PHONY: verify helm-verify rbac-verify workload-verify

verify:
	$(MAKE) -C $(OPERATOR_DIR) verify
	$(MAKE) helm-verify
	$(MAKE) rbac-verify
	$(MAKE) workload-verify

helm-verify:
	$(HELM) lint .
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
	$(HELM) template verify . --namespace stacks-network-system | $(GO_ISOLATED) -C $(OPERATOR_DIR) run ./cmd/rbac-check

workload-verify:
	$(HELM) template verify . --namespace stacks-network-system | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
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
