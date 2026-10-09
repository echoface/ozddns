// Package ids generates the short, prefixed identifiers OzDDNS uses for
// providers, IP sources and jobs. It is deliberately tiny and shared so the
// application layer can assign an id before persisting and immediately know it.
package ids

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns an id like "p-1a2b3c4d5e".
func New(prefix string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}
