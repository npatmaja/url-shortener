package shortcode

import (
	"crypto/rand"
	"math/big"
)

// Alphabet excludes ambiguous characters: 0, O, I, l, 1
const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
const codeLength = 8

// Generator generates random short codes.
type Generator struct {
	alphabet string
	length   int
}

// NewGenerator creates a new short code generator.
func NewGenerator() *Generator {
	return &Generator{
		alphabet: alphabet,
		length:   codeLength,
	}
}

// Generate creates a new random short code.
// The code is 8 characters long using crypto/rand for security.
func (g *Generator) Generate() string {
	b := make([]byte, g.length)
	alphabetLen := big.NewInt(int64(len(g.alphabet)))

	for i := range b {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			// Fallback should never happen with crypto/rand
			panic("crypto/rand failed: " + err.Error())
		}
		b[i] = g.alphabet[n.Int64()]
	}

	return string(b)
}
