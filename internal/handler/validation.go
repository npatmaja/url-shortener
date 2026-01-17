package handler

import (
	"errors"
	"net/url"
	"time"
)

const (
	maxURLLength = 2048
	minTTL       = 60 * time.Second     // 1 minute
	maxTTL       = 365 * 24 * time.Hour // 1 year
)

func validateURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("long_url is required")
	}

	if len(rawURL) > maxURLLength {
		return errors.New("long_url exceeds maximum length of 2048 characters")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.New("invalid URL format")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("URL scheme must be http or https")
	}

	if parsed.Host == "" {
		return errors.New("URL must have a host")
	}

	return nil
}

func validateTTL(ttl time.Duration) error {
	if ttl < minTTL {
		return errors.New("ttl_seconds must be at least 60")
	}
	if ttl > maxTTL {
		return errors.New("ttl_seconds must not exceed 31536000 (1 year)")
	}
	return nil
}
