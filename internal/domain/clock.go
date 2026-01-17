package domain

import "time"

// Clock provides time operations for the application.
// This abstraction allows deterministic testing without time.Sleep.
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock using the system time.
type RealClock struct{}

// Now returns the current system time.
func (RealClock) Now() time.Time {
	return time.Now()
}

// MockClock implements Clock with controllable time for testing.
type MockClock struct {
	current time.Time
}

// NewMockClock creates a MockClock set to the given time.
func NewMockClock(t time.Time) *MockClock {
	return &MockClock{current: t}
}

// Now returns the mock's current time.
func (c *MockClock) Now() time.Time {
	return c.current
}

// Advance moves the clock forward by the given duration.
func (c *MockClock) Advance(d time.Duration) {
	c.current = c.current.Add(d)
}

// Set sets the clock to a specific time.
func (c *MockClock) Set(t time.Time) {
	c.current = t
}
