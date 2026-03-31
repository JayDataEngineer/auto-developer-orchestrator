package handlers

import (
	"encoding/json"
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"
)

// ErrorResponse represents a standardized JSON error
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// Recoverer is a middleware that recovers from panics and logs the error
func Recoverer(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					if rvr == http.ErrAbortHandler {
						panic(rvr)
					}

					logger.Error("Panic recovered",
						zap.Any("panic", rvr),
						zap.String("stack", string(debug.Stack())),
					)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(ErrorResponse{
						Success: false,
						Error:   "Internal Server Error",
						Message: "A critical error occurred and has been logged.",
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// JSONError sends a standardized JSON error response
func JSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{
		Success: false,
		Error:   http.StatusText(code),
		Message: message,
	})
}
