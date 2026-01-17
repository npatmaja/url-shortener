package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"url-shortener/internal/handler"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestStatsHandler_ValidCode_Returns200(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	lastAccessed := time.Date(2024, 1, 15, 15, 30, 0, 0, time.UTC)
	expectedRecord := &handler.URLRecord{
		ShortCode:      "Ab2CdE3F",
		LongURL:        "https://example.com",
		CreatedAt:      time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		ExpiresAt:      time.Date(2024, 1, 16, 12, 0, 0, 0, time.UTC),
		ClickCount:     42,
		LastAccessedAt: lastAccessed,
	}

	mockService.On("GetStats", mock.Anything, "Ab2CdE3F").
		Return(expectedRecord, nil)

	req := httptest.NewRequest(http.MethodGet, "/stats/Ab2CdE3F", nil)
	req.SetPathValue("code", "Ab2CdE3F")

	rec := httptest.NewRecorder()

	h.Stats(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp handler.StatsResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "Ab2CdE3F", resp.ShortCode)
	assert.Equal(t, "https://example.com", resp.LongURL)
	assert.Equal(t, "2024-01-15T12:00:00Z", resp.CreatedAt)
	assert.Equal(t, "2024-01-16T12:00:00Z", resp.ExpiresAt)
	assert.Equal(t, int64(42), resp.ClickCount)
	assert.NotNil(t, resp.LastAccessedAt)
	assert.Equal(t, "2024-01-15T15:30:00Z", *resp.LastAccessedAt)
}

func TestStatsHandler_NeverAccessed_LastAccessedIsNull(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	expectedRecord := &handler.URLRecord{
		ShortCode:      "newcode1",
		LongURL:        "https://example.com",
		CreatedAt:      time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		ExpiresAt:      time.Date(2024, 1, 16, 12, 0, 0, 0, time.UTC),
		ClickCount:     0,
		LastAccessedAt: time.Time{}, // Zero value = never accessed
	}

	mockService.On("GetStats", mock.Anything, "newcode1").
		Return(expectedRecord, nil)

	req := httptest.NewRequest(http.MethodGet, "/stats/newcode1", nil)
	req.SetPathValue("code", "newcode1")

	rec := httptest.NewRecorder()

	h.Stats(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp handler.StatsResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	assert.Nil(t, resp.LastAccessedAt)
	assert.Equal(t, int64(0), resp.ClickCount)
}

func TestStatsHandler_NotFound_Returns404(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	mockService.On("GetStats", mock.Anything, "notfound").
		Return(nil, handler.ErrNotFound)

	req := httptest.NewRequest(http.MethodGet, "/stats/notfound", nil)
	req.SetPathValue("code", "notfound")

	rec := httptest.NewRecorder()

	h.Stats(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
