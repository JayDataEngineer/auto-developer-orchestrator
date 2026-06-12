package sensitive

import (
	"strings"
	"testing"
)

func TestScrubText_AWSAccessKey(t *testing.T) {
	input := `export AWS_ACCESS_KEY_ID="AKIAIOSFODNN7EXAMPLE"`
	result := ScrubText(input)
	if strings.Contains(result, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("AWS access key not redacted: %s", result)
	}
	if !strings.Contains(result, "[REDACTED_AWS_ACCESS_KEY]") {
		t.Errorf("expected redaction marker, got: %s", result)
	}
}

func TestScrubText_GitHubToken(t *testing.T) {
	input := `token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij`
	result := ScrubText(input)
	if strings.Contains(result, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij") {
		t.Errorf("GitHub token not redacted: %s", result)
	}
	if !strings.Contains(result, "[REDACTED_GITHUB_TOKEN]") {
		t.Errorf("expected redaction marker, got: %s", result)
	}
}

func TestScrubText_OpenAIKey(t *testing.T) {
	input := `api_key: sk-abcdefghijklmnopqrstuvwxyz1234567890`
	result := ScrubText(input)
	if strings.Contains(result, "sk-abcdefghijklmnopqrstuvwxyz1234567890") {
		t.Errorf("OpenAI key not redacted: %s", result)
	}
}

func TestScrubText_BearerToken(t *testing.T) {
	input := `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.abc123`
	result := ScrubText(input)
	if strings.Contains(result, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Errorf("Bearer token not redacted: %s", result)
	}
}

func TestScrubText_Password(t *testing.T) {
	input := `password: "supersecretpassword123"`
	result := ScrubText(input)
	if strings.Contains(result, "supersecretpassword123") {
		t.Errorf("Password not redacted: %s", result)
	}
}

func TestScrubText_ConnectionString(t *testing.T) {
	input := `postgres://admin:mysecretpassword123@localhost:5432/mydb`
	result := ScrubText(input)
	if strings.Contains(result, "mysecretpassword123") {
		t.Errorf("DB password not redacted: %s", result)
	}
}

func TestScrubText_SlackToken(t *testing.T) {
	input := `SLACK_BOT_TOKEN=REDACTED_SK_TOKEN`
	result := ScrubText(input)
	if strings.Contains(result, "xoxb-") {
		t.Errorf("Slack token not redacted: %s", result)
	}
}

func TestScrubText_TelegramToken(t *testing.T) {
	input := `BOT_TOKEN=123456789:AAH_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p`
	result := ScrubText(input)
	if strings.Contains(result, "AAH_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p") {
		t.Errorf("Telegram token not redacted: %s", result)
	}
}

func TestScrubText_GoogleAPIKey(t *testing.T) {
	input := `GOOGLE_API_KEY=AIzaSyA1234567890abcdefghijklmnopqrstuvwx`
	result := ScrubText(input)
	if strings.Contains(result, "AIzaSyA1234567890abcdefghijklmnopqrstuvwx") {
		t.Errorf("Google API key not redacted: %s", result)
	}
}

func TestScrubText_PrivateKey(t *testing.T) {
	input := `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC7VJTUt9Us8cKj
-----END PRIVATE KEY-----`
	result := ScrubText(input)
	if strings.Contains(result, "MIIEvgIBADA") {
		t.Errorf("Private key not redacted: %s", result)
	}
	if !strings.Contains(result, "[REDACTED_PRIVATE_KEY]") {
		t.Errorf("expected redaction marker, got: %s", result)
	}
}

func TestScrubText_NoSecrets(t *testing.T) {
	input := `Hello world, this is a normal message with no secrets.`
	result := ScrubText(input)
	if result != input {
		t.Errorf("normal text was modified: %s", result)
	}
}

func TestScrubText_PartialRedaction(t *testing.T) {
	input := `The key is ghp_abc123456789012345678901234567890123 and the URL is https://example.com`
	result := ScrubText(input)
	if strings.Contains(result, "ghp_") {
		t.Errorf("token not redacted: %s", result)
	}
	if !strings.Contains(result, "https://example.com") {
		t.Errorf("URL should not be redacted: %s", result)
	}
}

func TestScrubMap(t *testing.T) {
	input := map[string]any{
		"command": "export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"path":   "/home/user/project",
		"nested": map[string]any{
			"token": "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
		},
	}
	result := ScrubMap(input)

	cmd, _ := result["command"].(string)
	if strings.Contains(cmd, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("nested value not redacted: %s", cmd)
	}

	path, _ := result["path"].(string)
	if path != "/home/user/project" {
		t.Errorf("non-secret value was modified: %s", path)
	}

	nested, _ := result["nested"].(map[string]any)
	token, _ := nested["token"].(string)
	if strings.Contains(token, "ghp_") {
		t.Errorf("nested map value not redacted: %s", token)
	}
}

func TestShouldScrubEvent(t *testing.T) {
	tests := []struct {
		event string
		want  bool
	}{
		{"text_delta", true},
		{"thinking_delta", true},
		{"tool_execution_end", true},
		{"tool_update", true},
		{"error", true},
		{"subagent_end", true},
		{"agent_start", false},
		{"agent_end", false},
		{"tool_execution_start", false},
		{"source", false},
		{"step_start", false},
	}
	for _, tt := range tests {
		got := ShouldScrubEvent(tt.event)
		if got != tt.want {
			t.Errorf("ShouldScrubEvent(%q) = %v, want %v", tt.event, got, tt.want)
		}
	}
}

func TestScrubText_AnthropicKey(t *testing.T) {
	input := `ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890`
	result := ScrubText(input)
	if strings.Contains(result, "sk-ant-api03") {
		t.Errorf("Anthropic key not redacted: %s", result)
	}
}

func TestScrubText_DiscordToken(t *testing.T) {
	input := `DISCORD_TOKEN=REDACTED_DC_TOKEN`
	result := ScrubText(input)
	if strings.Contains(result, "MTIzNDU2Nzg5MDEyMzQ1Njc4") {
		t.Errorf("Discord token not redacted: %s", result)
	}
}
