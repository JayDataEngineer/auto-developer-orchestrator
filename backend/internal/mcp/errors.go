package mcp

import (
	"context"
	"errors"
	"net"
	"strings"
)

// IsTransportError classifies an error as transport-level (network failure,
// context deadline, HTTP 5xx) vs tool-level (valid JSON-RPC error envelope).
// Transport errors trigger endpoint fallback; tool errors do NOT — the server
// is up, the tool just failed.
//
// This is the load-bearing correctness split: a healthy server that returns
// a tool error (e.g. "image not found", "rate limited by upstream") must NOT
// cause the client to switch to the fallback. Doing so would mask real bugs
// in tool behavior and could cascade to a fallback that doesn't even have
// the tool.
//
// Heuristics, in order:
//  1. net.Error (covers dial errors, connection refused, broken pipe, EOF
//     during read, TLS handshake failures)
//  2. context.DeadlineExceeded / context.Canceled
//  3. Error message contains network-failure signatures ("connection refused",
//     "no such host", "dial tcp", "EOF", "connection reset")
//  4. HTTP 5xx errors (doRequest wraps these as "HTTP 5xx from <url>: ...")
//
// Anything else is treated as a tool-level error and does NOT trigger
// fallback. Errors are wrapped via %w throughout, so errors.As unwraps
// faithfully.
func IsTransportError(err error) bool {
	if err == nil {
		return false
	}

	// 1. net.Error covers most dial / read / write failures.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// 2. Context expiry. net.Error.Timeout() doesn't always catch
	// context.DeadlineExceeded because the wrapper may not be a netErr.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}

	// 3. Message-signature scan. Catches cases where the underlying error is
	// a *url.Error, *net.OpError, or a wrapped error from the HTTP stdlib
	// whose type we don't want to import here.
	msg := err.Error()
	for _, sig := range transportErrorSignatures {
		if strings.Contains(msg, sig) {
			return true
		}
	}

	// 4. HTTP 5xx from doRequest. doRequest wraps these as
	// "HTTP 5xx from <url>: <body>". We match on the prefix.
	if hasHTTP5xxPrefix(msg) {
		return true
	}

	return false
}

// transportErrorSignatures are substring signatures of network-level errors
// not caught by the net.Error type assertion. Order doesn't matter; the test
// suite covers each one.
var transportErrorSignatures = []string{
	"connection refused",
	"no such host",
	"connection reset",
	"i/o timeout",
	"proxyconnect",
	"tls: handshake",
	" dial tcp ",   // "dial tcp 127.0.0.1:9999: connect: connection refused"
	"dial unix",
	"EOF",
}

// hasHTTP5xxPrefix reports whether the error message starts with "HTTP 5".
// doRequest formats HTTP errors as "HTTP %d from %s: %s" — a 5xx status
// (server error) is treated as a transport failure suitable for fallback;
// 4xx (client error, e.g. bad request) is not.
func hasHTTP5xxPrefix(msg string) bool {
	// "HTTP 5" must appear at the start of a message OR just after a prefix
	// from outer wrapping (e.g. "MCP tools/list failed: HTTP 500 from ...").
	// We check for "HTTP 5" anywhere — false positives on user-supplied text
	// are unlikely because the doRequest format is very specific.
	if !strings.Contains(msg, "HTTP 5") {
		return false
	}
	// Sanity-check the format: "HTTP 5" followed by two digits and a space.
	idx := strings.Index(msg, "HTTP 5")
	if idx+7 > len(msg) {
		return false
	}
	rest := msg[idx+6:] // "5xx from ..."
	if len(rest) < 2 {
		return false
	}
	// Allow e.g. "500", "503", "599". The first char is already '5' (in the
	// "5" of "HTTP 5"), so we just check the next two are digits.
	for i := 0; i < 2; i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}
