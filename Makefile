# OzDDNS — build, test and packaging tasks.
BINARY  := ozddns
PKG     := github.com/echoface/ozddns
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v1.0.0)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD   ?= $(shell date -u +%Y.%m.%d)
GOFLAGS ?=
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Build=$(BUILD) \
	-X $(PKG)/internal/version.Commit=$(COMMIT)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: web-v2
web-v2: ## Build the Vue management panel into web/v2/dist
	cd web/v2 && npm ci && npm run build

.PHONY: build
build: web-v2 ## Build the single binary into ./bin/ozddns
	@mkdir -p bin
	CGO_ENABLED=0 go build $(GOFLAGS) -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/ozddns

.PHONY: install
install: ## Install the binary into $GOBIN
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/ozddns

.PHONY: run
run: build ## Run the daemon with the demo dataset on 127.0.0.1:9876
	./bin/$(BINARY) --db ./ozddns.db serve --demo

.PHONY: test
test: ## Run the unit test suite
	go test ./...

.PHONY: test-race
test-race: ## Run the tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Write a coverage report to coverage.out
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format the Go sources
	gofmt -w $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	go mod tidy

.PHONY: docker
docker: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) --build-arg BUILD=$(BUILD) --build-arg COMMIT=$(COMMIT) -t ozddns:$(VERSION) -t ozddns:latest .

.PHONY: up
up: ## Start the container via Compose (detached)
	docker compose up -d --build

.PHONY: down
down: ## Stop the Compose stack
	docker compose down

.PHONY: logs
logs: ## Follow the container logs
	docker compose logs -f

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf bin coverage.out web/v2/dist
