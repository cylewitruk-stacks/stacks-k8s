GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)
CONTROLLER_GEN ?= $(GO_ISOLATED) run -modfile=tools/go.mod sigs.k8s.io/controller-tools/cmd/controller-gen
ACTION_CRD_DIR ?= ../../charts/stacks-action-operator/crds
CRD_DIR ?= ../../charts/stacks-network-operator/crds
FOUNDATION_PATHS := paths=./common/v1alpha2/... paths=./v1alpha2/... paths=./bitcoin/v1alpha2/... paths=./stacks/v1alpha2/...

.PHONY: fmt generate test test-race vet verify

fmt:
	@test -z "$$(gofmt -l .)" || { echo "Go sources are not formatted" >&2; exit 1; }

generate:
	$(CONTROLLER_GEN) object $(FOUNDATION_PATHS) paths=./actions/v1alpha2/...
	$(CONTROLLER_GEN) crd $(FOUNDATION_PATHS) output:crd:artifacts:config=$(CRD_DIR)
	$(CONTROLLER_GEN) crd paths=./actions/v1alpha2/... output:crd:artifacts:config=$(ACTION_CRD_DIR)

test:
	$(GO_ISOLATED) test ./...

test-race:
	$(GO_ISOLATED) test -race ./...

vet:
	$(GO_ISOLATED) vet ./...

verify: fmt generate vet test test-race
	@git diff --exit-code -- common/v1alpha2/zz_generated.deepcopy.go v1alpha2/zz_generated.deepcopy.go bitcoin/v1alpha2/zz_generated.deepcopy.go stacks/v1alpha2/zz_generated.deepcopy.go actions/v1alpha2/zz_generated.deepcopy.go $(CRD_DIR)/*.yaml $(ACTION_CRD_DIR)/*.yaml
