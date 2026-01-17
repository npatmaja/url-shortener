package domain

import "errors"

var (
	// ErrNotFound indicates the requested record does not exist.
	ErrNotFound = errors.New("record not found")

	// ErrCodeExists indicates the short code is already taken.
	ErrCodeExists = errors.New("short code already exists")

	// ErrExpired indicates the record has expired.
	ErrExpired = errors.New("record has expired")
)
