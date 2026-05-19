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
	if !strings.Contains(stdout, "v") {
		t.Errorf("expected 'v' prefix in version output, got: %s", stdout)
	}
}
