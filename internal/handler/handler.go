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
