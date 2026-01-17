package domain_test

import (
	"testing"
	"time"

	"url-shortener/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestURLRecord_IsExpired(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt time.Time
		checkTime time.Time
		want      bool
	}{
		{
			name:      "not expired - before expiry",
			expiresAt: now.Add(time.Hour),
			checkTime: now,
			want:      false,
		},
		{
			name:      "expired - after expiry",
			expiresAt: now,
			checkTime: now.Add(time.Second),
			want:      true,
		},
		{
			name:      "not expired - exactly at expiry",
			expiresAt: now,
			checkTime: now,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := &domain.URLRecord{
				ExpiresAt: tt.expiresAt,
			}
			assert.Equal(t, tt.want, record.IsExpired(tt.checkTime))
		})
	}
}

func TestURLRecord_Clone(t *testing.T) {
	original := &domain.URLRecord{
		ShortCode:      "abc12345",
		LongURL:        "https://example.com",
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(time.Hour),
		ClickCount:     42,
		LastAccessedAt: time.Now(),
	}

	clone := original.Clone()

	// Should be equal
	assert.Equal(t, original.ShortCode, clone.ShortCode)
	assert.Equal(t, original.LongURL, clone.LongURL)
	assert.Equal(t, original.ClickCount, clone.ClickCount)

	// Should be independent (modifying clone doesn't affect original)
	clone.ClickCount = 100
	assert.Equal(t, int64(42), original.ClickCount)
}
