package repository

import (
	"context"
	"sync"
	"time"

	"url-shortener/internal/domain"
)

// MemoryRepository provides thread-safe in-memory storage.
type MemoryRepository struct {
	mu   sync.RWMutex
	data map[string]*domain.URLRecord
}

// NewMemoryRepository creates a new in-memory repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		data: make(map[string]*domain.URLRecord),
	}
}

// SaveIfNotExists atomically saves the record only if the short code
// doesn't already exist.
func (r *MemoryRepository) SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.data[record.ShortCode]; exists {
		return domain.ErrCodeExists
	}

	r.data[record.ShortCode] = record.Clone()
	return nil
}

// FindByShortCode retrieves a record by its short code.
func (r *MemoryRepository) FindByShortCode(ctx context.Context, code string) (*domain.URLRecord, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	record, exists := r.data[code]
	if !exists {
		return nil, domain.ErrNotFound
	}

	return record.Clone(), nil
}

// IncrementClickCount atomically increments the click counter.
func (r *MemoryRepository) IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.data[code]
	if !exists {
		return domain.ErrNotFound
	}

	record.ClickCount++
	record.LastAccessedAt = accessTime
	return nil
}

// DeleteExpired removes all records that have expired before the given time.
func (r *MemoryRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var deleted int64
	for code, record := range r.data {
		if record.ExpiresAt.Before(before) {
			delete(r.data, code)
			deleted++
		}
	}

	return deleted, nil
}
