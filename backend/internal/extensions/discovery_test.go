package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	exts, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 0 {
		t.Fatalf("expected 0 extensions, got %d", len(exts))
	}
}

func TestDiscover_NonexistentDir(t *testing.T) {
	exts, err := Discover("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 0 {
		t.Fatalf("expected nil for nonexistent dir, got %d", len(exts))
	}
}

func TestDiscover_DefaultManifest(t *testing.T) {
	dir := t.TempDir()
	extDir := filepath.Join(dir, "my-tool")
	os.MkdirAll(extDir, 0755)
	os.WriteFile(filepath.Join(extDir, "server.ts"), []byte("// test"), 0644)

	exts, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(exts))
	}
	if exts[0].Name != "my_tool" {
		t.Errorf("expected name 'my_tool', got %q", exts[0].Name)
	}
	if exts[0].Server.Command != "bun" {
		t.Errorf("expected command 'bun', got %q", exts[0].Server.Command)
	}
	if exts[0].Server.Timeout != 15 {
		t.Errorf("expected timeout 15, got %d", exts[0].Server.Timeout)
	}
}

func TestDiscover_WithManifest(t *testing.T) {
	dir := t.TempDir()
	extDir := filepath.Join(dir, "webhook-tool")
	os.MkdirAll(extDir, 0755)
	os.WriteFile(filepath.Join(extDir, "server.ts"), []byte("// test"), 0644)
	os.WriteFile(filepath.Join(extDir, "extension.yaml"), []byte(`
name: webhook_tool
version: 1.2.0
description: "Send webhooks"
server:
  command: node
  args: [run, server.ts]
  timeout: 30
`), 0644)

	exts, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(exts))
	}
	ext := exts[0]
	if ext.Name != "webhook_tool" {
		t.Errorf("expected name 'webhook_tool', got %q", ext.Name)
	}
	if ext.Version != "1.2.0" {
		t.Errorf("expected version '1.2.0', got %q", ext.Version)
	}
	if ext.Description != "Send webhooks" {
		t.Errorf("expected description, got %q", ext.Description)
	}
	if ext.Server.Command != "node" {
		t.Errorf("expected command 'node', got %q", ext.Server.Command)
	}
	if ext.Server.Timeout != 30 {
		t.Errorf("expected timeout 30, got %d", ext.Server.Timeout)
	}
}

func TestDiscover_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	hiddenDir := filepath.Join(dir, ".hidden")
	os.MkdirAll(hiddenDir, 0755)
	os.WriteFile(filepath.Join(hiddenDir, "server.ts"), []byte("// test"), 0644)

	exts, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 0 {
		t.Fatalf("expected 0 extensions (hidden dir skipped), got %d", len(exts))
	}
}

func TestDiscover_SkipsNoServerTS(t *testing.T) {
	dir := t.TempDir()
	extDir := filepath.Join(dir, "not-an-extension")
	os.MkdirAll(extDir, 0755)
	// No server.ts, no extension.yaml

	exts, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 0 {
		t.Fatalf("expected 0 extensions (no server.ts), got %d", len(exts))
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"my-tool", "my_tool"},
		{"My Tool", "my_tool"},
		{"webhook_tool", "webhook_tool"},
		{"tech-noir-studio", "tech_noir_studio"},
		{"tool@v2!", "toolv2"},
	}
	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
