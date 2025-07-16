# Nexus Makefile

# Variables
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint
GOCOVER=$(GOCMD) tool cover

# Build variables
BINARY_NAME=nexus
VERSION?=$(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Test variables
COVERAGE_FILE=coverage.out
COVERAGE_HTML=coverage.html

# Directories
PKG_LIST=$(shell go list ./... | grep -v /vendor/)

.PHONY: all build test clean deps lint fmt vet coverage bench help

# Default target
all: deps lint test build

# Build the project (library verification)
build:
	@echo "Building library..."
	$(GOBUILD) -v ./...

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -short ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	$(GOCOVER) -func=$(COVERAGE_FILE)

# Generate coverage HTML report
coverage-html: test-coverage
	@echo "Generating coverage HTML report..."
	$(GOCOVER) -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -tags=integration ./...

# Run all tests (unit + integration)
test-all: test test-integration

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Install dependencies
deps:
	@echo "Installing dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Install development tools
install-tools:
	@echo "Installing development tools..."
	@which $(GOLINT) > /dev/null || (echo "Installing golangci-lint..." && curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin)
	@echo "Tools installed!"

# Run golangci-lint
lint: install-tools
	@echo "Running linters..."
	$(GOLINT) run --timeout=5m --enable=errcheck,gosimple,govet,ineffassign,staticcheck,unused,gofmt,goimports,misspell,revive,gosec,unconvert,unparam,bodyclose,noctx,gocritic,gocyclo,dupl --disable=godox --tests ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .
	$(GOMOD) tidy

# Run go vet
vet:
	@echo "Running go vet..."
	$(GOCMD) vet ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@rm -f $(COVERAGE_FILE)
	@rm -f $(COVERAGE_HTML)
	@rm -rf dist/

# Check if code needs formatting
check-fmt:
	@echo "Checking formatting..."
	@test -z "$$($(GOFMT) -l . | tee /dev/stderr)" || (echo "Code needs formatting. Run 'make fmt'" && exit 1)

# Run security check
sec: install-tools
	@echo "Running security scan..."
	@which gosec > /dev/null || go install github.com/securego/gosec/v2/cmd/gosec@latest
	gosec -quiet ./...

# Update dependencies
update-deps:
	@echo "Updating dependencies..."
	$(GOGET) -u ./...
	$(GOMOD) tidy

# Pre-commit checks
pre-commit: fmt lint test

# CI checks (what runs in CI/CD)
ci: deps check-fmt lint vet test-coverage

# Library check - ensure all packages compile
check-lib:
	@echo "Checking library compilation..."
	$(GOBUILD) -v ./...

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t nexus:$(VERSION) .

# Help target
help:
	@echo "Nexus Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all              Run deps, lint, test, and build"
	@echo "  build            Build the project"
	@echo "  test             Run unit tests"
	@echo "  test-coverage    Run tests with coverage"
	@echo "  coverage-html    Generate HTML coverage report"
	@echo "  test-integration Run integration tests"
	@echo "  test-all         Run all tests"
	@echo "  bench            Run benchmarks"
	@echo "  deps             Install dependencies"
	@echo "  install-tools    Install development tools"
	@echo "  lint             Run golangci-lint"
	@echo "  fmt              Format code"
	@echo "  vet              Run go vet"
	@echo "  clean            Clean build artifacts"
	@echo "  check-fmt        Check if code needs formatting"
	@echo "  sec              Run security scan"
	@echo "  update-deps      Update dependencies"
	@echo "  pre-commit       Run pre-commit checks"
	@echo "  ci               Run CI checks"
	@echo "  check-lib        Check library compilation"
	@echo "  docker-build     Build Docker image"
	@echo "  help             Show this help message"
