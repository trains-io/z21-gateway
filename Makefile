APP_NAME := z21-gateway
BUILD := build
PKG := ./...

KO_IMAGE_TAG := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_SHA := $(shell git rev-parse --short HEAD)
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

KPT_FN_APPLY_SETTERS_IMG := ghcr.io/kptdev/krm-functions-catalog/apply-setters:v0.2

LOCAL_CLUSTER_NAME := dev

LDFLAGS := -X main.version=$(KO_IMAGE_TAG) \
		   -X main.commit=$(GIT_SHA) \
		   -X main.date=$(BUILD_DATE)

GO      ?= go
KO      ?= ko
KIND    ?= kind
K       ?= kubectl
HELM    ?= helm
KPT     ?= /home/pbe/go/bin/kpt

.PHONY: all
all: fmt vet build ## Run fmt, vet, and build

.PHONY: fmt
fmt: ## Format Go source code
	@echo "Formatting Go code ..."
	$(GO) fmt $(PKG)

.PHONY: vet
vet: ## Run go vet for static analysis
	@echo "Running go vet ..."
	$(GO) vet $(PKG)

.PHONY: build
build: ## Build the binary
	@echo "Building $(APP_NAME) ($(VERSION))"
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD)/$(APP_NAME) $(PKG)

.PHONY: setup
setup: ## Create a local KinD cluster
	@echo "Creating local cluster \"$(LOCAL_CLUSTER_NAME)\" ..."
	@$(KIND) create cluster --name $(LOCAL_CLUSTER_NAME) 2>/dev/null || true
	@$(K) config use-context kind-$(LOCAL_CLUSTER_NAME)
	@echo "Installing NATS ..."
	@$(HELM) repo add nats https://nats-io.github.io/k8s/helm/charts/ || true
	@$(HELM) install nats nats/nats 2>/dev/null || true

.PHONY: teardown
teardown: ## Delete local KinD cluster
	@$(KIND) delete cluster -n $(LOCAL_CLUSTER_NAME) || true

.PHONY: manifest
manifest: ## Build k8s manifests for local deployment
	@echo "Building manifests with kpt ..."
	@rm -rf $(BUILD)/manifests
	$(KPT) fn eval manifests/ \
		--image $(KPT_FN_APPLY_SETTERS_IMG) \
		-o $(BUILD)/manifests \
		-- z21-name=main \
		   z21-addr=${Z21_ADDR} \
		   nats-url=${NATS_URL}

.PHONY: deploy
deploy: manifest ## Build, containerize, and deploy locally
	@echo "Building, containerizing and deploy to KinD with ko ..."
	KO_IMAGE_TAG=$(KO_IMAGE_TAG) \
	GIT_SHA=$(GIT_SHA) \
	BUILD_DATE=$(BUILD_DATE) \
	KO_DOCKER_REPO=kind.local \
	KIND_CLUSTER_NAME=$(LOCAL_CLUSTER_NAME) \
	$(KO) apply -f $(BUILD)/manifests/z21-gateway.yaml

.PHONY: clean
clean: ## Remove build artifacts
	@echo "Cleaning up $(BUILD) ..."
	@rm -rf $(BUILD)

.PHONY: run
run: build ## Run the application locally
	@echo "Running $(APP_NAME)"
	./$(BUILD)/$(APP_NAME)

.PHONY: help
help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-10s %s\n", $$1, $$2}'
