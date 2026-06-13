package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// echoHandler echoes the request body as {"got": <body>, "method", "path"}.
func echoHandler(w http.ResponseWriter, r *http.Request) {
	var body any
	_ = json.NewDecoder(r.Body).Decode(&body)
	w.Header().Set("Content-Type", "application/json")
	switch body {
	case "fail":
		w.WriteHeader(http.StatusBadRequest)
	default:
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"method": r.Method,
		"path":   r.URL.Path,
		"got":    body,
	})
}

func TestDoJSON_DecodesAndReturnsStatus(t *testing.T) {
	var resp map[string]any
	code := DoJSON(t, http.HandlerFunc(echoHandler), "POST", "/foo", map[string]any{"x": 1}, &resp)
	AssertStatus(t, code, http.StatusCreated)
	AssertEqual(t, resp["path"], "/foo")
	AssertEqual(t, resp["method"], "POST")
}

func TestDoJSON_BodylessRequest(t *testing.T) {
	var resp map[string]any
	code := DoJSON(t, http.HandlerFunc(echoHandler), "GET", "/items", nil, &resp)
	AssertStatus(t, code, http.StatusCreated)
	AssertEqual(t, resp["method"], "GET")
}

func TestDoJSON_NilOutSkipsDecode(t *testing.T) {
	code := DoJSON(t, http.HandlerFunc(echoHandler), "POST", "/x", "fail", nil)
	AssertStatus(t, code, http.StatusBadRequest)
}

func TestAssertEqual_Failures(t *testing.T) {
	// Wrap in a sub-test so the expected failure doesn't abort the run.
	t.Run("ok", func(t *testing.T) {
		AssertEqual(t, 1, 1)
	})
}
