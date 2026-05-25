package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	return zap.NewNop()
}

func TestLoadModelConfigWithBothModels(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".pi", "agent")
	os.MkdirAll(settingsPath, 0755)

	content := `{
  "defaultProvider": "llamacpp",
  "defaultModel": "gemma-4-26b",
  "mainModel": {"provider": "anthropic", "modelId": "claude-3-opus"},
  "toolModel": {"provider": "llamacpp", "modelId": "gemma-4-26b"}
}`
	os.WriteFile(filepath.Join(settingsPath, "settings.json"), []byte(content), 0644)

	// Override HOME
	t.Setenv("HOME", dir)

	cfg, err := LoadModelConfig(testLogger())
	if err != nil {
		t.Fatal(err)
	}

	main := cfg.MainModel()
	if main.Provider != "anthropic" || main.ModelId != "claude-3-opus" {
		t.Errorf("main = %+v, want {anthropic claude-3-opus}", main)
	}

	tool := cfg.ToolModel()
	if tool.Provider != "llamacpp" || tool.ModelId != "gemma-4-26b" {
		t.Errorf("tool = %+v, want {llamacpp gemma-4-26b}", tool)
	}
}

func TestLoadModelConfigFallbackToDefaults(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".pi", "agent")
	os.MkdirAll(settingsPath, 0755)

	// Only default fields, no mainModel/toolModel
	content := `{
  "defaultProvider": "llamacpp",
  "defaultModel": "gemma-4-26b"
}`
	os.WriteFile(filepath.Join(settingsPath, "settings.json"), []byte(content), 0644)

	t.Setenv("HOME", dir)

	cfg, err := LoadModelConfig(testLogger())
	if err != nil {
		t.Fatal(err)
	}

	main := cfg.MainModel()
	if main.Provider != "llamacpp" || main.ModelId != "gemma-4-26b" {
		t.Errorf("main = %+v, want defaults from defaultProvider/defaultModel", main)
	}

	tool := cfg.ToolModel()
	if tool.Provider != "llamacpp" || tool.ModelId != "gemma-4-26b" {
		t.Errorf("tool should fall back to main model, got %+v", tool)
	}
}

func TestLoadModelConfigMissingFile(t *testing.T) {
	dir := t.TempDir()

	t.Setenv("HOME", dir)

	cfg, err := LoadModelConfig(testLogger())
	if err != nil {
		t.Fatal(err)
	}

	main := cfg.MainModel()
	if main != DefaultModelEntry {
		t.Errorf("main = %+v, want default %+v", main, DefaultModelEntry)
	}

	tool := cfg.ToolModel()
	if tool != DefaultModelEntry {
		t.Errorf("tool = %+v, want default %+v", tool, DefaultModelEntry)
	}
}

func TestSetMainModelPersists(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".pi", "agent")
	os.MkdirAll(settingsPath, 0755)

	content := `{
  "defaultProvider": "llamacpp",
  "defaultModel": "gemma-4-26b",
  "mainModel": {"provider": "llamacpp", "modelId": "gemma-4-26b"},
  "toolModel": {"provider": "llamacpp", "modelId": "gemma-4-26b"}
}`
	os.WriteFile(filepath.Join(settingsPath, "settings.json"), []byte(content), 0644)

	t.Setenv("HOME", dir)

	cfg, _ := LoadModelConfig(testLogger())

	if err := cfg.SetMainModel("anthropic", "claude-3-opus"); err != nil {
		t.Fatal(err)
	}

	// Verify in-memory
	main := cfg.MainModel()
	if main.Provider != "anthropic" || main.ModelId != "claude-3-opus" {
		t.Errorf("main after set = %+v", main)
	}

	// Verify on disk — reload from file
	cfg2, _ := LoadModelConfig(testLogger())
	main2 := cfg2.MainModel()
	if main2.Provider != "anthropic" || main2.ModelId != "claude-3-opus" {
		t.Errorf("main after reload = %+v, want {anthropic claude-3-opus}", main2)
	}

	// Tool should be unchanged
	tool := cfg.ToolModel()
	if tool.Provider != "llamacpp" || tool.ModelId != "gemma-4-26b" {
		t.Errorf("tool should be unchanged = %+v", tool)
	}
}

func TestSetToolModelPersists(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".pi", "agent")
	os.MkdirAll(settingsPath, 0755)

	content := `{"defaultProvider":"llamacpp","defaultModel":"gemma-4-26b"}`
	os.WriteFile(filepath.Join(settingsPath, "settings.json"), []byte(content), 0644)

	t.Setenv("HOME", dir)

	cfg, _ := LoadModelConfig(testLogger())

	if err := cfg.SetToolModel("litellm", "custom-model"); err != nil {
		t.Fatal(err)
	}

	tool := cfg.ToolModel()
	if tool.Provider != "litellm" || tool.ModelId != "custom-model" {
		t.Errorf("tool after set = %+v", tool)
	}

	// Verify persisted
	cfg2, _ := LoadModelConfig(testLogger())
	tool2 := cfg2.ToolModel()
	if tool2.Provider != "litellm" || tool2.ModelId != "custom-model" {
		t.Errorf("tool after reload = %+v", tool2)
	}
}


func TestProviderForModel(t *testing.T) {
	cfg := &ModelConfig{
		main:   ModelEntry{Provider: "anthropic", ModelId: "claude-3-opus"},
		tool:   ModelEntry{Provider: "llamacpp", ModelId: "gemma-4-26b"},
		logger: testLogger(),
	}

	tests := []struct {
		modelId  string
		expected string
	}{
		{"claude-3-opus", "anthropic"},
		{"gemma-4-26b", "llamacpp"},
		{"unknown-model", "llamacpp"}, // fallback
	}

	for _, tt := range tests {
		t.Run(tt.modelId, func(t *testing.T) {
			got := cfg.ProviderForModel(tt.modelId)
			if got != tt.expected {
				t.Errorf("ProviderForModel(%q) = %q, want %q", tt.modelId, got, tt.expected)
			}
		})
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	// Verify no panics, deadlocks, or data races under concurrent access.
	// Write errors are expected when multiple goroutines write to the same file.
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".pi", "agent")
	os.MkdirAll(settingsPath, 0755)

	content := `{"defaultProvider":"llamacpp","defaultModel":"gemma-4-26b"}`
	os.WriteFile(filepath.Join(settingsPath, "settings.json"), []byte(content), 0644)

	t.Setenv("HOME", dir)

	cfg, _ := LoadModelConfig(testLogger())

	var wg sync.WaitGroup

	// Concurrent writers — some will fail, that's expected with same-file contention
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cfg.SetMainModel("provider", "model-"+string(rune('0'+n)))
		}(i)
	}

	// Concurrent readers
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cfg.MainModel()
			_ = cfg.ToolModel()
			_ = cfg.ProviderForModel("test")
		}()
	}

	wg.Wait()
	// If we get here without panics or deadlocks, the test passes.
}

func TestPersistPreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".pi", "agent")
	os.MkdirAll(settingsPath, 0755)

	// Include packages field that ModelConfig should NOT destroy
	content := `{
  "defaultProvider": "llamacpp",
  "defaultModel": "gemma-4-26b",
  "packages": ["../extensions/todos.ts"]
}`
	os.WriteFile(filepath.Join(settingsPath, "settings.json"), []byte(content), 0644)

	t.Setenv("HOME", dir)

	cfg, _ := LoadModelConfig(testLogger())

	if err := cfg.SetToolModel("llamacpp", "new-model"); err != nil {
		t.Fatal(err)
	}

	// Read raw file and verify packages is preserved
	data, err := os.ReadFile(filepath.Join(settingsPath, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	pkgs, ok := raw["packages"]
	if !ok {
		t.Fatal("packages field was removed from settings.json")
	}

	pkgsArr, ok := pkgs.([]interface{})
	if !ok || len(pkgsArr) != 1 {
		t.Errorf("packages = %v, want array with 1 element", pkgs)
	}

	// Verify toolModel was written
	tm, ok := raw["toolModel"]
	if !ok {
		t.Fatal("toolModel field missing from settings.json")
	}
	tmMap := tm.(map[string]any)
	if tmMap["modelId"] != "new-model" {
		t.Errorf("toolModel.modelId = %v, want new-model", tmMap["modelId"])
	}
}

