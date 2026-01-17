package handler

// === Requests ===

type CreateRequest struct {
	LongURL    string `json:"long_url"`
	TTLSeconds *int64 `json:"ttl_seconds,omitempty"`
}

// === Responses ===

type CreateResponse struct {
	ShortCode string `json:"short_code"`
	ShortURL  string `json:"short_url"`
	LongURL   string `json:"long_url"`
	ExpiresAt string `json:"expires_at"`
}

type StatsResponse struct {
	ShortCode      string  `json:"short_code"`
	LongURL        string  `json:"long_url"`
	CreatedAt      string  `json:"created_at"`
	ExpiresAt      string  `json:"expires_at"`
	ClickCount     int64   `json:"click_count"`
	LastAccessedAt *string `json:"last_accessed_at"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
