package repository_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"url-shortener/internal/domain"
	"url-shortener/internal/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryRepository_SaveIfNotExists_Success(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	record := &domain.URLRecord{
		ShortCode: "abc12345",
		LongURL:   "https://example.com",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	err := repo.SaveIfNotExists(ctx, record)
	assert.NoError(t, err)

	// Verify it was saved
	saved, err := repo.FindByShortCode(ctx, "abc12345")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", saved.LongURL)
}

func TestMemoryRepository_SaveIfNotExists_Duplicate(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	record := &domain.URLRecord{
		ShortCode: "abc12345",
		LongURL:   "https://example.com",
	}

	// First save succeeds
	err := repo.SaveIfNotExists(ctx, record)
	require.NoError(t, err)

	// Second save with same code fails
	record2 := &domain.URLRecord{
		ShortCode: "abc12345",
		LongURL:   "https://different.com",
	}
	err = repo.SaveIfNotExists(ctx, record2)
	assert.ErrorIs(t, err, domain.ErrCodeExists)
}

func TestMemoryRepository_SaveIfNotExists_StoresClone(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	record := &domain.URLRecord{
		ShortCode:  "abc12345",
		LongURL:    "https://example.com",
		ClickCount: 0,
	}

	err := repo.SaveIfNotExists(ctx, record)
	require.NoError(t, err)

	// Modify original after save
	record.ClickCount = 999

	// Stored record should be unaffected
	saved, _ := repo.FindByShortCode(ctx, "abc12345")
	assert.Equal(t, int64(0), saved.ClickCount)
}

func TestMemoryRepository_FindByShortCode_Success(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	record := &domain.URLRecord{
		ShortCode:  "abc12345",
		LongURL:    "https://example.com",
		ClickCount: 42,
	}
	_ = repo.SaveIfNotExists(ctx, record)

	found, err := repo.FindByShortCode(ctx, "abc12345")
	require.NoError(t, err)
	assert.Equal(t, "abc12345", found.ShortCode)
	assert.Equal(t, "https://example.com", found.LongURL)
	assert.Equal(t, int64(42), found.ClickCount)
}

func TestMemoryRepository_FindByShortCode_NotFound(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	_, err := repo.FindByShortCode(ctx, "notexist")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestMemoryRepository_FindByShortCode_ReturnsClone(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	record := &domain.URLRecord{
		ShortCode:  "abc12345",
		ClickCount: 10,
	}
	_ = repo.SaveIfNotExists(ctx, record)

	// Get record and modify it
	found, _ := repo.FindByShortCode(ctx, "abc12345")
	found.ClickCount = 999

	// Original in repo should be unaffected
	found2, _ := repo.FindByShortCode(ctx, "abc12345")
	assert.Equal(t, int64(10), found2.ClickCount)
}

func TestMemoryRepository_IncrementClickCount_Success(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	record := &domain.URLRecord{
		ShortCode:  "abc12345",
		ClickCount: 0,
	}
	_ = repo.SaveIfNotExists(ctx, record)

	accessTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	err := repo.IncrementClickCount(ctx, "abc12345", accessTime)
	require.NoError(t, err)

	found, _ := repo.FindByShortCode(ctx, "abc12345")
	assert.Equal(t, int64(1), found.ClickCount)
	assert.Equal(t, accessTime, found.LastAccessedAt)
}

func TestMemoryRepository_IncrementClickCount_NotFound(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	err := repo.IncrementClickCount(ctx, "notexist", time.Now())
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestMemoryRepository_IncrementClickCount_Multiple(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	record := &domain.URLRecord{
		ShortCode:  "abc12345",
		ClickCount: 0,
	}
	_ = repo.SaveIfNotExists(ctx, record)

	for i := 0; i < 100; i++ {
		_ = repo.IncrementClickCount(ctx, "abc12345", time.Now())
	}

	found, _ := repo.FindByShortCode(ctx, "abc12345")
	assert.Equal(t, int64(100), found.ClickCount)
}

func TestMemoryRepository_IncrementClickCount_Concurrent(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	record := &domain.URLRecord{
		ShortCode:  "abc12345",
		ClickCount: 0,
	}
	_ = repo.SaveIfNotExists(ctx, record)

	// 100 goroutines each incrementing 100 times
	const numGoroutines = 100
	const incrementsPerGoroutine = 100
	expectedTotal := int64(numGoroutines * incrementsPerGoroutine)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				err := repo.IncrementClickCount(ctx, "abc12345", time.Now())
				assert.NoError(t, err)
			}
		}()
	}

	wg.Wait()

	found, _ := repo.FindByShortCode(ctx, "abc12345")
	assert.Equal(t, expectedTotal, found.ClickCount,
		"click count should be exactly %d after concurrent increments", expectedTotal)
}

func TestMemoryRepository_SaveIfNotExists_ConcurrentCollision(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	const numGoroutines = 100
	code := "samecode"

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	var successCount int32
	var collisionCount int32

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			record := &domain.URLRecord{
				ShortCode: code,
				LongURL:   fmt.Sprintf("https://example.com/%d", id),
			}

			err := repo.SaveIfNotExists(ctx, record)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			} else if errors.Is(err, domain.ErrCodeExists) {
				atomic.AddInt32(&collisionCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Exactly one should succeed
	assert.Equal(t, int32(1), successCount)
	assert.Equal(t, int32(numGoroutines-1), collisionCount)
}

func TestMemoryRepository_DeleteExpired(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	// Create records with different expiry times
	records := []*domain.URLRecord{
		{ShortCode: "expired1", ExpiresAt: now.Add(-time.Hour)},   // Expired
		{ShortCode: "expired2", ExpiresAt: now.Add(-time.Minute)}, // Expired
		{ShortCode: "valid1", ExpiresAt: now.Add(time.Hour)},      // Valid
		{ShortCode: "valid2", ExpiresAt: now.Add(time.Minute)},    // Valid
	}

	for _, r := range records {
		_ = repo.SaveIfNotExists(ctx, r)
	}

	deleted, err := repo.DeleteExpired(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	// Verify expired are gone
	_, err = repo.FindByShortCode(ctx, "expired1")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	_, err = repo.FindByShortCode(ctx, "expired2")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	// Verify valid still exist
	_, err = repo.FindByShortCode(ctx, "valid1")
	assert.NoError(t, err)

	_, err = repo.FindByShortCode(ctx, "valid2")
	assert.NoError(t, err)
}

func TestMemoryRepository_DeleteExpired_Empty(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx := context.Background()

	deleted, err := repo.DeleteExpired(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestMemoryRepository_RespectsContextCancellation(t *testing.T) {
	repo := repository.NewMemoryRepository()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	record := &domain.URLRecord{ShortCode: "test1234"}

	err := repo.SaveIfNotExists(ctx, record)
	assert.ErrorIs(t, err, context.Canceled)

	_, err = repo.FindByShortCode(ctx, "test1234")
	assert.ErrorIs(t, err, context.Canceled)

	err = repo.IncrementClickCount(ctx, "test1234", time.Now())
	assert.ErrorIs(t, err, context.Canceled)

	_, err = repo.DeleteExpired(ctx, time.Now())
	assert.ErrorIs(t, err, context.Canceled)
}
