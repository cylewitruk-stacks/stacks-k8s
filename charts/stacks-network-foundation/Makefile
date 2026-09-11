HELM ?= helm
.PHONY: verify
verify:
	$(HELM) lint . --kube-version 1.37.0
	$(HELM) template foundation . --namespace foundation-system --kube-version 1.37.0 >/dev/null
	@if $(HELM) template foundation . --kube-version 1.36.0 >/dev/null 2>&1; then echo "unsupported Kubernetes version rendered" >&2; exit 1; fi
	GOWORK=off go -C ../../operators/network test ./internal/foundation -run TestFoundationChart
