package repository

import (
	"context"
	"time"

	"url-shortener/internal/domain"
)

// Repository defines the contract for URL storage operations.
// All implementations must be thread-safe for concurrent access.
type Repository interface {
	// SaveIfNotExists atomically saves the record only if the short code
	// doesn't already exist. Returns domain.ErrCodeExists if taken.
	SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error

	// FindByShortCode retrieves a record by its short code.
	// Returns domain.ErrNotFound if the code doesn't exist.
	FindByShortCode(ctx context.Context, code string) (*domain.URLRecord, error)

	// IncrementClickCount atomically increments the click counter
	// and updates LastAccessedAt timestamp.
	// Returns domain.ErrNotFound if the code doesn't exist.
	IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error

	// DeleteExpired removes all records where ExpiresAt < before.
	// Returns the number of deleted records.
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
