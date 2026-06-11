package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newID generates a short random hex ID with prefix.
func newID(prefix string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}
