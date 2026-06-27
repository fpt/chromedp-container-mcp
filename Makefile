IMAGE ?= chromedp-container-mcp:latest
BIN   ?= bin/chromedp-container-mcp
PORT  ?= 8080

.PHONY: build
build: ## Build the server binary (requires local Chrome to run)
	go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/server

.PHONY: docker-build
docker-build: ## Build the all-in-one container image
	docker build -t $(IMAGE) .

.PHONY: docker-run
docker-run: docker-build ## Build then run the container
	docker run --rm --init --shm-size 1g -p $(PORT):8080 \
		-e MCP_BASE_URL=http://localhost:$(PORT) $(IMAGE)

.PHONY: tidy
tidy: ## Sync go.mod / go.sum
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin out

.PHONY: help
help: ## Show available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
