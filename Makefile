IMAGE ?= chromedp-container-mcp:latest
BIN   ?= bin/chromedp-container-mcp
PORT  ?= 8080

.PHONY: build
build: ## Build the server binary (requires local Chrome to run)
	go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/server

ZSCALER_CERT := $(HOME)/.zscaler/certs.pem

certs.pem: ## Copy Zscaler CA cert for Docker builds (auto-skipped if not present)
	@if [ -f "$(ZSCALER_CERT)" ]; then \
		cp "$(ZSCALER_CERT)" certs.pem; \
		echo "Copied certs.pem from $(ZSCALER_CERT)"; \
	else \
		touch certs.pem; \
		echo "No Zscaler cert found, created empty certs.pem"; \
	fi

.PHONY: docker-build
docker-build: certs.pem ## Build the all-in-one container image
	docker build --secret id=ca_cert,src=certs.pem -t $(IMAGE) .

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
	rm -rf bin out certs.pem

.PHONY: help
help: ## Show available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
