package services

import "bytes"

// mustReader creates a bytes.Reader from a byte slice. Panics on nil.
func mustReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}
