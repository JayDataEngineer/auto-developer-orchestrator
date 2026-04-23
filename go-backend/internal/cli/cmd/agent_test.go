package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAgentHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pux/history" {
			t.Errorf("expected /api/pux/history, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("project") != "test-repo" {
			t.Errorf("expected project=test-repo, got %s", r.URL.Query().Get("project"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"conversations": []map[string]interface{}{
				{
					"project":      "test-repo",
					"agentId":      "default",
					"lastMessage":  "Hello",
					"messageCount": 5,
				},
			},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "-p", "test-repo", "agent", "history")
	if err != nil {
		t.Fatalf("agent history failed: %v", err)
	}
	if !strings.Contains(stdout, "test-repo") {
		t.Errorf("expected test-repo in output, got: %s", stdout)
	}
}

func TestAgentHistoryNoProject(t *testing.T) {
	// Clear ORCH_PROJECT to ensure no project is set
	os.Unsetenv("ORCH_PROJECT")
	_, _, err := runCommand(t, "http://unused:9999", "agent", "history")
	if err == nil {
		t.Fatal("expected error when project is not set")
	}
}

func TestAgentPromptMissingArgs(t *testing.T) {
	_, _, err := runCommand(t, "http://unused:9999", "agent", "prompt")
	// Should error because no message arg provided (cobra.MinimumNArgs(1))
	if err == nil {
		t.Fatal("expected error when no message provided")
	}
}
