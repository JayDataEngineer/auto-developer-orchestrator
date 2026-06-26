package mcpserver

import (
	"crypto/rand"
	"encoding/hex"
)

// newSessionID generates a random 16-byte hex session ID. Random per session
// (not per request) — used for the Mcp-Session-Id header.
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
