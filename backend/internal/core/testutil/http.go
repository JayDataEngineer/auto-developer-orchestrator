package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// NewJSONRequest builds an *http.Request with body marshalled as JSON.
// body may be nil for bodyless requests.
func NewJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("NewJSONRequest: marshal body: %v", err)
		}
		buf = bytes.NewBuffer(raw)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// DoJSON sends method+path+body (marshalled as JSON) through r and returns the
// HTTP status code. If out is non-nil, the response body is decoded into it.
//
// This collapses the common handler-test block:
//
//	jsonBody, _ := json.Marshal(body)
//	req := httptest.NewRequest("POST", "/path", bytes.NewBuffer(jsonBody))
//	w := httptest.NewRecorder()
//	r.ServeHTTP(w, req)
//	json.NewDecoder(w.Body).Decode(&resp)
//
// into:
//
//	code := testutil.DoJSON(t, r, "POST", "/path", body, &resp)
func DoJSON(t *testing.T, r http.Handler, method, path string, body any, out any) int {
	t.Helper()
	req := NewJSONRequest(t, method, path, body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if out != nil && w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("DoJSON: decode response body: %v\nbody: %s", err, w.Body.String())
		}
	}
	return w.Code
}

// AssertStatus fails the test if code != want.
func AssertStatus(t *testing.T, code, want int) {
	t.Helper()
	if code != want {
		t.Errorf("status = %d, want %d", code, want)
	}
}

// AssertEqual is a generic equality check for comparable types.
func AssertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// AssertDeepEqual uses reflect.DeepEqual for types that are not directly comparable.
func AssertDeepEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
