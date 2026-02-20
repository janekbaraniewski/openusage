APP_NAME    := openusage
MODULE      := github.com/janekbaraniewski/openusage
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  := $(shell date +%Y-%m-%dT%H:%M:%S%z)

BIN_DIR     := bin
CMD_DIR     := ./cmd/openusage

GO          := go
GOFLAGS     :=
LDFLAGS     := -s -w \
               -X '$(MODULE)/internal/version.Version=$(VERSION)' \
               -X '$(MODULE)/internal/version.CommitHash=$(COMMIT_HASH)' \
               -X '$(MODULE)/internal/version.BuildDate=$(BUILD_DATE)'

GOLANGCI_LINT := golangci-lint

.PHONY: all
all: clean lint test build

.PHONY: help
help: ## Display this help screen
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

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
	@if command -v $(GOLANGCI_LINT) >/dev/null 2>&1; then \
		$(GOLANGCI_LINT) run ./...; \
	else \
		echo "Warning: $(GOLANGCI_LINT) not found, skipping."; \
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

.PHONY: build
build: deps ## Build the binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME) $(CMD_DIR)

.PHONY: demo
demo: deps ## Build and run the demo with dummy data (for screenshots)
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-demo ./cmd/demo
	$(BIN_DIR)/$(APP_NAME)-demo

.PHONY: clean
clean: ## Clean build artifacts
	@rm -rf $(BIN_DIR) dist coverage.out
