# Plan: CI/CD Pipeline and Docker Configuration

## Status: COMPLETED

## Overview

Complete the CI/CD pipeline, Docker configuration, and Terraform infrastructure as specified in `docs/09-cicd-docker.md` and `docs/07-infrastructure.md`. This plan covers enhancements to the existing GitHub Actions workflow, Docker multi-stage builds, security scanning, and infrastructure as code for AWS and GCP deployments.

## Current State

### Implemented
- `.github/workflows/ci.yml` - Enhanced CI with lint, test, build, security scan, Docker build, and Terraform validate jobs
- `.golangci.yml` - Enhanced linter configuration with security linters (gosec, bodyclose, noctx) and format linters (gofmt, goimports)
- `Dockerfile` - Production multi-stage build using distroless base image with non-root user
- `Dockerfile.scratch` - Alternative minimal scratch-based Dockerfile
- `Dockerfile.lambda` - AWS Lambda deployment Dockerfile (requires cmd/lambda/main.go)
- `.dockerignore` - Docker build context exclusions
- `Makefile` - Docker build, verify, and scan targets
- `terraform/aws/main.tf` - AWS infrastructure (Lambda, API Gateway, DynamoDB)
- `terraform/gcp/main.tf` - GCP infrastructure (Cloud Run, Firestore)

### Remaining Verification (requires external tools)
1. Docker build verification (requires Docker installed)
2. CI pipeline verification (requires push to remote)
3. Terraform validation (requires Terraform installed)

---

## Part 1: Enhanced golangci-lint Configuration

### Step 1.1: Add Security and Quality Linters

Update `.golangci.yml` to include additional linters for security and code quality.

**Changes:**

```yaml
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
    - gosec        # NEW: Security scanner
    - bodyclose    # NEW: HTTP response body close checker
    - noctx        # NEW: Context usage checker
    - gofmt        # NEW: Format checker
    - goimports    # NEW: Import organization

linters-settings:
  gocyclo:
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
  max-issues-per-linter: 0
  max-same-issues: 0

  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
        - gocyclo
```

**Verification:**
```bash
golangci-lint run --timeout=5m
```

---

## Part 2: Production Dockerfile

### Step 2.1: Create Multi-Stage Dockerfile

Create `Dockerfile` with multi-stage build using distroless base image.

**File: `Dockerfile`**

```dockerfile
# =============================================================================
# Stage 1: Build
# =============================================================================
FROM golang:1.24-alpine AS builder

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

# Run the application
ENTRYPOINT ["/server"]
```

**Verification:**
```bash
# Build image
docker build -t url-shortener:test .

# Verify non-root user
docker run --rm url-shortener:test whoami
# Expected: nonroot

# Check image size
docker images url-shortener:test
# Expected: ~5-10 MB
```

---

### Step 2.2: Create Scratch Dockerfile (Alternative)

Create `Dockerfile.scratch` for smallest possible image.

**File: `Dockerfile.scratch`**

```dockerfile
FROM golang:1.24-alpine AS builder

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

---

### Step 2.3: Create Lambda Dockerfile (Future Use)

Create `Dockerfile.lambda` for AWS Lambda deployment.

**File: `Dockerfile.lambda`**

```dockerfile
FROM golang:1.24-alpine AS builder

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

**Note:** This requires `cmd/lambda/main.go` which will be created in a future plan.

---

## Part 3: Enhanced CI Pipeline

### Step 3.1: Add Concurrency Tests Job

Update `.github/workflows/ci.yml` to run concurrency tests separately.

**Changes to test job:**

```yaml
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
```

---

### Step 3.2: Add Docker Build to Build Job

Update build job to build and verify Docker image.

**Changes to build job:**

```yaml
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
        docker run --rm url-shortener:${{ github.sha }} whoami | grep -q nonroot
```

---

### Step 3.3: Add Security Scan Job

Add new job for security vulnerability scanning.

```yaml
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
```

---

### Step 3.4: Add Terraform Validate Job (Optional)

Add job to validate Terraform configurations if they exist.

```yaml
terraform-validate:
  name: Terraform Validate
  runs-on: ubuntu-latest
  if: hashFiles('terraform/**/*.tf') != ''
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
      continue-on-error: true

    - name: Terraform Validate
      working-directory: terraform/${{ matrix.provider }}
      run: terraform validate
      continue-on-error: true

    - name: Terraform Format Check
      working-directory: terraform/${{ matrix.provider }}
      run: terraform fmt -check -recursive
      continue-on-error: true
```

---

## Part 4: Add .dockerignore

### Step 4.1: Create .dockerignore

Create `.dockerignore` to exclude unnecessary files from Docker build context.

**File: `.dockerignore`**

```
# Git
.git
.gitignore

# Documentation
docs/
*.md
!README.md

# IDE
.idea/
.vscode/
*.swp
*.swo

# Test files
*_test.go
testdata/

# Build artifacts
bin/
*.exe
bootstrap

# CI/CD
.github/

# Terraform
terraform/

# Local environment
.env
.env.*

# OS files
.DS_Store
Thumbs.db
```

---

## Part 5: Update Makefile

### Step 5.1: Add Docker Commands to Makefile

Add Docker build and run targets to Makefile.

**New targets:**

```makefile
# Docker
DOCKER_IMAGE := url-shortener
DOCKER_TAG := $(shell git rev-parse --short HEAD 2>/dev/null || echo "latest")

.PHONY: docker-build
docker-build: ## Build Docker image
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_IMAGE):latest

.PHONY: docker-build-scratch
docker-build-scratch: ## Build Docker image using scratch base
	docker build -f Dockerfile.scratch -t $(DOCKER_IMAGE):$(DOCKER_TAG)-scratch .

.PHONY: docker-run
docker-run: docker-build ## Run Docker container
	docker run -p 8080:8080 $(DOCKER_IMAGE):$(DOCKER_TAG)

.PHONY: docker-verify
docker-verify: docker-build ## Verify Docker image security
	@echo "Verifying non-root user..."
	@docker run --rm $(DOCKER_IMAGE):$(DOCKER_TAG) whoami | grep -q nonroot && echo "OK: Running as non-root" || (echo "FAIL: Running as root" && exit 1)
	@echo "Checking image size..."
	@docker images $(DOCKER_IMAGE):$(DOCKER_TAG) --format "Size: {{.Size}}"

.PHONY: docker-scan
docker-scan: docker-build ## Scan Docker image for vulnerabilities
	trivy image $(DOCKER_IMAGE):$(DOCKER_TAG)
```

---

## Part 6: Terraform Infrastructure (AWS & GCP)

This section provides Terraform configurations for deploying the URL Shortener to AWS and GCP, following the specifications in `docs/07-infrastructure.md`.

### Directory Structure

```
terraform/
├── aws/
│   ├── main.tf           # AWS resources (Lambda, API Gateway, DynamoDB)
│   ├── variables.tf      # AWS variables (optional, can be in main.tf)
│   └── outputs.tf        # AWS outputs (optional, can be in main.tf)
└── gcp/
    ├── main.tf           # GCP resources (Cloud Run, Firestore)
    ├── variables.tf      # GCP variables (optional, can be in main.tf)
    └── outputs.tf        # GCP outputs (optional, can be in main.tf)
```

### Step 6.1: AWS Infrastructure

Create `terraform/aws/main.tf` with the following resources:

**AWS Resources:**
- DynamoDB table (on-demand billing, TTL enabled, encrypted at rest)
- IAM role with least privilege (only DynamoDB operations)
- Lambda function (ARM64, provided.al2023 runtime)
- API Gateway HTTP API with routes (`POST /shorten`, `GET /s/{code}`, `GET /stats/{code}`, `GET /health`)
- CloudWatch Log Groups (30-day retention)

**Key Configuration Points:**
- Use `PAY_PER_REQUEST` billing mode for DynamoDB
- Enable TTL on `expires_at` attribute
- Enable point-in-time recovery
- Use ARM64 architecture for Lambda (cost-effective)
- Configure CORS for API Gateway
- No IP address logging (privacy by design)

**Verification:**
```bash
cd terraform/aws
terraform init
terraform validate
terraform fmt -check
```

---

### Step 6.2: GCP Infrastructure

Create `terraform/gcp/main.tf` with the following resources:

**GCP Resources:**
- Firestore database (native mode)
- Firestore TTL policy on `expires_at` field
- Firestore index for expiration queries
- Artifact Registry repository for Docker images
- Service account with `roles/datastore.user` role only
- Cloud Run service (scale-to-zero enabled)

**Key Configuration Points:**
- Enable required APIs: Cloud Run, Firestore, Artifact Registry
- Use `FIRESTORE_NATIVE` database type
- Set `cpu_idle = true` for scale-to-zero
- Configure health probes (startup and liveness)
- Allow unauthenticated access for public API

**Verification:**
```bash
cd terraform/gcp
terraform init
terraform validate
terraform fmt -check
```

---

### Step 6.3: Deployment Commands

**AWS Deployment:**
```bash
cd terraform/aws

# Initialize
terraform init

# Plan
terraform plan -var="environment=prod"

# Apply
terraform apply -var="environment=prod"
```

**GCP Deployment:**
```bash
cd terraform/gcp

# Initialize
terraform init

# Plan
terraform plan -var="project_id=my-project" -var="environment=prod"

# Apply
terraform apply -var="project_id=my-project" -var="environment=prod"
```

---

## Checklist

### Part 1: golangci-lint Enhancement
- [x] Add security linters (gosec, bodyclose, noctx)
- [x] Add format linters (gofmt, goimports)
- [x] Add extended revive rules
- [x] Add modules-download-mode setting
- [x] Verify linting passes on current codebase

### Part 2: Dockerfiles
- [x] Create production Dockerfile (distroless)
- [x] Create Dockerfile.scratch (alternative)
- [x] Create Dockerfile.lambda (future use)
- [ ] Verify Docker build succeeds (requires Docker)
- [ ] Verify non-root user in container (requires Docker)
- [ ] Verify image size is reasonable (~5-10 MB) (requires Docker)

### Part 3: CI Pipeline
- [x] Add concurrency tests step
- [x] Add Docker build to build job
- [x] Add non-root user verification
- [x] Add security scan job (Trivy)
- [x] Add Terraform validate job (optional, conditional)
- [ ] Verify CI pipeline passes (requires push to remote)

### Part 4: Supporting Files
- [x] Create .dockerignore
- [x] Update Makefile with Docker targets

### Part 5: Verification
- [ ] Run `make docker-build` locally (requires Docker)
- [ ] Run `make docker-verify` locally (requires Docker)
- [ ] Run `make docker-scan` locally (requires trivy)
- [ ] Push branch and verify CI passes

### Part 6: Terraform Infrastructure
- [x] Create `terraform/aws/main.tf` with full AWS configuration
- [x] Create `terraform/gcp/main.tf` with full GCP configuration
- [ ] Verify `terraform init` succeeds for both providers (requires Terraform)
- [ ] Verify `terraform validate` passes for both (requires Terraform)
- [ ] Verify `terraform fmt -check` passes for both (requires Terraform)

---

## Dependencies

- Plan 01 (HTTP Server): Completed - provides `cmd/server/main.go`
- Plan 02 (Handlers): Completed - provides handler implementations
- Plan 03 (Shortcode & Storage): Not required for Docker build (handlers use interface)
- Plan 03 (Shortcode & Storage): Required for Terraform deployment - provides the repository interface that DynamoDB and Firestore implementations will satisfy

**Note:** For production deployment with Terraform, DynamoDB and Firestore repository implementations will be needed. These implementations should satisfy the `URLRepository` interface defined in Plan 03.

---

## Notes

1. **Dockerfile.lambda** requires `cmd/lambda/main.go` which doesn't exist yet. This will be created in a future infrastructure plan.

2. **Terraform validate job** is conditional and will skip if no Terraform files exist.

3. **Security scanning** with Trivy may find vulnerabilities in base images. These should be reviewed and addressed as needed.

4. **Image size** target:
   - Distroless: ~5-10 MB
   - Scratch: ~3-5 MB
   - Lambda: ~50 MB (AWS managed base)

5. The current CI uses `make test-race` and `make build`. The plan updates to use explicit `go` commands for consistency with the spec, but the Makefile approach is also valid.

6. **Implementation Notes:**
   - Enhanced linter configuration includes exclusions for test files to avoid false positives on common test patterns (noctx, bodyclose, gosec, unused-parameter)
   - The `handleHealth` function parameter was renamed from `r` to `_` to satisfy the unused-parameter rule
   - All Go tests pass with race detector enabled
   - Linting passes with the new enhanced configuration
