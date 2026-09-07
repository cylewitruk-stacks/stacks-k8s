# Thin lifecycle helpers for the pinned local kind cluster.
.DEFAULT_GOAL := help
export KIND_EXPERIMENTAL_PROVIDER := docker

.PHONY: help create start stop destroy chaos-install
help:
	@echo 'Use create, start, stop, chaos-install, or destroy (deletes all cluster data).'

create:
	@umask 077; kind create cluster --name stacks-k8s --config kind.yaml --kubeconfig kubeconfig --wait 120s

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
