package domain

import "time"

// URLRecord represents a shortened URL entry.
type URLRecord struct {
	ShortCode      string
	LongURL        string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ClickCount     int64
	LastAccessedAt time.Time
}

// IsExpired returns true if the record has expired at the given time.
func (r *URLRecord) IsExpired(now time.Time) bool {
	return now.After(r.ExpiresAt)
}

// Clone creates a deep copy of the record.
func (r *URLRecord) Clone() *URLRecord {
	return &URLRecord{
		ShortCode:      r.ShortCode,
		LongURL:        r.LongURL,
		CreatedAt:      r.CreatedAt,
		ExpiresAt:      r.ExpiresAt,
		ClickCount:     r.ClickCount,
		LastAccessedAt: r.LastAccessedAt,
	}
}
