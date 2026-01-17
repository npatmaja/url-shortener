.PHONY: lint test test-race build clean docker-build docker-build-scratch docker-run docker-verify docker-scan

lint:
	golangci-lint run ./...

test:
	go test -v ./...

test-race:
	go test -race -v ./...

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="-w -s" -o bin/server ./cmd/server

clean:
	rm -rf bin/
	go clean

# Docker
DOCKER_IMAGE := url-shortener
DOCKER_TAG := $(shell git rev-parse --short HEAD 2>/dev/null || echo "latest")

docker-build: ## Build Docker image
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_IMAGE):latest

docker-build-scratch: ## Build Docker image using scratch base
	docker build -f Dockerfile.scratch -t $(DOCKER_IMAGE):$(DOCKER_TAG)-scratch .

docker-run: docker-build ## Run Docker container
	docker run -p 8080:8080 $(DOCKER_IMAGE):$(DOCKER_TAG)

docker-verify: docker-build ## Verify Docker image security
	@echo "Verifying non-root user..."
	@docker run --rm --entrypoint="" $(DOCKER_IMAGE):$(DOCKER_TAG) cat /etc/passwd | grep -q nonroot && echo "OK: Running as non-root" || (echo "FAIL: Running as root" && exit 1)
	@echo "Checking image size..."
	@docker images $(DOCKER_IMAGE):$(DOCKER_TAG) --format "Size: {{.Size}}"

docker-scan: docker-build ## Scan Docker image for vulnerabilities
	trivy image $(DOCKER_IMAGE):$(DOCKER_TAG)
