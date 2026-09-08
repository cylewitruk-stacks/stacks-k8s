HELM ?= helm

.PHONY: verify helm-verify
verify: helm-verify

helm-verify:
	$(HELM) lint . --kube-version 1.37.0
	$(HELM) template profile . --namespace chaos-test >/dev/null
	$(HELM) template profile . --namespace chaos-test --set networkDelay.enabled=true --set chaosMesh.externalVersion=2.8.4 >/dev/null
	$(HELM) template profile . --namespace chaos-test --set networkPartition.enabled=true --set chaosMesh.externalVersion=2.8.4 >/dev/null
	$(HELM) template profile . --namespace chaos-test --set networkDelay.enabled=true --set networkPartition.enabled=true --set chaosMesh.externalVersion=2.8.4 >/dev/null
