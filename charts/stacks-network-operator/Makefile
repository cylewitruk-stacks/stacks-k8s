HELM ?= helm
.PHONY: verify helm-verify rbac-verify workload-verify
verify: helm-verify rbac-verify workload-verify
	$(MAKE) -C ../../operators/network verify

helm-verify:
	$(HELM) lint . --kube-version 1.37.0
	$(HELM) template foundation . --namespace foundation-system --kube-version 1.37.0 >/dev/null
	@if $(HELM) template foundation . --kube-version 1.36.0 >/dev/null 2>&1; then echo "unsupported Kubernetes version rendered" >&2; exit 1; fi

rbac-verify:
	GOWORK=off go -C ../../operators/network test ./internal/foundation -run TestFoundationChart

workload-verify:
	$(HELM) template foundation . --namespace foundation-system --kube-version 1.37.0 | GOWORK=off go -C ../../tools/chart-policy run ./cmd/chart-policy-check
