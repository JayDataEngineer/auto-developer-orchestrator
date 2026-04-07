package handlers

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"time"
)

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
