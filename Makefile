# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

BIN := contour-admission-webhook
VERSION := $(shell git describe --exact-match 2>/dev/null || basename $$(git describe --all --long 2>/dev/null))

GO_BUILD_LDFLAGS := \
	-s \
	-w

# Image URL to use all building/pushing image targets
IMG ?= $(BIN):$(VERSION)

## Tool Binaries
ENVTEST ?= $(LOCALBIN)/setup-envtest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.29.0

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

all: build

.PHONY: test
test: check

.PHONY: check
check: fmt vet lint envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out -race

.PHONY: test-unit
test-unit: fmt vet
	go test ./internal/cache/... ./internal/config/... ./pkg/utils/... -v -race -coverprofile cover-unit.out

.PHONY: test-integration
test-integration: fmt vet envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./internal/controller/... ./internal/webhook/... -v -race -coverprofile cover-integration.out

.PHONY: bench
bench:
	go test ./... -bench=. -benchmem -run=^$$ 2>/dev/null || true

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: build
build: fmt vet lint
	go build -mod=readonly -ldflags "$(GO_BUILD_LDFLAGS)" -a -o bin/$(BIN) cmd/main.go

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.0 run -v --timeout=5m

.PHONY: docker-build
docker-build:
	docker build -t ${IMG} -f Dockerfile .

.PHONY: docker-push
docker-push:
	docker push ${IMG}

.PHONY: clean
clean:
	@rm -rf cover.out cover-unit.out cover-integration.out
	@rm -rf bin

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: verify
verify: tidy fmt vet
	git diff --exit-code

.PHONY: cover
cover: check
	go tool cover -html=cover.out -o cover.html
	@echo "Coverage report generated: cover.html"

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all            - Build the binary (default)"
	@echo "  build          - Build the binary"
	@echo "  test           - Run all tests with coverage"
	@echo "  test-unit      - Run unit tests only"
	@echo "  test-integration - Run integration tests only"
	@echo "  bench          - Run benchmarks"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run golangci-lint"
	@echo "  tidy           - Run go mod tidy"
	@echo "  verify         - Verify no uncommitted changes after tidy/fmt/vet"
	@echo "  cover          - Generate HTML coverage report"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-push    - Push Docker image"
	@echo "  clean          - Remove build artifacts"
	@echo "  help           - Show this help"
