package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"url-shortener/internal/handler"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestRedirectHandler_ValidCode_Returns302(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	mockService.On("Resolve", mock.Anything, "Ab2CdE3F").
		Return("https://example.com/destination", nil)

	req := httptest.NewRequest(http.MethodGet, "/s/Ab2CdE3F", nil)
	req.SetPathValue("code", "Ab2CdE3F")

	rec := httptest.NewRecorder()

	h.Redirect(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "https://example.com/destination", rec.Header().Get("Location"))

	mockService.AssertExpectations(t)
}

func TestRedirectHandler_NotFound_Returns404(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	mockService.On("Resolve", mock.Anything, "notfound").
		Return("", handler.ErrNotFound)

	req := httptest.NewRequest(http.MethodGet, "/s/notfound", nil)
	req.SetPathValue("code", "notfound")

	rec := httptest.NewRecorder()

	h.Redirect(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRedirectHandler_Expired_Returns404(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	mockService.On("Resolve", mock.Anything, "expired1").
		Return("", handler.ErrExpired)

	req := httptest.NewRequest(http.MethodGet, "/s/expired1", nil)
	req.SetPathValue("code", "expired1")

	rec := httptest.NewRecorder()

	h.Redirect(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Response should say "not found or expired"
	assert.Contains(t, rec.Body.String(), "not found or expired")
}

func TestRedirectHandler_ServiceError_Returns500(t *testing.T) {
	mockService := new(MockURLService)
	h := handler.New(mockService, "http://localhost:8080")

	mockService.On("Resolve", mock.Anything, "error123").
		Return("", errors.New("database connection failed"))

	req := httptest.NewRequest(http.MethodGet, "/s/error123", nil)
	req.SetPathValue("code", "error123")

	rec := httptest.NewRecorder()

	h.Redirect(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
