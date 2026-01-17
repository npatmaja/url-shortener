package handler

import (
	"errors"
	"net/http"

	"url-shortener/internal/domain"
)

// Redirect handles GET /s/{code} requests.
func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		h.writeError(w, http.StatusBadRequest, "validation_error", "short code is required")
		return
	}

	longURL, err := h.service.Resolve(r.Context(), code)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrExpired) {
			h.writeError(w, http.StatusNotFound, "not_found", "short code not found or expired")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve URL")
		return
	}

	http.Redirect(w, r, longURL, http.StatusFound)
}
