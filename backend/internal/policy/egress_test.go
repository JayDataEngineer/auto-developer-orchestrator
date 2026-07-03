package policy

import "testing"

// TestEgressRules_HostnameCommentForDNS covers the Gap 5 contract:
// DNS-resolved hosts get a `# host: <name>` comment before their IP lines
// so refresh-egress-dns.sh can re-resolve later. Literal IP entries get
// no comment (nothing to re-resolve). The apply-egress-policy.sh boot
// script skips `#` lines, so comments are no-ops at boot time.
func TestEgressRules_HostnameCommentForDNS(t *testing.T) {
	p := &Policy{Egress: Egress{Allow: []Rule{
		// Literal IP — no comment expected.
		{Host: "1.2.3.4", Port: 443},
		// DNS host — comment expected above the (resolved) IP line(s).
		{Host: "localhost", Port: 80},
	}}}
	out, err := EgressRules(p)
	if err != nil {
		t.Skipf("DNS resolution failed (likely offline): %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty rules")
	}

	// Literal IP line should be present without a preceding `# host:` comment.
	// We check the literal line exists and that the line before it isn't a
	// host comment for this entry.
	lines := splitLines(out)
	literalIdx := indexOf(lines, "1.2.3.4 443")
	if literalIdx < 0 {
		t.Fatalf("literal IP line missing in %q", out)
	}
	if literalIdx > 0 && lines[literalIdx-1] == "# host: 1.2.3.4" {
		t.Errorf("literal IP should NOT have a host comment, got %q", lines[literalIdx-1])
	}

	// DNS host should have the comment immediately before the resolved IP.
	hostIdx := indexOf(lines, "# host: localhost")
	if hostIdx < 0 {
		t.Fatalf("missing '# host: localhost' comment in %q", out)
	}
	// Line after the comment should be "<ip> 80" — we don't know the exact
	// IP (DNS result varies), but it must end with " 80" and not start with #.
	if hostIdx+1 >= len(lines) {
		t.Fatalf("host comment at end of output, missing IP line")
	}
	ipLine := lines[hostIdx+1]
	if !endsWith(ipLine, " 80") || startsWith(ipLine, "#") {
		t.Errorf("expected '<ip> 80' after comment, got %q", ipLine)
	}
}

// TestEgressRules_LiteralIPsHaveNoComments is a regression test for the
// literal-IP short-circuit. Each of these must NOT emit a host comment.
func TestEgressRules_LiteralIPsHaveNoComments(t *testing.T) {
	p := &Policy{Egress: Egress{Allow: []Rule{
		{Host: "1.2.3.4", Port: 443},
		{Host: "::1", Port: 443},
		{Host: "10.0.0.1", Ports: []int{80, 443}},
	}}}
	out, err := EgressRules(p)
	if err != nil {
		t.Fatalf("EgressRules: %v", err)
	}
	for _, line := range splitLines(out) {
		if startsWith(line, "# host:") {
			t.Errorf("literal-IP-only policy should have no host comments, got %q", line)
		}
	}
}

// splitLines helper — splits on \n, drops trailing empty string from the
// final newline. Keeps empty lines in the middle (shouldn't happen here).
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	// Trim exactly one trailing newline (EgressRules always ends with \n).
	if s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	var n int
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	out := make([]string, 0, n+1)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func indexOf(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
