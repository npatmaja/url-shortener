# URL Shortener

A stateless, production-ready URL shortener service built with Go using clean architecture principles.

[![CI](https://github.com/npatmaja/url-shortener/actions/workflows/ci.yml/badge.svg)](https://github.com/npatmaja/url-shortener/actions/workflows/ci.yml)

## Features

- **URL Shortening** - Generate 8-character short codes using cryptographically secure randomness
- **Configurable TTL** - Set expiration from 60 seconds to 1 year (default: 24 hours)
- **Click Tracking** - Track click counts and last access timestamps
- **Statistics API** - Retrieve URL analytics via dedicated endpoint
- **Health Check** - Built-in health endpoint for load balancer integration
- **Processing Time Headers** - `X-Processing-Time-Micros` header on all responses
- **Privacy-Focused** - No IP address logging or user tracking

## Tech Stack

- **Go 1.24+** - Modern Go with latest features
- **Standard Library** - No external web frameworks, uses `net/http`
- **Clean Architecture** - Domain/Service/Repository/Handler layers
- **testify** - Testing assertions and mocking

## Quick Start

### Prerequisites

- Go 1.24 or higher
- Make (optional, for convenience commands)

### Build and Run

```bash
# Build the binary
make build

# Run the server
./bin/server

# Or build and run in one step
go run cmd/server/main.go
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `BASE_URL` | `http://localhost:{PORT}` | Base URL for generated short links |
| `SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |

```bash
# Example
PORT=3000 BASE_URL=https://short.example.com ./bin/server
```

## API Documentation

### Create Short URL

```
POST /shorten
```

**Request:**
```json
{
  "long_url": "https://example.com/very/long/path/to/resource",
  "ttl_seconds": 86400
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `long_url` | string | Yes | URL to shorten (http/https, max 2048 chars) |
| `ttl_seconds` | integer | No | Time-to-live in seconds (60-31536000, default: 86400) |

**Response (201 Created):**
```json
{
  "short_code": "Ab2CdE3F",
  "short_url": "http://localhost:8080/s/Ab2CdE3F",
  "long_url": "https://example.com/very/long/path/to/resource",
  "expires_at": "2024-01-16T12:00:00Z"
}
```

**Error Response (400 Bad Request):**
```json
{
  "error": "validation_error",
  "message": "long_url is required"
}
```

### Redirect

```
GET /s/{code}
```

Redirects to the original URL (HTTP 302). Increments click counter on each access.

**Error Response (404 Not Found):**
```json
{
  "error": "not_found",
  "message": "short code not found or expired"
}
```

### Get Statistics

```
GET /stats/{code}
```

**Response (200 OK):**
```json
{
  "short_code": "Ab2CdE3F",
  "long_url": "https://example.com/path",
  "created_at": "2024-01-15T12:00:00Z",
  "expires_at": "2024-01-16T12:00:00Z",
  "click_count": 42,
  "last_accessed_at": "2024-01-15T15:30:00Z"
}
```

Note: `last_accessed_at` is `null` if the URL has never been accessed.

### Health Check

```
GET /health
```

**Response (200 OK):**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T12:00:00Z"
}
```

## Project Structure

```
url-shortener/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/
│   ├── domain/                  # Core business entities and errors
│   │   ├── url.go               # URLRecord model
│   │   ├── errors.go            # Domain errors
│   │   └── clock.go             # Time abstraction
│   ├── service/                 # Business logic layer
│   │   └── url_service.go       # URL shortening service
│   ├── repository/              # Data persistence layer
│   │   ├── repository.go        # Repository interface
│   │   └── memory.go            # In-memory implementation
│   ├── handler/                 # HTTP handlers
│   │   ├── handler.go           # Handler dependencies
│   │   ├── create.go            # POST /shorten
│   │   ├── redirect.go          # GET /s/{code}
│   │   ├── stats.go             # GET /stats/{code}
│   │   ├── dto.go               # Request/response DTOs
│   │   └── validation.go        # Input validation
│   ├── shortcode/               # Code generation
│   │   └── generator.go         # Cryptographic code generator
│   ├── server/                  # HTTP server setup
│   │   └── server.go            # Routing and configuration
│   └── middleware/              # HTTP middleware
│       └── timing.go            # Request timing
├── terraform/                   # Infrastructure as code
│   ├── aws/                     # AWS Lambda + DynamoDB
│   └── gcp/                     # GCP Cloud Run + Firestore
├── Dockerfile                   # Production container (distroless)
├── Dockerfile.scratch           # Minimal scratch container
├── Dockerfile.lambda            # AWS Lambda container
├── Makefile                     # Build commands
└── go.mod                       # Go module definition
```

## Development

### Make Commands

| Command | Description |
|---------|-------------|
| `make build` | Build binary to `bin/server` |
| `make test` | Run all tests |
| `make test-race` | Run tests with race detector |
| `make lint` | Run golangci-lint |
| `make clean` | Remove build artifacts |
| `make docker-build` | Build Docker image |
| `make docker-run` | Build and run in Docker |
| `make docker-scan` | Scan image for vulnerabilities |

### Running Tests

```bash
# Run all tests
make test

# Run with race detector
make test-race

# Run specific package tests
go test -v ./internal/service/...
```

### Code Quality

```bash
# Run linter
make lint

# The project uses golangci-lint with these linters:
# - gocyclo (complexity max: 10)
# - gosec (security)
# - errcheck, govet, staticcheck
# - gofmt, goimports
```

## Architecture

The project follows **Clean Architecture** principles with clear separation of concerns:

```
┌─────────────────────────────────────┐
│         Handler Layer               │
│   (HTTP, JSON serialization)        │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│         Service Layer               │
│   (Business logic, orchestration)   │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│        Repository Layer             │
│   (Data persistence abstraction)    │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│         Domain Layer                │
│   (Entities, value objects)         │
└─────────────────────────────────────┘
```

### Storage Interface

The repository interface is designed for easy backend swapping:

```go
type Repository interface {
    SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error
    FindByShortCode(ctx context.Context, code string) (*domain.URLRecord, error)
    IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error
    DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
```

Current implementation uses in-memory storage with `sync.RWMutex` for thread safety. The interface supports:
- **Redis** - For distributed caching
- **DynamoDB** - AWS serverless storage (Terraform included)
- **Firestore** - GCP serverless storage (Terraform included)
- **PostgreSQL/MySQL** - Traditional RDBMS

### Short Code Generation

- **Length:** 8 characters
- **Alphabet:** `23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz` (54 chars)
- **Excludes:** Ambiguous characters (0/O, 1/l/I)
- **Randomness:** Uses `crypto/rand` for cryptographic security
- **Collision Space:** 54^8 = ~72 trillion possible codes
- **Collision Handling:** Up to 5 retry attempts with new codes

## Docker

### Build Options

```bash
# Standard build (distroless base)
make docker-build

# Minimal scratch-based image
make docker-build-scratch

# AWS Lambda build
docker build -f Dockerfile.lambda -t url-shortener:lambda .
```

### Run

```bash
# Run with default settings
make docker-run

# Run with custom configuration
docker run -p 8080:8080 \
  -e BASE_URL=https://short.example.com \
  url-shortener:latest
```

## Deployment

Terraform configurations are provided for cloud deployment:

### AWS (Lambda + DynamoDB)
```bash
cd terraform/aws
terraform init
terraform apply
```

### GCP (Cloud Run + Firestore)
```bash
cd terraform/gcp
terraform init
terraform apply
```

## License

MIT
