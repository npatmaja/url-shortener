package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"url-shortener/internal/domain"
	"url-shortener/internal/handler"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockURLService implements handler.URLService for testing
type MockURLService struct {
	mock.Mock
}

func (m *MockURLService) Create(ctx context.Context, longURL string, ttl time.Duration) (*domain.URLRecord, error) {
	args := m.Called(ctx, longURL, ttl)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.URLRecord), args.Error(1)
}

func (m *MockURLService) Resolve(ctx context.Context, shortCode string) (string, error) {
	args := m.Called(ctx, shortCode)
	return args.String(0), args.Error(1)
}

func (m *MockURLService) GetStats(ctx context.Context, shortCode string) (*domain.URLRecord, error) {
	args := m.Called(ctx, shortCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.URLRecord), args.Error(1)
}

func TestCreateHandler_ValidRequest_Returns201(t *testing.T) {
	// Arrange
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	expectedRecord := &domain.URLRecord{
		ShortCode: "Ab2CdE3F",
		LongURL:   "https://example.com/path",
		CreatedAt: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2024, 1, 16, 12, 0, 0, 0, time.UTC),
	}

	mockService.On("Create", mock.Anything, "https://example.com/path", 24*time.Hour).
		Return(expectedRecord, nil)

	body := `{"long_url": "https://example.com/path"}`
	req := httptest.NewRequest(http.MethodPost, "/shorten", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	// Act
	h.Create(rec, req)

	// Assert
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp handler.CreateResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "Ab2CdE3F", resp.ShortCode)
	assert.Equal(t, "http://localhost:8080/s/Ab2CdE3F", resp.ShortURL)
	assert.Equal(t, "https://example.com/path", resp.LongURL)
	assert.Equal(t, "2024-01-16T12:00:00Z", resp.ExpiresAt)

	mockService.AssertExpectations(t)
}

func TestCreateHandler_WithCustomTTL_UsesTTL(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	expectedRecord := &domain.URLRecord{
		ShortCode: "Ab2CdE3F",
		LongURL:   "https://example.com",
		ExpiresAt: time.Date(2024, 1, 15, 13, 0, 0, 0, time.UTC),
	}

	// Expect TTL of 3600 seconds = 1 hour
	mockService.On("Create", mock.Anything, "https://example.com", time.Hour).
		Return(expectedRecord, nil)

	body := `{"long_url": "https://example.com", "ttl_seconds": 3600}`
	req := httptest.NewRequest(http.MethodPost, "/shorten", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	mockService.AssertExpectations(t)
}

func TestCreateHandler_InvalidURL_Returns400(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	testCases := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{
			name:        "empty URL",
			body:        `{"long_url": ""}`,
			wantMessage: "long_url is required",
		},
		{
			name:        "missing scheme",
			body:        `{"long_url": "example.com"}`,
			wantMessage: "scheme must be http or https",
		},
		{
			name:        "ftp scheme",
			body:        `{"long_url": "ftp://example.com"}`,
			wantMessage: "scheme must be http or https",
		},
		{
			name:        "no host",
			body:        `{"long_url": "https://"}`,
			wantMessage: "URL must have a host",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/shorten", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()

			h.Create(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)

			var resp handler.ErrorResponse
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err)

			assert.Equal(t, "validation_error", resp.Error)
			assert.Contains(t, resp.Message, tc.wantMessage)
		})
	}

	// Service should never be called for invalid requests
	mockService.AssertNotCalled(t, "Create")
}

func TestCreateHandler_InvalidJSON_Returns400(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodPost, "/shorten", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp handler.ErrorResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "invalid_json", resp.Error)
}

func TestCreateHandler_URLTooLong_Returns400(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	// Create URL longer than 2048 chars
	longURL := "https://example.com/" + string(make([]byte, 2100))
	body, _ := json.Marshal(handler.CreateRequest{LongURL: longURL})

	req := httptest.NewRequest(http.MethodPost, "/shorten", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp handler.ErrorResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.Contains(t, resp.Message, "exceeds maximum length")
}
