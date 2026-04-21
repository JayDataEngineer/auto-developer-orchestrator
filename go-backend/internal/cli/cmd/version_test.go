package cli

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	stdout, _, err := runCommand(t, "http://unused:9999", "version")
	if err != nil {
		t.Fatalf("version failed: %v", err)
	}
	if !strings.Contains(stdout, "orch") {
		t.Errorf("expected 'orch' in version output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "v0.1.0") {
		t.Errorf("expected version number in output, got: %s", stdout)
	}
}
