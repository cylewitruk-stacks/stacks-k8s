GO ?= go
GO_ISOLATED ?= GOWORK=off $(GO)

.PHONY: fmt vet test test-race test-integration verify
fmt:
	@test -z "$$(gofmt -l .)" || { echo "Go sources are not formatted" >&2; exit 1; }
vet:
	$(GO_ISOLATED) vet ./...
test:
	$(GO_ISOLATED) test ./...
test-race:
	$(GO_ISOLATED) test -race ./...
test-integration:
	$(GO_ISOLATED) test -tags=integration -count=1 ./controllers/foundation
verify: fmt vet test test-race test-integration
