# Thin lifecycle helpers for the pinned local kind cluster.
.DEFAULT_GOAL := help
export KIND_EXPERIMENTAL_PROVIDER := docker
HEADLAMP ?= true
METRICS_SERVER ?= true
HEADLAMP_PORT ?= 8080
KUBECTL := kubectl --kubeconfig kubeconfig --context kind-stacks-k8s
HELM := helm --kubeconfig kubeconfig --kube-context kind-stacks-k8s

.PHONY: help create start stop destroy chaos-install headlamp-install headlamp-uninstall headlamp headlamp-token metrics-install check-options
help:
	@echo 'Use create, start, stop, chaos-install, headlamp-install, headlamp-uninstall, headlamp, headlamp-token, metrics-install, or destroy (deletes all cluster data).'
	@echo 'Create defaults: HEADLAMP=true METRICS_SERVER=true. Dashboard: HEADLAMP_PORT=8080.'

check-options:
	@case '$(HEADLAMP)/$(METRICS_SERVER)' in true/true|true/false|false/true|false/false) ;; *) echo 'HEADLAMP and METRICS_SERVER must be true or false.' >&2; exit 1 ;; esac

create: check-options
	@if test '$(HEADLAMP)' = true || test '$(METRICS_SERVER)' = true; then command -v helm >/dev/null && command -v shasum >/dev/null && command -v kubectl >/dev/null; fi
	@umask 077; kind create cluster --name stacks-k8s --config kind.yaml --kubeconfig kubeconfig --wait 120s
	@if test '$(HEADLAMP)' = true; then $(MAKE) headlamp-install; elif test '$(METRICS_SERVER)' = true; then $(MAKE) metrics-install; fi

start:
	@set -eu; \
		nodes="$$(docker ps -a --filter label=io.x-k8s.kind.cluster=stacks-k8s --format '{{.ID}}')"; \
		test -n "$$nodes" || { echo 'No stacks-k8s cluster exists; run make cluster-create.' >&2; exit 1; }; \
		docker start $$nodes
	@umask 077; kind export kubeconfig --name stacks-k8s --kubeconfig kubeconfig
	@set -eu; attempts=0; \
		until kubectl --kubeconfig kubeconfig --context kind-stacks-k8s --request-timeout=5s get --raw=/readyz >/dev/null 2>&1; do \
			attempts=$$((attempts + 1)); \
			test "$$attempts" -lt 24 || { echo 'Kubernetes API did not become ready.' >&2; exit 1; }; \
			sleep 2; \
		done
	kubectl --kubeconfig kubeconfig --context kind-stacks-k8s wait --for=condition=Ready node/stacks-k8s-control-plane node/stacks-k8s-worker node/stacks-k8s-worker2 --timeout=120s

stop:
	@set -eu; \
		nodes="$$(docker ps -a --filter label=io.x-k8s.kind.cluster=stacks-k8s --format '{{.ID}}')"; \
		if test -n "$$nodes"; then docker stop --timeout 30 $$nodes; fi

# Destruction is explicit and removes the cluster's persistent data.
destroy:
	kind delete cluster --name stacks-k8s --kubeconfig kubeconfig

# Install the verified upstream chart with the repository's kind/containerd values.
chaos-install:
	@set -eu; archive_dir="$$(mktemp -d)"; trap 'rm -rf "$$archive_dir"' EXIT; \
		helm pull chaos-mesh --repo https://charts.chaos-mesh.org --version 2.8.4 --destination "$$archive_dir"; \
		printf '%s  %s\n' ae4abd385649771300e4d33a44627c0df3618be0780c385bf30cc2fdf2ad93fa "$$archive_dir/chaos-mesh-2.8.4.tgz" | shasum -a 256 -c -; \
		helm upgrade --install chaos-mesh "$$archive_dir/chaos-mesh-2.8.4.tgz" \
			--kubeconfig kubeconfig --kube-context kind-stacks-k8s \
			--namespace chaos-mesh --create-namespace --reset-values \
			--values ../../examples/chaos/upstream-values.yaml --wait --timeout 5m

# Download and verify before giving Helm an archive; remove it on every exit.
define install-chart
set -eu; archive_dir="$$(mktemp -d)"; trap 'rm -rf "$$archive_dir"' EXIT; \
	helm pull $(1) --repo $(2) --version $(3) --destination "$$archive_dir"; \
	printf '%s  %s\n' $(4) "$$archive_dir/$(1)-$(3).tgz" | shasum -a 256 -c -; \
	$(HELM) upgrade --install $(1) "$$archive_dir/$(1)-$(3).tgz" \
		--namespace $(5) --create-namespace --reset-values --values $(6) --wait --timeout 5m
endef

# Reuse externally managed metrics APIs without taking over their installation.
metrics-install:
	@set -eu; owner="$$($(KUBECTL) get apiservice v1beta1.metrics.k8s.io --ignore-not-found -o 'jsonpath={.metadata.annotations.meta\.helm\.sh/release-name}/{.metadata.annotations.meta\.helm\.sh/release-namespace}')"; \
		if test -z "$$owner" || test "$$owner" = metrics-server/kube-system; then \
			$(call install-chart,metrics-server,https://kubernetes-sigs.github.io/metrics-server/,3.14.0,c2ca1185c01e6e7f53dd1b7d131f0c9b3fa50e003ed068b784563a1b5a3422a1,kube-system,metrics-server-values.yaml); \
		else echo 'Using the existing externally managed metrics API.'; fi
	$(KUBECTL) wait --for=condition=Available apiservice/v1beta1.metrics.k8s.io --timeout=120s
	$(KUBECTL) top nodes

headlamp-install: check-options
	@if test '$(METRICS_SERVER)' = true; then $(MAKE) metrics-install; fi
	@$(call install-chart,headlamp,https://kubernetes-sigs.github.io/headlamp/,0.45.0,38c619e0ed5db4164b02c353bcbb24e8157c1d98d961b292f99d68cc9316b8eb,headlamp,headlamp-values.yaml)
	@echo 'Headlamp ready. Run make cluster-headlamp from the repository root; use make cluster-headlamp-token in another terminal to log in.'

headlamp-uninstall:
	$(HELM) uninstall headlamp --namespace headlamp --ignore-not-found --wait --timeout 5m

headlamp:
	@echo 'Open http://127.0.0.1:$(HEADLAMP_PORT) and log in with make cluster-headlamp-token. Ctrl-C stops the port-forward.'
	$(KUBECTL) --namespace headlamp port-forward --address 127.0.0.1 service/headlamp $(HEADLAMP_PORT):80

headlamp-token:
	@$(KUBECTL) --namespace headlamp create token headlamp --duration=1h
