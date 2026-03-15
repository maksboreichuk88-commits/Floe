.PHONY: dev test build lint fuzz bench clean release demo

BINARY_NAME := floe
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath

## Development
dev: ## Run in development mode with hot reload
	go run ./cmd/floe start --dev

demo: ## Run interactive demo with mock providers
	go run ./cmd/floe demo

## Testing
test: ## Run all tests with race detector
	go test -race -coverprofile=coverage.out ./...
	@echo "Coverage:"
	@go tool cover -func=coverage.out | tail -1

fuzz: ## Run fuzz tests for 60 seconds
	go test -fuzz=FuzzParseWorkflow -fuzztime=60s ./internal/workflow/
	go test -fuzz=FuzzLoadConfig -fuzztime=60s ./internal/config/

bench: ## Run benchmarks
	go test -bench=. -benchmem ./internal/gateway/

## Quality
lint: ## Run linters
	golangci-lint run ./...

sec: ## Run security scanners
	gosec ./...
	trivy fs .

## Build
build: ## Build static binary for current platform
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY_NAME) ./cmd/floe

build-all: ## Cross-compile for all platforms
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/floe
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/floe
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/floe
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/floe
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/floe

## Release
release: ## Create release with goreleaser
	goreleaser release --clean

## Cleanup
clean: ## Remove build artifacts
	rm -rf bin/ coverage.out

## Help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
