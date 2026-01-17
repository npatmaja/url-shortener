package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"url-shortener/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestErrors_AreDistinct(t *testing.T) {
	assert.False(t, errors.Is(domain.ErrNotFound, domain.ErrCodeExists))
	assert.False(t, errors.Is(domain.ErrNotFound, domain.ErrExpired))
	assert.False(t, errors.Is(domain.ErrCodeExists, domain.ErrExpired))
}

func TestErrors_CanBeWrapped(t *testing.T) {
	wrapped := fmt.Errorf("operation failed: %w", domain.ErrNotFound)
	assert.True(t, errors.Is(wrapped, domain.ErrNotFound))
}
