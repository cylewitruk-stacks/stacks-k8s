# Optional backend installation is independent of network and observation releases.
HELM ?= helm
KUBECTL ?= kubectl
NAMESPACE ?= stacks-observation-system
.PHONY: install uninstall
install:
	$(KUBECTL) get namespace $(NAMESPACE) >/dev/null
	$(KUBECTL) get secret greptime-auth --namespace $(NAMESPACE) >/dev/null
	@set -eu; archive_dir="$$(mktemp -d)"; trap 'rm -rf "$$archive_dir"' EXIT; \
		$(HELM) pull greptimedb-standalone --repo https://greptimeteam.github.io/helm-charts/ --version 0.4.14 --destination "$$archive_dir"; \
		printf '%s  %s\n' 9c5cbd8f1fe523965bb94c17cd211f9d6251bad182a5ac4d4e37e67c2ba534b5 "$$archive_dir/greptimedb-standalone-0.4.14.tgz" | shasum -a 256 --check; \
		$(HELM) upgrade --install greptime "$$archive_dir/greptimedb-standalone-0.4.14.tgz" --namespace $(NAMESPACE) --values greptime-values.yaml --wait --timeout 5m
uninstall:
	$(HELM) uninstall greptime --namespace $(NAMESPACE) --wait
	@echo 'Database PVC is retained. Delete it separately only when retained telemetry is no longer needed.'
