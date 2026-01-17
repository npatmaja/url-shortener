package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"url-shortener/internal/domain"
	"url-shortener/internal/repository"
	"url-shortener/internal/service"
	"url-shortener/internal/shortcode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockGenerator for testing collision scenarios.
type MockGenerator struct {
	codes []string
	index int
}

func (m *MockGenerator) Generate() string {
	if m.index >= len(m.codes) {
		return fmt.Sprintf("fallback%d", m.index)
	}
	code := m.codes[m.index]
	m.index++
	return code
}

func TestURLService_Create_Success(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	record, err := svc.Create(context.Background(), "https://example.com", time.Hour)
	require.NoError(t, err)

	assert.Len(t, record.ShortCode, 8)
	assert.Equal(t, "https://example.com", record.LongURL)
	assert.Equal(t, clock.Now(), record.CreatedAt)
	assert.Equal(t, clock.Now().Add(time.Hour), record.ExpiresAt)
	assert.Equal(t, int64(0), record.ClickCount)
}

func TestURLService_Create_UsesDefaultTTL(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	// Pass 0 duration to use default
	record, err := svc.Create(context.Background(), "https://example.com", 0)
	require.NoError(t, err)

	// Default TTL is 24 hours
	assert.Equal(t, clock.Now().Add(24*time.Hour), record.ExpiresAt)
}

func TestURLService_Create_StoresInRepository(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Now())

	svc := service.NewURLService(repo, gen, clock)

	record, err := svc.Create(context.Background(), "https://example.com", time.Hour)
	require.NoError(t, err)

	// Verify stored in repository
	stored, err := repo.FindByShortCode(context.Background(), record.ShortCode)
	require.NoError(t, err)
	assert.Equal(t, record.LongURL, stored.LongURL)
}

func TestURLService_Create_RetriesOnCollision(t *testing.T) {
	repo := repository.NewMemoryRepository()
	clock := domain.NewMockClock(time.Now())

	// Use a mock generator that returns predictable codes
	mockGen := &MockGenerator{
		codes: []string{"code0001", "code0001", "code0001", "code0004"},
	}

	svc := service.NewURLServiceWithGenerator(repo, mockGen, clock)

	// First create succeeds with code0001
	record1, err := svc.Create(context.Background(), "https://first.com", time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "code0001", record1.ShortCode)

	// Second create: code0001 collides, code0001 collides, code0001 collides, code0004 succeeds
	record2, err := svc.Create(context.Background(), "https://second.com", time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "code0004", record2.ShortCode)
}

func TestURLService_Create_FailsAfterMaxRetries(t *testing.T) {
	repo := repository.NewMemoryRepository()
	clock := domain.NewMockClock(time.Now())

	// Generator always returns same code
	mockGen := &MockGenerator{
		codes: []string{"samecode", "samecode", "samecode", "samecode", "samecode", "samecode"},
	}

	svc := service.NewURLServiceWithGenerator(repo, mockGen, clock)

	// First create succeeds
	_, err := svc.Create(context.Background(), "https://first.com", time.Hour)
	require.NoError(t, err)

	// Second create fails after 5 retries (all collide)
	_, err = svc.Create(context.Background(), "https://second.com", time.Hour)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max retries exceeded")
}

func TestURLService_Resolve_Success(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	// Create a URL
	record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

	// Resolve it
	longURL, err := svc.Resolve(context.Background(), record.ShortCode)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", longURL)
}

func TestURLService_Resolve_IncrementsClickCount(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

	// Resolve multiple times
	for i := 0; i < 5; i++ {
		_, err := svc.Resolve(context.Background(), record.ShortCode)
		require.NoError(t, err)
	}

	// Check click count
	stats, _ := svc.GetStats(context.Background(), record.ShortCode)
	assert.Equal(t, int64(5), stats.ClickCount)
}

func TestURLService_Resolve_UpdatesLastAccessedAt(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

	// Advance clock
	clock.Advance(30 * time.Minute)

	// Resolve
	_, _ = svc.Resolve(context.Background(), record.ShortCode)

	// Check LastAccessedAt
	stats, _ := svc.GetStats(context.Background(), record.ShortCode)
	assert.Equal(t, clock.Now(), stats.LastAccessedAt)
}

func TestURLService_Resolve_NotFound(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Now())

	svc := service.NewURLService(repo, gen, clock)

	_, err := svc.Resolve(context.Background(), "notexist")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestURLService_Resolve_Expired(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	// Create URL with 1 hour TTL
	record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

	// URL works before expiration
	_, err := svc.Resolve(context.Background(), record.ShortCode)
	require.NoError(t, err)

	// Advance clock past expiration
	clock.Advance(time.Hour + time.Second)

	// URL is now expired
	_, err = svc.Resolve(context.Background(), record.ShortCode)
	assert.ErrorIs(t, err, domain.ErrExpired)
}

func TestURLService_Resolve_JustBeforeExpiration(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

	// Advance to 1 second before expiration
	clock.Advance(time.Hour - time.Second)

	// Should still work
	longURL, err := svc.Resolve(context.Background(), record.ShortCode)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", longURL)
}

func TestURLService_GetStats_Success(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

	stats, err := svc.GetStats(context.Background(), record.ShortCode)
	require.NoError(t, err)

	assert.Equal(t, record.ShortCode, stats.ShortCode)
	assert.Equal(t, "https://example.com", stats.LongURL)
	assert.Equal(t, clock.Now(), stats.CreatedAt)
	assert.Equal(t, clock.Now().Add(time.Hour), stats.ExpiresAt)
	assert.Equal(t, int64(0), stats.ClickCount)
	assert.True(t, stats.LastAccessedAt.IsZero())
}

func TestURLService_GetStats_NotFound(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Now())

	svc := service.NewURLService(repo, gen, clock)

	_, err := svc.GetStats(context.Background(), "notexist")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestURLService_GetStats_Expired(t *testing.T) {
	repo := repository.NewMemoryRepository()
	gen := shortcode.NewGenerator()
	clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

	svc := service.NewURLService(repo, gen, clock)

	record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

	// Advance past expiration
	clock.Advance(2 * time.Hour)

	_, err := svc.GetStats(context.Background(), record.ShortCode)
	assert.ErrorIs(t, err, domain.ErrExpired)
}
