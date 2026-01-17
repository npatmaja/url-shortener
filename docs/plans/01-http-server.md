# Plan: HTTP Server with Graceful Shutdown

## Overview

Implement the HTTP server entry point using Go standard library with proper graceful shutdown handling. This follows TDD: write tests first, then implement.

## Goals

- Create server that listens on configurable port
- Handle OS signals (SIGINT, SIGTERM) for graceful shutdown
- Allow in-flight requests to complete before shutdown
- Configurable shutdown timeout
- Health endpoint for readiness probes

## Directory Structure

```
cmd/
└── server/
    └── main.go          # Entry point
internal/
└── server/
    ├── server.go        # Server struct and lifecycle
    └── server_test.go   # Server tests
```

---

## TDD Steps

### Step 1: Test Server Starts and Responds to Health Check

**Test First:**

```go
// internal/server/server_test.go

package server_test

import (
    "context"
    "net/http"
    "testing"
    "time"

    "github.com/npatmaja/url-shortener/internal/server"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestServer_StartsAndRespondsToHealthCheck(t *testing.T) {
    // Arrange
    cfg := server.Config{
        Port:            8081, // Use non-standard port for tests
        ShutdownTimeout: 5 * time.Second,
    }
    srv := server.New(cfg)

    // Act - Start server in background
    go func() {
        _ = srv.Start()
    }()

    // Wait for server to be ready
    waitForServer(t, "http://localhost:8081/health", 2*time.Second)

    // Assert - Health endpoint responds
    resp, err := http.Get("http://localhost:8081/health")
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusOK, resp.StatusCode)

    // Cleanup
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    err = srv.Shutdown(ctx)
    assert.NoError(t, err)
}

func waitForServer(t *testing.T, url string, timeout time.Duration) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        resp, err := http.Get(url)
        if err == nil {
            resp.Body.Close()
            return
        }
        time.Sleep(10 * time.Millisecond)
    }
    t.Fatalf("server did not start within %v", timeout)
}
```

**Implementation:**

```go
// internal/server/server.go

package server

import (
    "context"
    "fmt"
    "net/http"
    "time"
)

type Config struct {
    Port            int
    ShutdownTimeout time.Duration
}

type Server struct {
    cfg        Config
    httpServer *http.Server
    mux        *http.ServeMux
}

func New(cfg Config) *Server {
    mux := http.NewServeMux()

    s := &Server{
        cfg: cfg,
        mux: mux,
        httpServer: &http.Server{
            Addr:    fmt.Sprintf(":%d", cfg.Port),
            Handler: mux,
        },
    }

    s.registerRoutes()
    return s
}

func (s *Server) registerRoutes() {
    s.mux.HandleFunc("GET /health", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"healthy"}`))
}

func (s *Server) Start() error {
    return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
    return s.httpServer.Shutdown(ctx)
}
```

---

### Step 2: Test Graceful Shutdown Waits for In-Flight Requests

**Test First:**

```go
func TestServer_GracefulShutdown_WaitsForInFlightRequests(t *testing.T) {
    cfg := server.Config{
        Port:            8082,
        ShutdownTimeout: 5 * time.Second,
    }
    srv := server.New(cfg)

    // Add a slow endpoint for testing
    srv.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(500 * time.Millisecond)
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("done"))
    })

    go func() {
        _ = srv.Start()
    }()

    waitForServer(t, "http://localhost:8082/health", 2*time.Second)

    // Start a slow request
    requestCompleted := make(chan bool, 1)
    go func() {
        resp, err := http.Get("http://localhost:8082/slow")
        if err == nil {
            resp.Body.Close()
            requestCompleted <- resp.StatusCode == http.StatusOK
        } else {
            requestCompleted <- false
        }
    }()

    // Give the request time to start
    time.Sleep(100 * time.Millisecond)

    // Initiate shutdown while request is in-flight
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    shutdownErr := srv.Shutdown(ctx)
    assert.NoError(t, shutdownErr)

    // Verify the in-flight request completed successfully
    select {
    case completed := <-requestCompleted:
        assert.True(t, completed, "in-flight request should complete")
    case <-time.After(2 * time.Second):
        t.Fatal("request did not complete")
    }
}
```

**Implementation:**

```go
// Add to server.go

// HandleFunc allows adding custom handlers (useful for testing)
func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
    s.mux.HandleFunc(pattern, handler)
}
```

---

### Step 3: Test Shutdown Times Out If Requests Take Too Long

**Test First:**

```go
func TestServer_GracefulShutdown_TimesOutIfRequestsTooSlow(t *testing.T) {
    cfg := server.Config{
        Port:            8083,
        ShutdownTimeout: 100 * time.Millisecond, // Very short timeout
    }
    srv := server.New(cfg)

    // Add a very slow endpoint
    srv.HandleFunc("GET /very-slow", func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(5 * time.Second) // Much longer than shutdown timeout
        w.WriteHeader(http.StatusOK)
    })

    go func() {
        _ = srv.Start()
    }()

    waitForServer(t, "http://localhost:8083/health", 2*time.Second)

    // Start a very slow request
    go func() {
        http.Get("http://localhost:8083/very-slow")
    }()

    time.Sleep(50 * time.Millisecond)

    // Shutdown with short timeout
    ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
    defer cancel()

    err := srv.Shutdown(ctx)

    // Should return context deadline exceeded
    assert.ErrorIs(t, err, context.DeadlineExceeded)
}
```

---

### Step 4: Test Server Handles OS Signals

**Test First:**

```go
func TestServer_Run_HandlesSignals(t *testing.T) {
    cfg := server.Config{
        Port:            8084,
        ShutdownTimeout: 5 * time.Second,
    }
    srv := server.New(cfg)

    // Channel to track if Run() returned
    done := make(chan error, 1)

    go func() {
        done <- srv.Run(context.Background())
    }()

    waitForServer(t, "http://localhost:8084/health", 2*time.Second)

    // Send interrupt signal to trigger shutdown
    // Note: In tests, we use context cancellation instead of OS signals
    ctx, cancel := context.WithCancel(context.Background())

    go func() {
        srv2 := server.New(server.Config{Port: 8085, ShutdownTimeout: 5 * time.Second})
        done <- srv2.Run(ctx)
    }()

    waitForServer(t, "http://localhost:8085/health", 2*time.Second)

    // Cancel context (simulates SIGTERM)
    cancel()

    select {
    case err := <-done:
        // Should exit cleanly (nil or http.ErrServerClosed)
        if err != nil && err != http.ErrServerClosed {
            t.Errorf("unexpected error: %v", err)
        }
    case <-time.After(3 * time.Second):
        t.Fatal("server did not shutdown")
    }
}
```

**Implementation:**

```go
// Add to server.go

import (
    "os"
    "os/signal"
    "syscall"
)

// Run starts the server and blocks until shutdown signal is received.
// It handles graceful shutdown on SIGINT and SIGTERM.
func (s *Server) Run(ctx context.Context) error {
    // Channel for shutdown signals
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    // Channel for server errors
    errChan := make(chan error, 1)

    // Start server
    go func() {
        if err := s.Start(); err != nil && err != http.ErrServerClosed {
            errChan <- err
        }
    }()

    // Wait for shutdown signal or context cancellation
    select {
    case sig := <-sigChan:
        fmt.Printf("received signal: %v\n", sig)
    case <-ctx.Done():
        fmt.Println("context cancelled")
    case err := <-errChan:
        return fmt.Errorf("server error: %w", err)
    }

    // Graceful shutdown
    shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
    defer cancel()

    return s.Shutdown(shutdownCtx)
}
```

---

### Step 5: Test Server Adds Processing Time Header

**Test First:**

```go
func TestServer_AddsProcessingTimeHeader(t *testing.T) {
    cfg := server.Config{
        Port:            8086,
        ShutdownTimeout: 5 * time.Second,
    }
    srv := server.New(cfg)

    go func() {
        _ = srv.Start()
    }()

    waitForServer(t, "http://localhost:8086/health", 2*time.Second)
    defer func() {
        ctx, cancel := context.WithTimeout(context.Background(), time.Second)
        defer cancel()
        srv.Shutdown(ctx)
    }()

    resp, err := http.Get("http://localhost:8086/health")
    require.NoError(t, err)
    defer resp.Body.Close()

    // Check X-Processing-Time-Micros header exists
    header := resp.Header.Get("X-Processing-Time-Micros")
    assert.NotEmpty(t, header, "X-Processing-Time-Micros header should be present")

    // Verify it's a valid number
    _, err = strconv.ParseInt(header, 10, 64)
    assert.NoError(t, err, "header should be a valid integer")
}
```

**Implementation:**

```go
// internal/middleware/timing.go

package middleware

import (
    "net/http"
    "strconv"
    "time"
)

func Timing(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()

        wrapped := &responseWriter{
            ResponseWriter: w,
            start:          start,
        }

        next.ServeHTTP(wrapped, r)
    })
}

type responseWriter struct {
    http.ResponseWriter
    start       time.Time
    wroteHeader bool
}

func (w *responseWriter) WriteHeader(code int) {
    if !w.wroteHeader {
        micros := time.Since(w.start).Microseconds()
        w.Header().Set("X-Processing-Time-Micros", strconv.FormatInt(micros, 10))
        w.wroteHeader = true
    }
    w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
    if !w.wroteHeader {
        w.WriteHeader(http.StatusOK)
    }
    return w.ResponseWriter.Write(b)
}
```

Update server to use middleware:

```go
func New(cfg Config) *Server {
    mux := http.NewServeMux()

    s := &Server{
        cfg: cfg,
        mux: mux,
        httpServer: &http.Server{
            Addr:    fmt.Sprintf(":%d", cfg.Port),
            Handler: middleware.Timing(mux), // Wrap with timing middleware
        },
    }

    s.registerRoutes()
    return s
}
```

---

## Final Server Structure

```go
// internal/server/server.go

package server

import (
    "context"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/npatmaja/url-shortener/internal/middleware"
)

type Config struct {
    Port            int
    ShutdownTimeout time.Duration
}

type Server struct {
    cfg        Config
    httpServer *http.Server
    mux        *http.ServeMux
}

func New(cfg Config) *Server {
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

    s.registerRoutes()
    return s
}

func (s *Server) registerRoutes() {
    s.mux.HandleFunc("GET /health", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"healthy"}`))
}

func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
    s.mux.HandleFunc(pattern, handler)
}

func (s *Server) Start() error {
    return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
    return s.httpServer.Shutdown(ctx)
}

func (s *Server) Run(ctx context.Context) error {
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    errChan := make(chan error, 1)

    go func() {
        if err := s.Start(); err != nil && err != http.ErrServerClosed {
            errChan <- err
        }
    }()

    select {
    case sig := <-sigChan:
        fmt.Printf("received signal: %v\n", sig)
    case <-ctx.Done():
    case err := <-errChan:
        return fmt.Errorf("server error: %w", err)
    }

    shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
    defer cancel()

    return s.Shutdown(shutdownCtx)
}
```

---

## Entry Point

```go
// cmd/server/main.go

package main

import (
    "context"
    "log"
    "os"
    "strconv"
    "time"

    "github.com/npatmaja/url-shortener/internal/server"
)

func main() {
    port := getEnvInt("PORT", 8080)
    shutdownTimeout := getEnvDuration("SHUTDOWN_TIMEOUT", 30*time.Second)

    cfg := server.Config{
        Port:            port,
        ShutdownTimeout: shutdownTimeout,
    }

    srv := server.New(cfg)

    log.Printf("starting server on port %d", port)

    if err := srv.Run(context.Background()); err != nil {
        log.Fatalf("server error: %v", err)
        os.Exit(1)
    }

    log.Println("server stopped gracefully")
}

func getEnvInt(key string, defaultVal int) int {
    if val := os.Getenv(key); val != "" {
        if i, err := strconv.Atoi(val); err == nil {
            return i
        }
    }
    return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
    if val := os.Getenv(key); val != "" {
        if d, err := time.ParseDuration(val); err == nil {
            return d
        }
    }
    return defaultVal
}
```

---

## Checklist

- [x] Write test: Server starts and responds to health check
- [x] Implement: Basic server with health endpoint
- [x] Write test: Graceful shutdown waits for in-flight requests
- [x] Implement: Proper shutdown handling
- [x] Write test: Shutdown times out if requests too slow
- [x] Verify: Context deadline behavior
- [x] Write test: Server handles OS signals
- [x] Implement: Signal handling with Run()
- [x] Write test: Processing time header added
- [x] Implement: Timing middleware
- [x] Create main.go entry point
- [x] Run all tests with race detector

## Completion Notes

**Completed on:** Implementation complete

**Tests:** 11 tests passing (6 server + 5 middleware)

**Files created:**
- `cmd/server/main.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/middleware/timing.go`
- `internal/middleware/timing_test.go`

**Deviations from plan:**
- Used `log/slog` instead of `log` for structured logging in main.go
- Added `signal.Stop(sigChan)` in Run() for proper cleanup
- Used higher port numbers (18081-18086) in tests to avoid conflicts
