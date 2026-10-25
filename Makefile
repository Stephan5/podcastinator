BINARY   := podcastinator
BUILD_DIR := build
MAIN     := ./cmd/podcastinator

# Default target
.DEFAULT_GOAL := build

# ── Build ──────────────────────────────────────────────────────────────────────

.PHONY: build
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) $(MAIN)

# Cross-compile helpers
.PHONY: build-linux build-darwin-arm64 build-darwin-amd64
build-linux:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-linux-amd64 $(MAIN)

build-darwin-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 $(MAIN)

build-darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 $(MAIN)

# ── Test ───────────────────────────────────────────────────────────────────────

.PHONY: test
test:
	go test -v -count=1 ./internal/... ./cmd/podcastinator/...

# Integration tests in test/ build and invoke the binary as a subprocess
.PHONY: test-integration
test-integration: build
	go test -v -count=1 ./test/...

# Run everything
.PHONY: test-all
test-all: test test-integration

# Run tests without verbose output (for CI / quick feedback)
.PHONY: test-short
test-short:
	go test -count=1 ./internal/... ./cmd/podcastinator/...

# Run tests with race detector
.PHONY: test-race
test-race:
	go test -race -count=1 ./internal/... ./cmd/podcastinator/...

# ── Code quality ───────────────────────────────────────────────────────────────

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint: fmt vet

# ── Dependencies ───────────────────────────────────────────────────────────────

.PHONY: tidy
tidy:
	go mod tidy

# ── Clean ──────────────────────────────────────────────────────────────────────

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

# ── Help ───────────────────────────────────────────────────────────────────────

.PHONY: help
help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Build:"
	@echo "  build              Build binary to $(BUILD_DIR)/$(BINARY)"
	@echo "  build-linux        Cross-compile for linux/amd64"
	@echo "  build-darwin-arm64 Cross-compile for darwin/arm64"
	@echo "  build-darwin-amd64 Cross-compile for darwin/amd64"
	@echo ""
	@echo "Test:"
	@echo "  test               Run unit tests (internal/... cmd/podcastinator/...) with verbose output"
	@echo "  test-integration   Build binary then run integration tests in test/"
	@echo "  test-all           Run unit tests + integration tests"
	@echo "  test-short         Run unit tests (quiet)"
	@echo "  test-race          Run unit tests with race detector"
	@echo ""
	@echo "Code quality:"
	@echo "  fmt                Run go fmt"
	@echo "  vet                Run go vet"
	@echo "  lint               Run fmt + vet"
	@echo ""
	@echo "Other:"
	@echo "  tidy               Run go mod tidy"
	@echo "  clean              Remove $(BUILD_DIR)/"

