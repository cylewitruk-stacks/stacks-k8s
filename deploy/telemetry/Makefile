# Optional local operator + Greptime installation; ordinary operator installs can use an external backend.
HELM ?= helm
KUBECTL ?= kubectl
NAMESPACE ?= stacks-observation-system
RELEASE ?= telemetry
CHART := ../../charts/stacks-observability-operator
HELM_ARGS ?=
.PHONY: install uninstall
install:
	$(MAKE) -C $(CHART) dependencies
	$(KUBECTL) get namespace $(NAMESPACE) >/dev/null
	$(KUBECTL) get secret greptime-auth --namespace $(NAMESPACE) >/dev/null
	$(HELM) upgrade --install $(RELEASE) $(CHART) --namespace $(NAMESPACE) \
		--values $(CHART)/values-local.yaml $(HELM_ARGS) --wait --timeout 5m
uninstall:
	$(HELM) uninstall $(RELEASE) --namespace $(NAMESPACE) --wait
	@echo 'Operator and bundled backend removed. Database PVC is retained; cluster destruction deletes it.'
