HELM ?= helm
GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)
OPERATOR_DIR ?= ../../operators/action
CHART_POLICY_DIR ?= ../../tools/chart-policy

.PHONY: verify helm-verify rbac-verify workload-verify
verify: helm-verify rbac-verify workload-verify
	$(MAKE) -C $(OPERATOR_DIR) verify

helm-verify:
	$(HELM) lint . --kube-version 1.37.0
	$(HELM) template action . --namespace lab --kube-version 1.37.0 >/dev/null
	$(HELM) template action . --namespace lab --kube-version 1.37.0 --set bitcoinGeneration.enabled=false --set bitcoinReorganization.enabled=true >/dev/null
	@if $(HELM) template action . --kube-version 1.37.0 --set bitcoinGeneration.enabled=false --set bitcoinReorganization.enabled=false >/dev/null 2>&1; then echo "empty action installation rendered" >&2; exit 1; fi
	@if $(HELM) template action . --kube-version 1.36.0 >/dev/null 2>&1; then echo "unsupported Kubernetes version rendered" >&2; exit 1; fi

rbac-verify:
	$(GO_ISOLATED) -C $(OPERATOR_DIR) test ./controllers/foundation -run '^TestActionFoundationChartExactPermissions$$' -count=1

workload-verify:
	$(HELM) template action . --namespace lab --kube-version 1.37.0 | $(GO_ISOLATED) -C $(CHART_POLICY_DIR) run ./cmd/chart-policy-check
