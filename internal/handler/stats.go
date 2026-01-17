package handler

import (
	"errors"
	"net/http"
	"time"

	"url-shortener/internal/domain"
)

// Stats handles GET /stats/{code} requests.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		h.writeError(w, http.StatusBadRequest, "validation_error", "short code is required")
		return
	}

	record, err := h.service.GetStats(r.Context(), code)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrExpired) {
			h.writeError(w, http.StatusNotFound, "not_found", "short code not found or expired")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to get stats")
		return
	}

	resp := StatsResponse{
		ShortCode:  record.ShortCode,
		LongURL:    record.LongURL,
		CreatedAt:  record.CreatedAt.Format(time.RFC3339),
		ExpiresAt:  record.ExpiresAt.Format(time.RFC3339),
		ClickCount: record.ClickCount,
	}

	// Only set LastAccessedAt if it's not zero
	if !record.LastAccessedAt.IsZero() {
		formatted := record.LastAccessedAt.Format(time.RFC3339)
		resp.LastAccessedAt = &formatted
	}

	h.writeJSON(w, http.StatusOK, resp)
}
