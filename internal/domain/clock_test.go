package domain_test

import (
	"testing"
	"time"

	"url-shortener/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestRealClock_ReturnsCurrentTime(t *testing.T) {
	clock := domain.RealClock{}

	before := time.Now()
	now := clock.Now()
	after := time.Now()

	assert.True(t, !now.Before(before), "clock.Now() should not be before time.Now()")
	assert.True(t, !now.After(after), "clock.Now() should not be after time.Now()")
}

func TestMockClock_ReturnsFixedTime(t *testing.T) {
	fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	clock := domain.NewMockClock(fixed)

	assert.Equal(t, fixed, clock.Now())
	assert.Equal(t, fixed, clock.Now()) // Same time on subsequent calls
}

func TestMockClock_Advance(t *testing.T) {
	fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	clock := domain.NewMockClock(fixed)

	clock.Advance(time.Hour)

	expected := time.Date(2024, 1, 15, 13, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, clock.Now())
}

func TestMockClock_Set(t *testing.T) {
	clock := domain.NewMockClock(time.Now())

	newTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	clock.Set(newTime)

	assert.Equal(t, newTime, clock.Now())
}
