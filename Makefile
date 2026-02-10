# Makefile for agentusage
#
# This Makefile provides a comprehensive set of targets for building, testing,
# and deploying the agentusage application.

# ==============================================================================
# Variables
# ==============================================================================

# Application information
APP_NAME    := agentusage
MODULE      := github.com/janekbaraniewski/agentusage
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  := $(shell date +%Y-%m-%dT%H:%M:%S%z)

# Directories
BIN_DIR     := bin
CMD_DIR     := cmd/agentusage
CHART_DIR   := charts/agentusage
DIST_DIR    := dist

# Go settings
GO          := go
GOFLAGS     :=
LDFLAGS     := -s -w 
	           -X '$(MODULE)/internal/version.Version=$(VERSION)' 
	           -X '$(MODULE)/internal/version.CommitHash=$(COMMIT_HASH)' 
	           -X '$(MODULE)/internal/version.BuildDate=$(BUILD_DATE)'

# Tools
GOLANGCI_LINT := golangci-lint
HELM          := helm
DOCKER        := docker

# ==============================================================================
# Targets
# ==============================================================================

.PHONY: all
all: clean lint test build ## Run clean, lint, test, and build

.PHONY: help
help: ## Display this help screen
	@awk 'BEGIN {FS = ":.*##"; printf "
Usage:
  make \033[36m<target>\033[0m

Targets:
"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s
", $$1, $$2 }' $(MAKEFILE_LIST)

# ------------------------------------------------------------------------------
# Development
# ------------------------------------------------------------------------------

.PHONY: deps
deps: ## Download Go module dependencies
	$(GO) mod download
	$(GO) mod verify

.PHONY: tidy
tidy: ## Tidy Go module dependencies
	$(GO) mod tidy

.PHONY: fmt
fmt: ## Format Go source code
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## Run linter (golangci-lint)
	@if command -v $(GOLANGCI_LINT) >/dev/null 2>&1; then 
		echo "Running $(GOLANGCI_LINT)..."; 
		$(GOLANGCI_LINT) run ./...; 
	else 
		echo "Warning: $(GOLANGCI_LINT) not found. Skipping linting."; 
		echo "To install: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$$(go env GOPATH)/bin v1.61.0"; 
	fi

.PHONY: test
test: ## Run unit tests with coverage
	$(GO) test $(GOFLAGS) -race -coverprofile=coverage.out -covermode=atomic ./...

.PHONY: test-verbose
test-verbose: ## Run unit tests with verbose output
	$(GO) test $(GOFLAGS) -v -race ./...

.PHONY: run
run: ## Run the application locally
	$(GO) run $(CMD_DIR)/main.go

# ------------------------------------------------------------------------------
# Build
# ------------------------------------------------------------------------------

.PHONY: build
build: deps ## Build the binary
	@echo "Building $(APP_NAME) $(VERSION)..."
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME) $(CMD_DIR)

.PHONY: clean
clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out

# ------------------------------------------------------------------------------
# Docker
# ------------------------------------------------------------------------------

.PHONY: docker-build
docker-build: ## Build Docker image
	$(DOCKER) build -t $(APP_NAME):$(VERSION) .
	$(DOCKER) tag $(APP_NAME):$(VERSION) $(APP_NAME):latest

.PHONY: docker-run
docker-run: ## Run Docker container
	$(DOCKER) run -it --rm $(APP_NAME):latest

# ------------------------------------------------------------------------------
# Helm
# ------------------------------------------------------------------------------

.PHONY: helm-lint
helm-lint: ## Lint Helm chart
	@if [ -d "$(CHART_DIR)" ]; then 
		$(HELM) lint $(CHART_DIR); 
	else 
		echo "Helm chart directory $(CHART_DIR) does not exist. Skipping."; 
	fi

.PHONY: helm-template
helm-template: ## Render Helm chart templates locally
	@if [ -d "$(CHART_DIR)" ]; then 
		$(HELM) template $(APP_NAME) $(CHART_DIR); 
	else 
		echo "Helm chart directory $(CHART_DIR) does not exist. Skipping."; 
	fi

.PHONY: helm-package
helm-package: ## Package Helm chart
	@mkdir -p $(DIST_DIR)
	@if [ -d "$(CHART_DIR)" ]; then 
		$(HELM) package $(CHART_DIR) --version $(VERSION) --app-version $(VERSION) -d $(DIST_DIR); 
	else 
		echo "Helm chart directory $(CHART_DIR) does not exist. Skipping."; 
	fi

.PHONY: helm-install
helm-install: ## Install/Upgrade Helm chart locally
	@if [ -d "$(CHART_DIR)" ]; then 
		$(HELM) upgrade --install $(APP_NAME) $(CHART_DIR) --set image.tag=$(VERSION); 
	else 
		echo "Helm chart directory $(CHART_DIR) does not exist. Skipping."; 
	fi

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall Helm release
	$(HELM) uninstall $(APP_NAME)
