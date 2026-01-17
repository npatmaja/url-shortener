package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"url-shortener/internal/domain"
	"url-shortener/internal/handler"
	"url-shortener/internal/server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// StubURLService is a simple stub implementation for integration testing
type StubURLService struct {
	records map[string]*domain.URLRecord
	counter int
}

func NewStubURLService() *StubURLService {
	return &StubURLService{
		records: make(map[string]*domain.URLRecord),
		counter: 0,
	}
}

func (s *StubURLService) Create(ctx context.Context, longURL string, ttl time.Duration) (*domain.URLRecord, error) {
	s.counter++
	shortCode := fmt.Sprintf("code%04d", s.counter)
	record := &domain.URLRecord{
		ShortCode:  shortCode,
		LongURL:    longURL,
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(ttl),
		ClickCount: 0,
	}
	s.records[record.ShortCode] = record
	return record, nil
}

func (s *StubURLService) Resolve(ctx context.Context, shortCode string) (string, error) {
	record, ok := s.records[shortCode]
	if !ok {
		return "", domain.ErrNotFound
	}
	if time.Now().After(record.ExpiresAt) {
		return "", domain.ErrExpired
	}
	record.ClickCount++
	record.LastAccessedAt = time.Now().UTC()
	return record.LongURL, nil
}

func (s *StubURLService) GetStats(ctx context.Context, shortCode string) (*domain.URLRecord, error) {
	record, ok := s.records[shortCode]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return record, nil
}

func TestIntegration_FullWorkflow(t *testing.T) {
	// Setup
	stubService := NewStubURLService()
	cfg := server.Config{
		Port:            18090,
		ShutdownTimeout: 5 * time.Second,
		BaseURL:         "http://localhost:18090",
	}
	srv := server.New(cfg, stubService)

	// Start server
	go func() {
		_ = srv.Start()
	}()

	baseURL := "http://localhost:18090"
	waitForServer(t, baseURL+"/health", 2*time.Second)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	// Test 1: Health check
	t.Run("health check returns 200", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var health handler.HealthResponse
		err = json.NewDecoder(resp.Body).Decode(&health)
		require.NoError(t, err)
		assert.Equal(t, "healthy", health.Status)
		assert.NotEmpty(t, health.Timestamp)
	})

	// Test 2: Create short URL
	var createdShortCode string
	t.Run("create short URL returns 201", func(t *testing.T) {
		payload := `{"long_url": "https://example.com/test-page"}`
		resp, err := http.Post(baseURL+"/shorten", "application/json", bytes.NewBufferString(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var createResp handler.CreateResponse
		err = json.NewDecoder(resp.Body).Decode(&createResp)
		require.NoError(t, err)

		assert.NotEmpty(t, createResp.ShortCode)
		assert.Equal(t, "https://example.com/test-page", createResp.LongURL)
		assert.Contains(t, createResp.ShortURL, "/s/"+createResp.ShortCode)
		assert.NotEmpty(t, createResp.ExpiresAt)

		createdShortCode = createResp.ShortCode
	})

	// Test 3: Create with custom TTL
	t.Run("create with custom TTL", func(t *testing.T) {
		payload := `{"long_url": "https://example.com/custom-ttl", "ttl_seconds": 3600}`
		resp, err := http.Post(baseURL+"/shorten", "application/json", bytes.NewBufferString(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// Test 4: Redirect to original URL
	t.Run("redirect returns 302 with Location header", func(t *testing.T) {
		// Use a client that doesn't follow redirects
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		resp, err := client.Get(baseURL + "/s/" + createdShortCode)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusFound, resp.StatusCode)
		assert.Equal(t, "https://example.com/test-page", resp.Header.Get("Location"))
	})

	// Test 5: Get stats
	t.Run("stats returns 200 with click count", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/stats/" + createdShortCode)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var stats handler.StatsResponse
		err = json.NewDecoder(resp.Body).Decode(&stats)
		require.NoError(t, err)

		assert.Equal(t, createdShortCode, stats.ShortCode)
		assert.Equal(t, "https://example.com/test-page", stats.LongURL)
		assert.Equal(t, int64(1), stats.ClickCount) // One redirect was made
		assert.NotNil(t, stats.LastAccessedAt)
	})

	// Test 6: Redirect not found
	t.Run("redirect not found returns 404", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		resp, err := client.Get(baseURL + "/s/nonexistent")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		var errResp handler.ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Equal(t, "not_found", errResp.Error)
	})

	// Test 7: Stats not found
	t.Run("stats not found returns 404", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/stats/nonexistent")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	// Test 8: Processing time header is present
	t.Run("responses include processing time header", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		header := resp.Header.Get("X-Processing-Time-Micros")
		assert.NotEmpty(t, header, "X-Processing-Time-Micros header should be present")
	})
}

func TestIntegration_ValidationErrors(t *testing.T) {
	// Setup
	stubService := NewStubURLService()
	cfg := server.Config{
		Port:            18091,
		ShutdownTimeout: 5 * time.Second,
		BaseURL:         "http://localhost:18091",
	}
	srv := server.New(cfg, stubService)

	go func() {
		_ = srv.Start()
	}()

	baseURL := "http://localhost:18091"
	waitForServer(t, baseURL+"/health", 2*time.Second)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	testCases := []struct {
		name           string
		payload        string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "empty URL",
			payload:        `{"long_url": ""}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "validation_error",
		},
		{
			name:           "invalid scheme",
			payload:        `{"long_url": "ftp://example.com"}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "validation_error",
		},
		{
			name:           "missing scheme",
			payload:        `{"long_url": "example.com"}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "validation_error",
		},
		{
			name:           "invalid JSON",
			payload:        `not valid json`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(baseURL+"/shorten", "application/json", bytes.NewBufferString(tc.payload))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.expectedStatus, resp.StatusCode)

			var errResp handler.ErrorResponse
			err = json.NewDecoder(resp.Body).Decode(&errResp)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedError, errResp.Error)
		})
	}
}

func TestIntegration_ContentTypeJSON(t *testing.T) {
	// Setup
	stubService := NewStubURLService()
	cfg := server.Config{
		Port:            18092,
		ShutdownTimeout: 5 * time.Second,
		BaseURL:         "http://localhost:18092",
	}
	srv := server.New(cfg, stubService)

	go func() {
		_ = srv.Start()
	}()

	baseURL := "http://localhost:18092"
	waitForServer(t, baseURL+"/health", 2*time.Second)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	t.Run("health endpoint returns JSON content type", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		assert.Equal(t, "application/json", contentType)
	})

	t.Run("create endpoint returns JSON content type", func(t *testing.T) {
		payload := `{"long_url": "https://example.com"}`
		resp, err := http.Post(baseURL+"/shorten", "application/json", bytes.NewBufferString(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		assert.Equal(t, "application/json", contentType)
	})

	t.Run("stats endpoint returns JSON content type", func(t *testing.T) {
		// First create a URL
		payload := `{"long_url": "https://example.com"}`
		createResp, _ := http.Post(baseURL+"/shorten", "application/json", bytes.NewBufferString(payload))
		var created handler.CreateResponse
		json.NewDecoder(createResp.Body).Decode(&created)
		createResp.Body.Close()

		resp, err := http.Get(baseURL + "/stats/" + created.ShortCode)
		require.NoError(t, err)
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		assert.Equal(t, "application/json", contentType)
	})

	t.Run("error responses return JSON content type", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/stats/nonexistent")
		require.NoError(t, err)
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		assert.Equal(t, "application/json", contentType)
	})
}
