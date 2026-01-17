package shortcode_test

import (
	"strings"
	"testing"

	"url-shortener/internal/shortcode"

	"github.com/stretchr/testify/assert"
)

func TestGenerator_ExcludesAmbiguousCharacters(t *testing.T) {
	gen := shortcode.NewGenerator()
	excluded := "0OIl1"

	// Generate many codes and verify none contain excluded chars
	for i := 0; i < 10000; i++ {
		code := gen.Generate()
		for _, c := range excluded {
			assert.False(t, strings.ContainsRune(code, c),
				"code %q should not contain excluded char %q", code, string(c))
		}
	}
}

func TestGenerator_ProducesCorrectLength(t *testing.T) {
	gen := shortcode.NewGenerator()

	for i := 0; i < 1000; i++ {
		code := gen.Generate()
		assert.Len(t, code, 8, "code should be 8 characters")
	}
}

func TestGenerator_ProducesOnlyAlphanumeric(t *testing.T) {
	gen := shortcode.NewGenerator()
	allowed := "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

	for i := 0; i < 1000; i++ {
		code := gen.Generate()
		for _, c := range code {
			assert.True(t, strings.ContainsRune(allowed, c),
				"code %q contains invalid char %q", code, string(c))
		}
	}
}

func TestGenerator_ProducesUniqueCodesStatistically(t *testing.T) {
	gen := shortcode.NewGenerator()
	seen := make(map[string]bool)
	count := 10000

	for i := 0; i < count; i++ {
		code := gen.Generate()
		seen[code] = true
	}

	// With 57^8 possible combinations, 10000 codes should all be unique
	// (collision probability is negligible)
	assert.Len(t, seen, count, "all generated codes should be unique")
}
