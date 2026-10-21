BINARY  := lifeledger
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
IMAGE   ?= lifeledger:$(VERSION)
URL     ?= http://localhost:8080
LDFLAGS := -s -w \
	-X github.com/rohitshukla001/lifeledger/internal/version.Version=$(VERSION) \
	-X github.com/rohitshukla001/lifeledger/internal/version.Commit=$(COMMIT)

.PHONY: help build run test test-live cover vet fmt fmt-check check clean web-install web web-dev web-test web-check docker docker-run smoke

help: ## Show the available targets.
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: ## Build the binary into ./bin.
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/lifeledger

run: build ## Build and start the HTTP server.
	./$(BIN_DIR)/$(BINARY) serve

test: ## Run all tests with the race detector.
	go test -race -count=1 ./...

test-live: ## Call the real Token Factory and Tavily APIs (needs keys in .env, spends a few cents).
	LIFELEDGER_LIVE_TEST=1 go test -count=1 -v -run Live ./...

cover: ## Run tests and print the coverage for each package.
	go test -count=1 -cover ./...

vet: ## Run go vet.
	go vet ./...

fmt: ## Format all Go files.
	gofmt -w cmd internal

fmt-check: ## Fail if a Go file is not formatted.
	@test -z "$$(gofmt -l cmd internal)" || (echo "Run 'make fmt' on:"; gofmt -l cmd internal; exit 1)

check: fmt-check vet test ## Run all Go checks before a commit.

web-install: ## Install the web UI dependencies.
	cd web && npm ci

web: ## Build the web UI into web/dist/browser.
	cd web && npm run build

web-dev: ## Start the web UI dev server on :4200 (start "make run" first).
	cd web && npm start

web-test: ## Run the web UI unit tests.
	cd web && npm test

web-check: ## Check formatting, run the unit tests, and build the web UI.
	cd web && npm run format:check && npm test && npm run build

docker: ## Build the linux/amd64 container image (IMAGE=name:tag).
	docker build --platform linux/amd64 --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(IMAGE) .

docker-run: ## Run the image on :8080 with .env and a named volume for the database.
	docker run --rm -p 8080:8080 --env-file .env \
		-e LIFELEDGER_ADDR=:8080 -e LIFELEDGER_DB_PATH=/data/lifeledger.db -e LIFELEDGER_WEB_DIR=/srv/web \
		-v lifeledger-data:/data $(IMAGE)

smoke: ## Check a running server (URL=https://...; needs LIFELEDGER_API_TOKEN).
	scripts/smoke.sh $(URL)

clean: ## Remove build output.
	rm -rf $(BIN_DIR) web/dist
