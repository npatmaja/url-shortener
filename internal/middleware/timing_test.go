package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"url-shortener/internal/middleware"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTiming_AddsProcessingTimeHeader(t *testing.T) {
	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with timing middleware
	wrapped := middleware.Timing(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Check header exists
	header := rec.Header().Get("X-Processing-Time-Micros")
	assert.NotEmpty(t, header, "X-Processing-Time-Micros header should be present")

	// Check it's a valid number
	micros, err := strconv.ParseInt(header, 10, 64)
	require.NoError(t, err, "header should be a valid integer")
	assert.GreaterOrEqual(t, micros, int64(0), "processing time should be non-negative")
}

func TestTiming_MeasuresActualProcessingTime(t *testing.T) {
	// Create a slow handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Timing(handler)

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	header := rec.Header().Get("X-Processing-Time-Micros")
	micros, err := strconv.ParseInt(header, 10, 64)
	require.NoError(t, err)

	// Should be at least 50ms (50000 microseconds)
	assert.GreaterOrEqual(t, micros, int64(50000), "should measure at least 50ms")
	// But not more than 200ms (allowing for test environment variance)
	assert.Less(t, micros, int64(200000), "should not be unreasonably high")
}

func TestTiming_WorksWithImplicitStatusOK(t *testing.T) {
	// Handler that doesn't call WriteHeader explicitly
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK")) // Implicit 200 OK
	})

	wrapped := middleware.Timing(handler)

	req := httptest.NewRequest(http.MethodGet, "/implicit", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("X-Processing-Time-Micros"))
}

func TestTiming_PreservesOtherHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "custom-value")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"status":"created"}`))
	})

	wrapped := middleware.Timing(handler)

	req := httptest.NewRequest(http.MethodPost, "/create", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, "custom-value", rec.Header().Get("X-Custom-Header"))
	assert.NotEmpty(t, rec.Header().Get("X-Processing-Time-Micros"))
}

func TestTiming_WorksWithErrorResponses(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	})

	wrapped := middleware.Timing(handler)

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("X-Processing-Time-Micros"))
}
