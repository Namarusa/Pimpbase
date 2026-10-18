// Package ids generates short random identifiers and secrets.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"strings"
)

var enc = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// New returns a random lowercase id of 16 characters (80 bits), with a
// prefix such as "app" or "tbl".
func New(prefix string) string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + enc.EncodeToString(b)
}

// Secret returns a random token with 256 bits of entropy.
func Secret(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + strings.ToLower(enc.EncodeToString(b))
}

// Hash returns the hex SHA-256 of a token, for storing secrets at rest.
func Hash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
