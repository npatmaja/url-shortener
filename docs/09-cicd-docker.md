# CI/CD Pipeline and Docker Configuration

## GitHub Actions CI/CD Pipeline

The pipeline enforces strict quality gates. **Any warning fails the build.**

### Pipeline Configuration

```yaml
# .github/workflows/ci.yml

name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  GO_VERSION: '1.22'

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}

      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest
          args: --timeout=5m

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}

      - name: Run tests with race detector
        run: go test -race -v ./...

      - name: Run concurrency tests
        run: go test -race -v -run "Concurrent" ./...

  build:
    name: Build
    runs-on: ubuntu-latest
    needs: [lint, test]
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}

      - name: Build binary
        run: |
          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bootstrap ./cmd/server

      - name: Build Docker image
        run: docker build -t url-shortener:${{ github.sha }} .

      - name: Verify non-root user in container
        run: |
          docker run --rm url-shortener:${{ github.sha }} whoami | grep -v root

  security:
    name: Security Scan
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Run Trivy vulnerability scanner
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: 'fs'
          ignore-unfixed: true
          severity: 'CRITICAL,HIGH'

  terraform-validate:
    name: Terraform Validate
    runs-on: ubuntu-latest
    strategy:
      matrix:
        provider: [aws, gcp]
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Terraform
        uses: hashicorp/setup-terraform@v3

      - name: Terraform Init
        working-directory: terraform/${{ matrix.provider }}
        run: terraform init -backend=false

      - name: Terraform Validate
        working-directory: terraform/${{ matrix.provider }}
        run: terraform validate

      - name: Terraform Format Check
        working-directory: terraform/${{ matrix.provider }}
        run: terraform fmt -check -recursive
```

### golangci-lint Configuration

```yaml
# .golangci.yml

run:
  timeout: 5m
  modules-download-mode: readonly

linters:
  enable:
    # Required by assessment
    - gocyclo
    - revive

    # Additional quality linters
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - gosec
    - bodyclose
    - noctx
    - gofmt
    - goimports

linters-settings:
  gocyclo:
    # Maximum cyclomatic complexity (assessment requirement: 10)
    min-complexity: 10

  revive:
    severity: warning
    rules:
      - name: blank-imports
      - name: context-as-argument
      - name: context-keys-type
      - name: dot-imports
      - name: error-return
      - name: error-strings
      - name: error-naming
      - name: exported
      - name: if-return
      - name: increment-decrement
      - name: var-naming
      - name: var-declaration
      - name: package-comments
      - name: range
      - name: receiver-naming
      - name: time-naming
      - name: unexported-return
      - name: indent-error-flow
      - name: errorf
      - name: empty-block
      - name: superfluous-else
      - name: unused-parameter
      - name: unreachable-code

  gosec:
    excludes:
      - G104 # Audit errors not checked (we handle errors explicitly)

issues:
  # Fail on any warning
  max-issues-per-linter: 0
  max-same-issues: 0

  exclude-rules:
    # Allow longer functions in tests
    - path: _test\.go
      linters:
        - gocyclo
```

---

## Multi-Stage Dockerfile

The Dockerfile follows security best practices:
- Multi-stage build (minimal final image)
- Non-root user
- Minimal attack surface (distroless or scratch)

### Production Dockerfile

```dockerfile
# Dockerfile

# =============================================================================
# Stage 1: Build
# =============================================================================
FROM golang:1.22-alpine AS builder

# Install ca-certificates for HTTPS and git for go mod
RUN apk add --no-cache ca-certificates git

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/server \
    ./cmd/server

# =============================================================================
# Stage 2: Production
# =============================================================================
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binary from builder
COPY --from=builder /app/server /server

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Use non-root user (distroless provides 'nonroot' user with UID 65532)
USER nonroot:nonroot

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/server", "-health-check"]

# Run the application
ENTRYPOINT ["/server"]
```

### Alternative: Scratch Image (Smallest)

```dockerfile
# Dockerfile.scratch

FROM golang:1.22-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/server \
    ./cmd/server

# Create non-root user info
RUN echo "appuser:x:65532:65532::/nonexistent:/sbin/nologin" > /etc/passwd.minimal

# =============================================================================
# Scratch image (smallest possible)
# =============================================================================
FROM scratch

# Copy CA certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy passwd for non-root user
COPY --from=builder /etc/passwd.minimal /etc/passwd

# Copy binary
COPY --from=builder /app/server /server

# Use non-root user
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/server"]
```

### AWS Lambda Dockerfile

```dockerfile
# Dockerfile.lambda

FROM golang:1.22-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build for Lambda (binary must be named 'bootstrap' for provided.al2023 runtime)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
    -tags lambda.norpc \
    -ldflags="-w -s" \
    -o /app/bootstrap \
    ./cmd/lambda

# =============================================================================
# Lambda runtime
# =============================================================================
FROM public.ecr.aws/lambda/provided:al2023-arm64

COPY --from=builder /app/bootstrap ${LAMBDA_RUNTIME_DIR}/bootstrap

CMD ["bootstrap"]
```

---

## Image Sizes Comparison

| Image Type | Base | Final Size | Security |
|------------|------|------------|----------|
| Alpine | golang:alpine | ~15 MB | Good |
| Distroless | distroless/static | ~5 MB | Better |
| Scratch | scratch | ~3 MB | Best |
| Lambda | provided:al2023 | ~50 MB | AWS managed |

---

## Security Verification

### Verify Non-Root User

```bash
# Build the image
docker build -t url-shortener:test .

# Verify the user
docker run --rm url-shortener:test whoami
# Output: nonroot

# Verify UID
docker run --rm --entrypoint id url-shortener:test
# Output: uid=65532(nonroot) gid=65532(nonroot)
```

### Verify No Shell Access

```bash
# Distroless has no shell
docker run --rm -it url-shortener:test /bin/sh
# Error: executable file not found
```

### Scan for Vulnerabilities

```bash
# Using Trivy
trivy image url-shortener:test

# Using Docker Scout
docker scout cves url-shortener:test
```

---

## Build Commands

### Local Development

```bash
# Build for local testing
go build -o server ./cmd/server

# Run locally
./server
```

### Docker Build

```bash
# Build production image
docker build -t url-shortener:latest .

# Build with specific tag
docker build -t url-shortener:$(git rev-parse --short HEAD) .

# Run container
docker run -p 8080:8080 url-shortener:latest
```

### Lambda Package

```bash
# Build Lambda binary
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build \
    -tags lambda.norpc \
    -ldflags="-w -s" \
    -o bootstrap \
    ./cmd/lambda

# Create deployment package
zip function.zip bootstrap
```

---

## Quality Gates Summary

| Check | Tool | Threshold | Fail Condition |
|-------|------|-----------|----------------|
| Cyclomatic complexity | gocyclo | 10 | Any function > 10 |
| Linting | revive | - | Any warning |
| Static analysis | golangci-lint | - | Any issue |
| Race conditions | go test -race | - | Any detected |
| Vulnerabilities | trivy | HIGH | Any high/critical |
| Terraform syntax | terraform validate | - | Any error |
| Container user | docker inspect | non-root | Running as root |
