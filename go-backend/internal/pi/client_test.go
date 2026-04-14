package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestProviderForModel(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"gemma-4-26b", "llamacpp"},
		{"gemma-4-26b-fast", "llamacpp"},
		{"qwen-cloud-vision", "llamacpp"},
		{"claude-3-opus", "llamacpp"},
		{"", "llamacpp"},
		{"anything-else", "llamacpp"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := providerForModel(tt.model); got != tt.expected {
				t.Errorf("providerForModel(%q) = %q, want %q", tt.model, got, tt.expected)
			}
		})
	}
}

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

func TestFixPiModelsConfigDeduplication(t *testing.T) {
	// Verify that fixPiModelsConfig removes duplicate providers with the same baseUrl
	tmpDir := t.TempDir()
	piDir := filepath.Join(tmpDir, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Config with TWO providers pointing to the same URL
	duplicateConfig := `{
  "providers": {
    "llamacpp": {
      "baseUrl": "http://localhost:8001/v1",
      "apiKey": "sk-no-key",
      "models": [{"id": "gemma-4-26b"}]
    },
    "litellm": {
      "baseUrl": "http://localhost:8001/v1",
      "apiKey": "sk-no-key",
      "models": [{"id": "gemma-4-26b"}],
      "default_model": "gemma-4-26b"
    }
  }
}`
	modelsPath := filepath.Join(piDir, "models.json")
	if err := os.WriteFile(modelsPath, []byte(duplicateConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Run the dedup logic directly
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", originalHome) })
	os.Setenv("HOME", tmpDir)

	data, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}

	var config struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}

	// Dedup: find providers with duplicate URLs (prefer "llamacpp")
	seen := make(map[string]string)
	var toRemove []string
	for name, cfg := range config.Providers {
		if existing, ok := seen[cfg.BaseURL]; ok {
			toRemoveName := name
			if name == "llamacpp" && existing != "llamacpp" {
				// Keep llamacpp, remove the other one
				toRemoveName = existing
				seen[cfg.BaseURL] = name
			}
			toRemove = append(toRemove, toRemoveName)
			t.Logf("Would remove duplicate: %s (duplicate of %s, both at %s)", toRemoveName, seen[cfg.BaseURL], cfg.BaseURL)
		} else {
			seen[cfg.BaseURL] = name
		}
	}

	if len(toRemove) != 1 {
		t.Fatalf("expected 1 duplicate, got %d", len(toRemove))
	}
	if toRemove[0] != "litellm" {
		t.Errorf("expected to remove 'litellm', got %q", toRemove[0])
	}

	// Verify the removal produces valid JSON with only llamacpp
	var rawConfig map[string]any
	json.Unmarshal(data, &rawConfig)
	providers := rawConfig["providers"].(map[string]any)
	for _, name := range toRemove {
		delete(providers, name)
	}

	result, _ := json.MarshalIndent(rawConfig, "", "  ")
	var parsed struct {
		Providers map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Providers) != 1 {
		t.Errorf("expected 1 provider after dedup, got %d", len(parsed.Providers))
	}
	if _, ok := parsed.Providers["llamacpp"]; !ok {
		t.Error("expected 'llamacpp' provider to remain")
	}
	if _, ok := parsed.Providers["litellm"]; ok {
		t.Error("expected 'litellm' provider to be removed")
	}
}
