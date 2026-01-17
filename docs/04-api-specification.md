# API Specification

## Base URL

```
Development: http://localhost:8080
Production:  https://api.example.com
```

## Common Headers

All responses include:

| Header | Description |
|--------|-------------|
| `X-Processing-Time-Micros` | Request processing time in microseconds |
| `Content-Type` | `application/json` for JSON responses |

## Endpoints

### 1. Create Short URL

Creates a new shortened URL.

**Request**

```
POST /shorten
Content-Type: application/json
```

```json
{
  "long_url": "https://example.com/very/long/path?query=params",
  "ttl_seconds": 86400
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `long_url` | string | Yes | - | Valid HTTP/HTTPS URL (max 2048 chars) |
| `ttl_seconds` | integer | No | 86400 | Time-to-live in seconds (default 24h) |

**Response: 201 Created**

```json
{
  "short_code": "Ab2CdE3F",
  "short_url": "http://localhost:8080/s/Ab2CdE3F",
  "long_url": "https://example.com/very/long/path?query=params",
  "expires_at": "2024-01-16T12:00:00Z"
}
```

**Response: 400 Bad Request**

```json
{
  "error": "validation_error",
  "message": "invalid URL: scheme must be http or https",
  "field": "long_url"
}
```

**Validation Rules:**
- `long_url` must be a valid URL with `http` or `https` scheme
- `long_url` maximum length: 2048 characters
- `ttl_seconds` minimum: 60 (1 minute)
- `ttl_seconds` maximum: 31536000 (1 year)

---

### 2. Redirect to Long URL

Redirects to the original URL.

**Request**

```
GET /s/{short_code}
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `short_code` | string | 8-character short code |

**Response: 302 Found**

```
HTTP/1.1 302 Found
Location: https://example.com/very/long/path?query=params
X-Processing-Time-Micros: 1234
```

**Response: 404 Not Found**

```json
{
  "error": "not_found",
  "message": "short code not found or expired"
}
```

**Behavior:**
- Valid, non-expired code: 302 redirect with `Location` header
- Non-existent code: 404 Not Found
- Expired code: 404 Not Found (lazy expiration check)
- Each successful redirect increments `click_count` and updates `last_accessed_at`

---

### 3. Get URL Statistics

Returns statistics for a shortened URL.

**Request**

```
GET /stats/{short_code}
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `short_code` | string | 8-character short code |

**Response: 200 OK**

```json
{
  "short_code": "Ab2CdE3F",
  "long_url": "https://example.com/very/long/path?query=params",
  "created_at": "2024-01-15T12:00:00Z",
  "expires_at": "2024-01-16T12:00:00Z",
  "click_count": 42,
  "last_accessed_at": "2024-01-15T15:30:00Z"
}
```

**Response: 404 Not Found**

```json
{
  "error": "not_found",
  "message": "short code not found or expired"
}
```

**Note:** `last_accessed_at` is `null` if the URL has never been accessed.

---

### 4. Health Check

Returns service health status.

**Request**

```
GET /health
```

**Response: 200 OK**

```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T12:00:00Z"
}
```

---

## Error Response Format

All errors follow a consistent format:

```json
{
  "error": "error_code",
  "message": "Human-readable error description",
  "field": "field_name"  // Optional, only for validation errors
}
```

| Error Code | HTTP Status | Description |
|------------|-------------|-------------|
| `validation_error` | 400 | Invalid request data |
| `not_found` | 404 | Resource not found |
| `method_not_allowed` | 405 | HTTP method not supported |
| `internal_error` | 500 | Server error |

---

## Implementation: Handler Layer

### HTTP Handlers (Standard Library)

```go
package handler

import (
    "encoding/json"
    "net/http"

    "github.com/npatmaja/url-shortener/internal/service"
)

type Handler struct {
    urlService *service.URLService
    baseURL    string
}

func NewHandler(urlService *service.URLService, baseURL string) *Handler {
    return &Handler{
        urlService: urlService,
        baseURL:    baseURL,
    }
}

// RegisterRoutes sets up the HTTP routes using standard library.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
    mux.HandleFunc("POST /shorten", h.CreateShortURL)
    mux.HandleFunc("GET /s/{code}", h.Redirect)
    mux.HandleFunc("GET /stats/{code}", h.GetStats)
    mux.HandleFunc("GET /health", h.Health)
}
```

### Create Handler

```go
type CreateRequest struct {
    LongURL    string `json:"long_url"`
    TTLSeconds *int64 `json:"ttl_seconds,omitempty"`
}

type CreateResponse struct {
    ShortCode string `json:"short_code"`
    ShortURL  string `json:"short_url"`
    LongURL   string `json:"long_url"`
    ExpiresAt string `json:"expires_at"`
}

func (h *Handler) CreateShortURL(w http.ResponseWriter, r *http.Request) {
    var req CreateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "validation_error", "invalid JSON body")
        return
    }

    // Validate URL
    if err := validateURL(req.LongURL); err != nil {
        writeError(w, http.StatusBadRequest, "validation_error", err.Error())
        return
    }

    // Default TTL: 24 hours
    ttl := 24 * time.Hour
    if req.TTLSeconds != nil {
        ttl = time.Duration(*req.TTLSeconds) * time.Second
    }

    // Create short URL
    record, err := h.urlService.Create(r.Context(), req.LongURL, ttl)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "internal_error", "failed to create short URL")
        return
    }

    resp := CreateResponse{
        ShortCode: record.ShortCode,
        ShortURL:  h.baseURL + "/s/" + record.ShortCode,
        LongURL:   record.LongURL,
        ExpiresAt: record.ExpiresAt.Format(time.RFC3339),
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(resp)
}
```

### Redirect Handler

```go
func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
    code := r.PathValue("code")

    longURL, err := h.urlService.Resolve(r.Context(), code)
    if err != nil {
        if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrExpired) {
            writeError(w, http.StatusNotFound, "not_found", "short code not found or expired")
            return
        }
        writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve URL")
        return
    }

    http.Redirect(w, r, longURL, http.StatusFound)
}
```

### Stats Handler

```go
type StatsResponse struct {
    ShortCode      string  `json:"short_code"`
    LongURL        string  `json:"long_url"`
    CreatedAt      string  `json:"created_at"`
    ExpiresAt      string  `json:"expires_at"`
    ClickCount     int64   `json:"click_count"`
    LastAccessedAt *string `json:"last_accessed_at"`
}

func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
    code := r.PathValue("code")

    record, err := h.urlService.GetStats(r.Context(), code)
    if err != nil {
        if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrExpired) {
            writeError(w, http.StatusNotFound, "not_found", "short code not found or expired")
            return
        }
        writeError(w, http.StatusInternalServerError, "internal_error", "failed to get stats")
        return
    }

    resp := StatsResponse{
        ShortCode:  record.ShortCode,
        LongURL:    record.LongURL,
        CreatedAt:  record.CreatedAt.Format(time.RFC3339),
        ExpiresAt:  record.ExpiresAt.Format(time.RFC3339),
        ClickCount: record.ClickCount,
    }

    if !record.LastAccessedAt.IsZero() {
        formatted := record.LastAccessedAt.Format(time.RFC3339)
        resp.LastAccessedAt = &formatted
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}
```

### URL Validation

```go
import (
    "errors"
    "net/url"
)

const maxURLLength = 2048

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
```

### Error Helper

```go
type ErrorResponse struct {
    Error   string `json:"error"`
    Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(ErrorResponse{
        Error:   code,
        Message: message,
    })
}
```

---

## Middleware: Processing Time Header

```go
package middleware

import (
    "net/http"
    "strconv"
    "time"
)

// Timing adds X-Processing-Time-Micros header to all responses.
func Timing(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()

        // Wrap response writer to capture when headers are written
        wrapped := &timingResponseWriter{
            ResponseWriter: w,
            start:          start,
        }

        next.ServeHTTP(wrapped, r)
    })
}

type timingResponseWriter struct {
    http.ResponseWriter
    start       time.Time
    wroteHeader bool
}

func (w *timingResponseWriter) WriteHeader(code int) {
    if !w.wroteHeader {
        elapsed := time.Since(w.start).Microseconds()
        w.Header().Set("X-Processing-Time-Micros", strconv.FormatInt(elapsed, 10))
        w.wroteHeader = true
    }
    w.ResponseWriter.WriteHeader(code)
}

func (w *timingResponseWriter) Write(b []byte) (int, error) {
    if !w.wroteHeader {
        w.WriteHeader(http.StatusOK)
    }
    return w.ResponseWriter.Write(b)
}
```

---

## cURL Examples

### Create Short URL

```bash
curl -X POST http://localhost:8080/shorten \
  -H "Content-Type: application/json" \
  -d '{"long_url": "https://example.com/very/long/path", "ttl_seconds": 3600}'
```

### Redirect

```bash
curl -I http://localhost:8080/s/Ab2CdE3F
```

### Get Stats

```bash
curl http://localhost:8080/stats/Ab2CdE3F
```

### Health Check

```bash
curl http://localhost:8080/health
```
