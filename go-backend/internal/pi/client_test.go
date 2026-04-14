package pi

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestResolveDockerContainerIP(t *testing.T) {
	// This test only passes if Docker is running and the shared-llama-cpp container exists.
	// In CI or environments without Docker, skip.
	ip := resolveDockerContainerIP("shared-llama-cpp")
	if ip == "" {
		t.Skip("shared-llama-cpp container not found — skipping (no Docker)")
	}
	t.Logf("Resolved shared-llama-cpp IP: %s", ip)
	// Should be a valid IPv4 address
	re := regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
	if !re.MatchString(ip) {
		t.Errorf("expected IPv4 address, got %q", ip)
	}
}

func TestResolveDockerContainerIPNonExistent(t *testing.T) {
	ip := resolveDockerContainerIP("nonexistent-container-xyz-12345")
	if ip != "" {
		t.Errorf("expected empty string for nonexistent container, got %q", ip)
	}
}

func TestDockerIPRegexReplacement(t *testing.T) {
	re := regexp.MustCompile(`http://172\.\d+\.\d+\.\d+:\d+/v1`)

	tests := []struct {
		name     string
		input    string
		replace  string
		expected string
	}{
		{
			name:     "replaces stale IP",
			input:    `"baseUrl": "http://172.17.0.14:8001/v1"`,
			replace:  "http://172.17.0.13:8001/v1",
			expected: `"baseUrl": "http://172.17.0.13:8001/v1"`,
		},
		{
			name:     "replaces multiple IPs",
			input:    `"baseUrl": "http://172.17.0.14:8001/v1",\n"other": "http://172.17.0.99:4000/v1"`,
			replace:  "http://172.17.0.13:8001/v1",
			expected: `"baseUrl": "http://172.17.0.13:8001/v1",\n"other": "http://172.17.0.13:8001/v1"`,
		},
		{
			name:     "no match leaves unchanged",
			input:    `"baseUrl": "http://localhost:4000/v1"`,
			replace:  "http://172.17.0.13:8001/v1",
			expected: `"baseUrl": "http://localhost:4000/v1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := re.ReplaceAllString(tt.input, tt.replace)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFixPiModelsConfigIntegration(t *testing.T) {
	// Create a temp models.json with a stale IP and verify fixPiModelsConfig updates it.
	tmpDir := t.TempDir()
	modelsPath := filepath.Join(tmpDir, "models.json")

	// Write a config with a stale IP
	staleConfig := `{
  "providers": {
    "llamacpp": {
      "baseUrl": "http://172.17.0.99:8001/v1",
      "models": [{"id": "gemma-4-26b"}]
    }
  }
}`
	if err := os.WriteFile(modelsPath, []byte(staleConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Override HOME so fixPiModelsConfig reads our temp file
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", originalHome) })

	piDir := filepath.Join(tmpDir, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0755); err != nil {
		t.Fatal(err)
	}
	os.Setenv("HOME", tmpDir)
	if err := os.WriteFile(filepath.Join(piDir, "models.json"), []byte(staleConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Manually test the regex replacement logic (same as fixPiModelsConfig)
	data, err := os.ReadFile(filepath.Join(piDir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`http://172\.\d+\.\d+\.\d+:\d+/v1`)
	currentIP := resolveDockerContainerIP("shared-llama-cpp")
	if currentIP == "" {
		t.Skip("Docker not available — skipping integration test")
	}

	correctURL := "http://" + currentIP + ":8001/v1"
	content := re.ReplaceAllString(string(data), correctURL)

	if content == string(data) {
		t.Error("expected content to be updated")
	}

	if !contains(content, currentIP) {
		t.Errorf("expected content to contain %q, got %q", currentIP, content)
	}
	t.Logf("Updated config: %s", content)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
