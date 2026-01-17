package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"url-shortener/internal/domain"
	"url-shortener/internal/repository"
	"url-shortener/internal/shortcode"
)

const (
	maxRetries = 5
	defaultTTL = 24 * time.Hour
)

// CodeGenerator defines the interface for short code generation.
type CodeGenerator interface {
	Generate() string
}

// URLService handles URL shortening business logic.
type URLService struct {
	repo      repository.Repository
	generator CodeGenerator
	clock     domain.Clock
}

// NewURLService creates a new URLService with the default generator.
func NewURLService(repo repository.Repository, generator *shortcode.Generator, clock domain.Clock) *URLService {
	return &URLService{
		repo:      repo,
		generator: generator,
		clock:     clock,
	}
}

// NewURLServiceWithGenerator creates a URLService with a custom generator (for testing).
func NewURLServiceWithGenerator(repo repository.Repository, generator CodeGenerator, clock domain.Clock) *URLService {
	return &URLService{
		repo:      repo,
		generator: generator,
		clock:     clock,
	}
}

// Create creates a new shortened URL with the given TTL.
// If ttl is 0, the default TTL (24 hours) is used.
// Returns the created record or an error if max retries exceeded.
func (s *URLService) Create(ctx context.Context, longURL string, ttl time.Duration) (*domain.URLRecord, error) {
	if ttl == 0 {
		ttl = defaultTTL
	}

	now := s.clock.Now()

	for attempt := 0; attempt < maxRetries; attempt++ {
		code := s.generator.Generate()

		record := &domain.URLRecord{
			ShortCode:      code,
			LongURL:        longURL,
			CreatedAt:      now,
			ExpiresAt:      now.Add(ttl),
			ClickCount:     0,
			LastAccessedAt: time.Time{},
		}

		err := s.repo.SaveIfNotExists(ctx, record)
		if err == nil {
			return record, nil
		}

		if errors.Is(err, domain.ErrCodeExists) {
			continue // Collision, retry with new code
		}

		return nil, fmt.Errorf("saving record: %w", err)
	}

	return nil, errors.New("max retries exceeded: unable to generate unique code")
}

// Resolve returns the long URL for the given short code.
// It increments the click count and updates LastAccessedAt.
// Returns domain.ErrNotFound if not found, domain.ErrExpired if expired.
func (s *URLService) Resolve(ctx context.Context, shortCode string) (string, error) {
	record, err := s.repo.FindByShortCode(ctx, shortCode)
	if err != nil {
		return "", err
	}

	// Check expiration
	if record.IsExpired(s.clock.Now()) {
		return "", domain.ErrExpired
	}

	// Increment click count (fire and forget - don't block redirect)
	_ = s.repo.IncrementClickCount(ctx, shortCode, s.clock.Now())

	return record.LongURL, nil
}

// GetStats returns the full record for the given short code.
// Returns domain.ErrNotFound if not found, domain.ErrExpired if expired.
func (s *URLService) GetStats(ctx context.Context, shortCode string) (*domain.URLRecord, error) {
	record, err := s.repo.FindByShortCode(ctx, shortCode)
	if err != nil {
		return nil, err
	}

	if record.IsExpired(s.clock.Now()) {
		return nil, domain.ErrExpired
	}

	return record, nil
}
