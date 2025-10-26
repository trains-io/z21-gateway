APP_NAME := z21-gateway
BUILD_DIR := bin
PKG := ./...

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT_HASH := $(shell git rev-parse --short HEAD)
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -X main.version=$(VERSION) \
		   -X main.commit=$(COMMIT_HASH) \
		   -X main.date=$(BUILD_DATE)

GO ?= go
KO ?= ko

.PHONY: all
all: fmt vet build ## Run fmt, vet, and build

.PHONY: fmt
fmt: ## Format Go source code
	@echo "Formatting Go code ..."
	@$(GO) fmt $(PKG)

.PHONY: vet
vet: ## Run go vet for static analysis
	@echo "Running go vet ..."
	@$(GO) vet $(PKG)

.PHONY: build
build: ## Build the binary
	@echo "Building $(APP_NAME) ($(VERSION))"
	@$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) $(PKG)

.PHONY: image
image: ## Build, containerize, and deploy locally
	@echo "Building, containerizing and deploy to KinD with ko ..."
	@$(KO) build --local --bare --tags $(VERSION) --ldflags "$(LDFLAGS)"

.PHONY: clean
clean: ## Remove build artifacts
	@echo "Cleaning up $(BUILD_DIR) ..."
	@rm -rf $(BUILD_DIR)

.PHONY: run
run: build ## Run the application locally
	@echo "Running $(APP_NAME)"
	@./$(BUILD_DIR)/$(APP_NAME)

.PHONY: help
help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-10s %s\n", $$1, $$2}'
