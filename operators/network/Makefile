GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)
CONTROLLER_GEN ?= $(GO_ISOLATED) run -modfile=tools/go.mod sigs.k8s.io/controller-tools/cmd/controller-gen
CRD_DIR ?= ../../charts/stacks-network-operator/crds

.PHONY: fmt generate test test-integration test-live test-race vet verify

fmt:
	@test -z "$$(gofmt -l .)" || { echo "Go sources are not formatted" >&2; exit 1; }

generate:
	$(CONTROLLER_GEN) object paths=./api/...
	$(CONTROLLER_GEN) crd paths=./api/... output:crd:artifacts:config=$(CRD_DIR)

test:
	$(GO_ISOLATED) test ./...

test-integration:
	$(GO_ISOLATED) test -tags=integration -count=1 ./internal/integration

test-live:
	$(GO_ISOLATED) test -tags=live -count=1 -v ./internal/integration

test-race:
	$(GO_ISOLATED) test -race ./...

vet:
	$(GO_ISOLATED) vet ./...

verify: fmt generate vet test test-race test-integration
	@git diff --exit-code -- api/v1alpha1/zz_generated.deepcopy.go $(CRD_DIR)/network.stacks.org_*.yaml
