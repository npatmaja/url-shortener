package server_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"url-shortener/internal/server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_StartsAndRespondsToHealthCheck(t *testing.T) {
	// Arrange
	cfg := server.Config{
		Port:            18081,
		ShutdownTimeout: 5 * time.Second,
	}
	srv := server.New(cfg)

	// Act - Start server in background
	go func() {
		_ = srv.Start()
	}()

	// Wait for server to be ready
	waitForServer(t, "http://localhost:18081/health", 2*time.Second)

	// Assert - Health endpoint responds
	resp, err := http.Get("http://localhost:18081/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestServer_GracefulShutdown_WaitsForInFlightRequests(t *testing.T) {
	cfg := server.Config{
		Port:            18082,
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

	waitForServer(t, "http://localhost:18082/health", 2*time.Second)

	// Start a slow request
	requestCompleted := make(chan bool, 1)
	go func() {
		resp, err := http.Get("http://localhost:18082/slow")
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

func TestServer_Run_ShutdownOnContextCancel(t *testing.T) {
	cfg := server.Config{
		Port:            18084,
		ShutdownTimeout: 5 * time.Second,
	}
	srv := server.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	// Channel to track if Run() returned
	done := make(chan error, 1)

	go func() {
		done <- srv.Run(ctx)
	}()

	waitForServer(t, "http://localhost:18084/health", 2*time.Second)

	// Cancel context (simulates SIGTERM)
	cancel()

	select {
	case err := <-done:
		// Should exit cleanly (nil or http.ErrServerClosed is acceptable)
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shutdown")
	}
}

func TestServer_Run_CompletesInFlightRequestsOnShutdown(t *testing.T) {
	cfg := server.Config{
		Port:            18085,
		ShutdownTimeout: 5 * time.Second,
	}
	srv := server.New(cfg)

	srv.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("completed"))
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	waitForServer(t, "http://localhost:18085/health", 2*time.Second)

	// Start a slow request
	requestDone := make(chan bool, 1)
	go func() {
		resp, err := http.Get("http://localhost:18085/slow")
		if err == nil {
			resp.Body.Close()
			requestDone <- resp.StatusCode == http.StatusOK
		} else {
			requestDone <- false
		}
	}()

	// Give request time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context while request in flight
	cancel()

	// Request should still complete
	select {
	case completed := <-requestDone:
		assert.True(t, completed, "in-flight request should complete")
	case <-time.After(2 * time.Second):
		t.Fatal("request did not complete")
	}

	// Server should shutdown cleanly
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shutdown")
	}
}

func TestServer_GracefulShutdown_TimesOutIfRequestsTooSlow(t *testing.T) {
	cfg := server.Config{
		Port:            18083,
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

	waitForServer(t, "http://localhost:18083/health", 2*time.Second)

	// Start a very slow request
	go func() {
		http.Get("http://localhost:18083/very-slow")
	}()

	time.Sleep(50 * time.Millisecond)

	// Shutdown with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	err := srv.Shutdown(ctx)

	// Should return context deadline exceeded
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestServer_AddsProcessingTimeHeader(t *testing.T) {
	cfg := server.Config{
		Port:            18086,
		ShutdownTimeout: 5 * time.Second,
	}
	srv := server.New(cfg)

	go func() {
		_ = srv.Start()
	}()

	waitForServer(t, "http://localhost:18086/health", 2*time.Second)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	resp, err := http.Get("http://localhost:18086/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Check X-Processing-Time-Micros header exists
	header := resp.Header.Get("X-Processing-Time-Micros")
	assert.NotEmpty(t, header, "X-Processing-Time-Micros header should be present")

	// Verify it's a valid number
	_, err = strconv.ParseInt(header, 10, 64)
	assert.NoError(t, err, "header should be a valid integer")
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
