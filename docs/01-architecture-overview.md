# Architecture Overview

## System Overview

The Distributed URL Shortener is a stateless HTTP service that generates short codes for long URLs and redirects users to the original destination. The architecture follows clean architecture principles with strict separation of concerns.

## High-Level System Diagram

```
                                    +------------------+
                                    |   API Gateway    |
                                    |  (Rate Limiting) |
                                    +--------+---------+
                                             |
                    +------------------------+------------------------+
                    |                        |                        |
            +-------v-------+        +-------v-------+        +-------v-------+
            |   Instance 1  |        |   Instance 2  |        |   Instance N  |
            |  (Stateless)  |        |  (Stateless)  |        |  (Stateless)  |
            +-------+-------+        +-------+-------+        +-------+-------+
                    |                        |                        |
                    +------------------------+------------------------+
                                             |
                                    +--------v---------+
                                    |  Storage Layer   |
                                    |  (DynamoDB/Redis)|
                                    +------------------+
```

## Component Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              HTTP Layer                                      │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                    Middleware Chain                                      ││
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌─────────────┐ ││
│  │  │   Recovery   │─▶│   Timing     │─▶│   Logging    │─▶│   Router    │ ││
│  │  └──────────────┘  └──────────────┘  └──────────────┘  └─────────────┘ ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Handler Layer                                     │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐              │
│  │  CreateHandler  │  │ RedirectHandler │  │   StatsHandler  │              │
│  └────────┬────────┘  └────────┬────────┘  └────────┬────────┘              │
└───────────┼─────────────────────┼─────────────────────┼──────────────────────┘
            │                     │                     │
            └─────────────────────┼─────────────────────┘
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Service Layer                                     │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                         URLService                                       ││
│  │  - Create(longURL, ttl) -> shortCode                                    ││
│  │  - Resolve(shortCode) -> longURL                                        ││
│  │  - GetStats(shortCode) -> URLRecord                                     ││
│  └─────────────────────────────────────────────────────────────────────────┘│
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                       ExpirationService                                  ││
│  │  - StartCleanup(ctx)                                                    ││
│  │  - IsExpired(record) -> bool                                            ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Domain Layer                                       │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐              │
│  │    URLRecord    │  │  ShortCodeGen   │  │     Clock       │              │
│  │    (Entity)     │  │   (Interface)   │  │   (Interface)   │              │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘              │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Storage Layer                                       │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                      Repository Interface                                ││
│  │  - Save(record) -> error                                                ││
│  │  - FindByShortCode(code) -> (record, error)                             ││
│  │  - IncrementClickCount(code) -> error                                   ││
│  │  - DeleteExpired(before time.Time) -> (count, error)                    ││
│  │  - Exists(code) -> bool                                                 ││
│  └─────────────────────────────────────────────────────────────────────────┘│
│                                      │                                       │
│         ┌────────────────────────────┼────────────────────────────┐         │
│         ▼                            ▼                            ▼         │
│  ┌─────────────────┐        ┌─────────────────┐        ┌─────────────────┐  │
│  │MemoryRepository│        │DynamoRepository │        │ RedisRepository │  │
│  │ (Development)  │        │  (Production)   │        │  (Production)   │  │
│  └─────────────────┘        └─────────────────┘        └─────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Data Model

### URLRecord Entity

```go
type URLRecord struct {
    ShortCode      string    // Primary key, 8-character code
    LongURL        string    // Original URL (max 2048 chars)
    CreatedAt      time.Time // Timestamp of creation
    ExpiresAt      time.Time // TTL expiration timestamp
    ClickCount     int64     // Atomic counter for redirects
    LastAccessedAt time.Time // Last redirect timestamp
}
```

### Storage Schema (DynamoDB)

| Attribute        | Type   | Key Type | Description                    |
|------------------|--------|----------|--------------------------------|
| short_code       | String | PK       | 8-character unique identifier  |
| long_url         | String | -        | Original URL                   |
| created_at       | Number | -        | Unix timestamp (milliseconds)  |
| expires_at       | Number | GSI-PK   | Unix timestamp for TTL         |
| click_count      | Number | -        | Redirect counter               |
| last_accessed_at | Number | -        | Last access Unix timestamp     |

**Global Secondary Index (GSI):**
- `expires_at-index`: Enables efficient querying for expired records during background cleanup.

## Directory Structure

```
url-shortener/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/
│   ├── domain/
│   │   ├── url.go               # URLRecord entity
│   │   ├── errors.go            # Domain-specific errors
│   │   └── clock.go             # Clock interface for testing
│   ├── service/
│   │   ├── url_service.go       # Core business logic
│   │   ├── shortcode.go         # Short code generation
│   │   └── expiration.go        # Expiration cleanup service
│   ├── repository/
│   │   ├── repository.go        # Repository interface
│   │   ├── memory.go            # In-memory implementation
│   │   └── dynamodb.go          # DynamoDB implementation (stub)
│   ├── handler/
│   │   ├── create.go            # POST /shorten handler
│   │   ├── redirect.go          # GET /s/{code} handler
│   │   ├── stats.go             # GET /stats/{code} handler
│   │   └── health.go            # GET /health handler
│   ├── middleware/
│   │   ├── timing.go            # X-Processing-Time-Micros
│   │   ├── recovery.go          # Panic recovery
│   │   └── logging.go           # Request logging (PII-safe)
│   └── config/
│       └── config.go            # Configuration management
├── terraform/
│   ├── aws/
│   │   └── main.tf              # AWS infrastructure
│   └── gcp/
│       └── main.tf              # GCP infrastructure
├── .github/
│   └── workflows/
│       └── ci.yml               # CI/CD pipeline
├── Dockerfile                   # Multi-stage build
├── .golangci.yml               # Linter configuration
├── go.mod
├── go.sum
└── README.md
```

## Design Principles

### 1. Dependency Inversion
All dependencies flow inward. The domain layer has no external dependencies. Services depend on interfaces, not concrete implementations.

### 2. Interface Segregation
Small, focused interfaces:
- `Repository`: Storage operations
- `Clock`: Time abstraction for testing
- `ShortCodeGenerator`: Code generation strategy

### 3. Single Responsibility
Each component has one reason to change:
- Handlers: HTTP concerns only
- Services: Business logic only
- Repository: Data persistence only

### 4. Privacy by Design
- No IP addresses stored or logged
- Request logs contain only non-PII data
- Short codes are not derived from user data
