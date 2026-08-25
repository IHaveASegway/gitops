MODULE  := github.com/IHaveASegway/gitops
BINARY  := gitops
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/buildinfo.version=$(VERSION)

.DEFAULT_GOAL := help

.PHONY: all build install test test-race cover lint fmt vet tidy check clean snapshot help

all: check build ## Lint, test and build

build: ## Build ./gitops with the version stamped in
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/gitops

install: ## Install gitops into $(go env GOPATH)/bin
	go install -ldflags '$(LDFLAGS)' ./cmd/gitops

test: ## Run the test suite (needs git, no network)
	go test ./...

test-race: ## Run the test suite with the race detector
	go test -race ./...

cover: ## Produce coverage.html
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format sources (gofmt + goimports via golangci-lint)
	golangci-lint fmt

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy go.mod/go.sum
	go mod tidy

check: vet lint test ## Everything CI runs

clean: ## Remove build and coverage artifacts
	rm -rf $(BINARY) dist coverage.out coverage.html

snapshot: ## Build release archives locally without publishing
	goreleaser release --snapshot --clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
