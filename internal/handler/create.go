package handler

import (
	"encoding/json"
	"net/http"
	"time"
)

const defaultTTL = 24 * time.Hour

// Create handles POST /shorten requests.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	// Validate URL
	if err := validateURL(req.LongURL); err != nil {
		h.writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	// Determine TTL
	ttl := defaultTTL
	if req.TTLSeconds != nil {
		ttl = time.Duration(*req.TTLSeconds) * time.Second
		if err := validateTTL(ttl); err != nil {
			h.writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
	}

	// Call service
	record, err := h.service.Create(r.Context(), req.LongURL, ttl)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to create short URL")
		return
	}

	// Build response
	resp := CreateResponse{
		ShortCode: record.ShortCode,
		ShortURL:  h.baseURL + "/s/" + record.ShortCode,
		LongURL:   record.LongURL,
		ExpiresAt: record.ExpiresAt.Format(time.RFC3339),
	}

	h.writeJSON(w, http.StatusCreated, resp)
}
