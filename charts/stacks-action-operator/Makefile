HELM ?= helm
GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)
OPERATOR_DIR ?= ../../operators/action
CHART_POLICY_DIR ?= ../../tools/chart-policy

.PHONY: verify helm-verify rbac-verify workload-verify

verify:
	$(MAKE) -C $(OPERATOR_DIR) verify
	$(MAKE) helm-verify
	$(MAKE) rbac-verify
	$(MAKE) workload-verify

helm-verify:
	@if $(HELM) template verify . --namespace stacks-action-system --set bitcoinGeneration.enabled=false --set bitcoinReorganization.enabled=false >/dev/null 2>&1; then \
		echo "render with no enabled action kinds unexpectedly succeeded" >&2; exit 1; \
	fi
	$(HELM) lint . --kube-version 1.37.0
	$(HELM) template verify . --namespace stacks-action-system >/dev/null
	@if $(HELM) template verify . --namespace stacks-action-system --set-json image=null >/dev/null 2>&1; then \
		echo "null image configuration unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-action-system --set replicaCount=6 >/dev/null 2>&1; then \
		echo "replicaCount above the supported bound unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-action-system --set controller.maxConcurrentReconciles=33 >/dev/null 2>&1; then \
		echo "controller concurrency above the supported bound unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-action-system --set replicaCount=2 --set controller.leaderElection=false >/dev/null 2>&1; then \
		echo "unsafe multi-replica render unexpectedly succeeded" >&2; exit 1; \
	fi
	$(HELM) template verify . --namespace stacks-action-system --set replicaCount=2 --set controller.leaderElection=true >/dev/null
	@$(HELM) template verify . --namespace stacks-action-system --set controller.leaderElection=false | grep -q 'type: Recreate'

rbac-verify:
	$(HELM) template verify . --namespace stacks-action-system | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/action-rbac-check

	$(HELM) template verify . --namespace stacks-action-system --set bitcoinReorganization.enabled=true | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/action-rbac-check
	$(HELM) template verify . --namespace stacks-action-system --set bitcoinGeneration.enabled=false --set bitcoinReorganization.enabled=true | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/action-rbac-check

workload-verify:
	$(HELM) template verify . --namespace stacks-action-system | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
	$(HELM) template verify . --namespace stacks-action-system --set serviceAccount.create=false --set serviceAccount.name=existing.account >/dev/null
	@if $(HELM) template verify . --namespace stacks-action-system --set-string 'serviceAccount.name=bad\nhostPID: true' >/dev/null 2>&1; then \
		echo "invalid service-account name unexpectedly rendered" >&2; exit 1; \
	fi
	@if $(HELM) template verify . --namespace stacks-action-system --set-string serviceAccount.name=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa >/dev/null 2>&1; then \
		echo "overlong service-account label unexpectedly rendered" >&2; exit 1; \
	fi
