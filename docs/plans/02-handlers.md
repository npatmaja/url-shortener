# Plan: HTTP Handlers (Structure Only)

## Status: COMPLETED

## Overview

Implement handler structure for all API endpoints. This phase focuses only on:
- Request parsing and validation
- Response formatting
- HTTP status codes
- Error response structure

Business logic (service layer) will be stubbed and implemented later.

## Goals

- Define request/response DTOs
- Implement request validation
- Return proper HTTP status codes
- Consistent error response format
- All handlers accept dependencies via interface (for testing)

## Directory Structure

```
internal/
├── handler/
│   ├── handler.go        # Handler struct, interface, sentinel errors, helpers
│   ├── dto.go            # Request/Response types
│   ├── validation.go     # URL and TTL validation
│   ├── create.go         # POST /shorten
│   ├── create_test.go    # Create handler tests
│   ├── redirect.go       # GET /s/{code}
│   ├── redirect_test.go  # Redirect handler tests
│   ├── stats.go          # GET /stats/{code}
│   └── stats_test.go     # Stats handler tests
├── middleware/
│   ├── timing.go         # X-Processing-Time-Micros
│   └── timing_test.go
└── server/
    ├── server.go         # Updated to use handlers
    └── server_test.go
```

---

## Implementation Summary

### Step 1: DTOs (dto.go)

```go
package handler

// === Requests ===

type CreateRequest struct {
    LongURL    string `json:"long_url"`
    TTLSeconds *int64 `json:"ttl_seconds,omitempty"`
}

// === Responses ===

type CreateResponse struct {
    ShortCode string `json:"short_code"`
    ShortURL  string `json:"short_url"`
    LongURL   string `json:"long_url"`
    ExpiresAt string `json:"expires_at"`
}

type StatsResponse struct {
    ShortCode      string  `json:"short_code"`
    LongURL        string  `json:"long_url"`
    CreatedAt      string  `json:"created_at"`
    ExpiresAt      string  `json:"expires_at"`
    ClickCount     int64   `json:"click_count"`
    LastAccessedAt *string `json:"last_accessed_at"`
}

type HealthResponse struct {
    Status    string `json:"status"`
    Timestamp string `json:"timestamp"`
}

type ErrorResponse struct {
    Error   string `json:"error"`
    Message string `json:"message"`
}
```

---

### Step 2: Handler and Service Interface (handler.go)

```go
package handler

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "time"
)

// Sentinel errors for handler layer
var (
    ErrNotFound = errors.New("not found")
    ErrExpired  = errors.New("expired")
)

// URLRecord represents the domain entity (will be defined in domain package later)
type URLRecord struct {
    ShortCode      string
    LongURL        string
    CreatedAt      time.Time
    ExpiresAt      time.Time
    ClickCount     int64
    LastAccessedAt time.Time
}

// URLService defines the service interface.
// This allows testing handlers without real service implementation.
type URLService interface {
    Create(ctx context.Context, longURL string, ttl time.Duration) (*URLRecord, error)
    Resolve(ctx context.Context, shortCode string) (string, error)
    GetStats(ctx context.Context, shortCode string) (*URLRecord, error)
}

// Handler holds dependencies for HTTP handlers.
type Handler struct {
    service URLService
    baseURL string
}

// New creates a new Handler with the given dependencies.
func New(service URLService, baseURL string) *Handler {
    return &Handler{
        service: service,
        baseURL: baseURL,
    }
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
    h.writeJSON(w, status, ErrorResponse{
        Error:   code,
        Message: message,
    })
}
```

---

### Step 3: Validation (validation.go)

```go
package handler

import (
    "errors"
    "net/url"
    "time"
)

const (
    maxURLLength = 2048
    minTTL       = 60 * time.Second     // 1 minute
    maxTTL       = 365 * 24 * time.Hour // 1 year
)

func validateURL(rawURL string) error {
    if rawURL == "" {
        return errors.New("long_url is required")
    }

    if len(rawURL) > maxURLLength {
        return errors.New("long_url exceeds maximum length of 2048 characters")
    }

    parsed, err := url.Parse(rawURL)
    if err != nil {
        return errors.New("invalid URL format")
    }

    if parsed.Scheme != "http" && parsed.Scheme != "https" {
        return errors.New("URL scheme must be http or https")
    }

    if parsed.Host == "" {
        return errors.New("URL must have a host")
    }

    return nil
}

func validateTTL(ttl time.Duration) error {
    if ttl < minTTL {
        return errors.New("ttl_seconds must be at least 60")
    }
    if ttl > maxTTL {
        return errors.New("ttl_seconds must not exceed 31536000 (1 year)")
    }
    return nil
}
```

---

### Step 4: Create Handler (create.go)

```go
package handler

import (
    "encoding/json"
    "net/http"
    "time"
)

const defaultTTL = 24 * time.Hour

// Create handles POST /shorten requests.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
    var req CreateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        h.writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
        return
    }

    // Validate URL
    if err := validateURL(req.LongURL); err != nil {
        h.writeError(w, http.StatusBadRequest, "validation_error", err.Error())
        return
    }

    // Determine TTL
    ttl := defaultTTL
    if req.TTLSeconds != nil {
        ttl = time.Duration(*req.TTLSeconds) * time.Second
        if err := validateTTL(ttl); err != nil {
            h.writeError(w, http.StatusBadRequest, "validation_error", err.Error())
            return
        }
    }

    // Call service
    record, err := h.service.Create(r.Context(), req.LongURL, ttl)
    if err != nil {
        h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to create short URL")
        return
    }

    // Build response
    resp := CreateResponse{
        ShortCode: record.ShortCode,
        ShortURL:  h.baseURL + "/s/" + record.ShortCode,
        LongURL:   record.LongURL,
        ExpiresAt: record.ExpiresAt.Format(time.RFC3339),
    }

    h.writeJSON(w, http.StatusCreated, resp)
}
```

---

### Step 5: Redirect Handler (redirect.go)

```go
package handler

import (
    "errors"
    "net/http"
)

// Redirect handles GET /s/{code} requests.
func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
    code := r.PathValue("code")
    if code == "" {
        h.writeError(w, http.StatusBadRequest, "validation_error", "short code is required")
        return
    }

    longURL, err := h.service.Resolve(r.Context(), code)
    if err != nil {
        if errors.Is(err, ErrNotFound) || errors.Is(err, ErrExpired) {
            h.writeError(w, http.StatusNotFound, "not_found", "short code not found or expired")
            return
        }
        h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve URL")
        return
    }

    http.Redirect(w, r, longURL, http.StatusFound)
}
```

---

### Step 6: Stats Handler (stats.go)

```go
package handler

import (
    "errors"
    "net/http"
    "time"
)

// Stats handles GET /stats/{code} requests.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
    code := r.PathValue("code")
    if code == "" {
        h.writeError(w, http.StatusBadRequest, "validation_error", "short code is required")
        return
    }

    record, err := h.service.GetStats(r.Context(), code)
    if err != nil {
        if errors.Is(err, ErrNotFound) || errors.Is(err, ErrExpired) {
            h.writeError(w, http.StatusNotFound, "not_found", "short code not found or expired")
            return
        }
        h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to get stats")
        return
    }

    resp := StatsResponse{
        ShortCode:  record.ShortCode,
        LongURL:    record.LongURL,
        CreatedAt:  record.CreatedAt.Format(time.RFC3339),
        ExpiresAt:  record.ExpiresAt.Format(time.RFC3339),
        ClickCount: record.ClickCount,
    }

    // Only set LastAccessedAt if it's not zero
    if !record.LastAccessedAt.IsZero() {
        formatted := record.LastAccessedAt.Format(time.RFC3339)
        resp.LastAccessedAt = &formatted
    }

    h.writeJSON(w, http.StatusOK, resp)
}
```

---

### Step 7: Server Integration (server.go)

Updated `server.go` to:
- Add `BaseURL` to Config
- Accept optional `URLService` via variadic argument (backwards compatible)
- Register handler routes when URLService is provided

```go
// Config holds server configuration.
type Config struct {
    Port            int
    ShutdownTimeout time.Duration
    BaseURL         string
}

// Server represents the HTTP server.
type Server struct {
    cfg        Config
    httpServer *http.Server
    mux        *http.ServeMux
    handler    *handler.Handler
}

// New creates a new Server with the given configuration.
// Optional urlService can be passed to enable URL shortening endpoints.
func New(cfg Config, urlService ...handler.URLService) *Server {
    mux := http.NewServeMux()

    s := &Server{
        cfg: cfg,
        mux: mux,
        httpServer: &http.Server{
            Addr:         fmt.Sprintf(":%d", cfg.Port),
            Handler:      middleware.Timing(mux),
            ReadTimeout:  10 * time.Second,
            WriteTimeout: 10 * time.Second,
            IdleTimeout:  60 * time.Second,
        },
    }

    // If URLService is provided, create handler
    if len(urlService) > 0 && urlService[0] != nil {
        s.handler = handler.New(urlService[0], cfg.BaseURL)
    }

    s.registerRoutes()
    return s
}

func (s *Server) registerRoutes() {
    s.mux.HandleFunc("GET /health", s.handleHealth)

    // Register URL shortening routes if handler is available
    if s.handler != nil {
        s.mux.HandleFunc("POST /shorten", s.handler.Create)
        s.mux.HandleFunc("GET /s/{code}", s.handler.Redirect)
        s.mux.HandleFunc("GET /stats/{code}", s.handler.Stats)
    }
}
```

---

## Test Coverage

### Create Handler Tests (create_test.go)
- `TestCreateHandler_ValidRequest_Returns201`
- `TestCreateHandler_WithCustomTTL_UsesTTL`
- `TestCreateHandler_InvalidURL_Returns400` (table-driven: empty URL, missing scheme, ftp scheme, no host)
- `TestCreateHandler_InvalidJSON_Returns400`
- `TestCreateHandler_URLTooLong_Returns400`

### Redirect Handler Tests (redirect_test.go)
- `TestRedirectHandler_ValidCode_Returns302`
- `TestRedirectHandler_NotFound_Returns404`
- `TestRedirectHandler_Expired_Returns404`
- `TestRedirectHandler_ServiceError_Returns500`

### Stats Handler Tests (stats_test.go)
- `TestStatsHandler_ValidCode_Returns200`
- `TestStatsHandler_NeverAccessed_LastAccessedIsNull`
- `TestStatsHandler_NotFound_Returns404`

---

## Checklist

- [x] Create DTOs (request/response types)
- [x] Define URLService interface
- [x] Create mock service for testing
- [x] Write test: Create handler - valid request returns 201
- [x] Write test: Create handler - custom TTL
- [x] Implement: Create handler
- [x] Write test: Create handler - validation errors (empty URL, invalid scheme, too long)
- [x] Implement: URL validation
- [x] Write test: Redirect handler - valid code returns 302
- [x] Write test: Redirect handler - not found returns 404
- [x] Write test: Redirect handler - expired returns 404
- [x] Implement: Redirect handler
- [x] Write test: Stats handler - valid code returns 200
- [x] Write test: Stats handler - never accessed has null LastAccessedAt
- [x] Write test: Stats handler - not found returns 404
- [x] Implement: Stats handler
- [x] Add helper methods (writeJSON, writeError)
- [x] Update server to register handler routes
- [x] Run all tests with race detector
- [x] Pass linting (golangci-lint)

---

## Notes

- All handlers use dependency injection via `URLService` interface
- Tests use `MockURLService` (testify/mock) to isolate handler logic
- Business logic (service layer) is not implemented yet - it will return stub data
- Error handling uses sentinel errors (`ErrNotFound`, `ErrExpired`) for type checking
- Response format is consistent across all endpoints
- Server accepts optional URLService for backwards compatibility with existing tests
- Import path is `url-shortener/internal/handler` (module name)
