package face

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestRegisterAll_EmptyConfig(t *testing.T) {
	tools := []core.Tool{}
	result := RegisterAll(tools, "", "")
	if len(result) != 0 {
		t.Fatalf("expected 0 tools with empty config, got %d", len(result))
	}
}

func TestRegisterAll_ValidConfig(t *testing.T) {
	tools := []core.Tool{}
	result := RegisterAll(tools, "http://localhost:8080", "test-key")
	if len(result) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(result))
	}

	expected := []string{
		"face_recognize",
		"face_batch_recognize",
		"face_cluster_identities",
		"face_add_subject",
		"face_list_subjects",
	}
	for i, name := range expected {
		if result[i].Name() != name {
			t.Errorf("tool %d: expected %q, got %q", i, name, result[i].Name())
		}
	}
}

func TestRegisterAll_PreservesExisting(t *testing.T) {
	existing := []core.Tool{&stubTool{name: "bash"}}
	result := RegisterAll(existing, "http://localhost:8080", "key")
	if len(result) != 6 {
		t.Fatalf("expected 6 tools (1 existing + 5 face), got %d", len(result))
	}
	if result[0].Name() != "bash" {
		t.Errorf("existing tool should be preserved, got %q", result[0].Name())
	}
}

func TestClient_TrimTrailingSlash(t *testing.T) {
	c := NewClient("http://localhost:8080/", "key")
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("expected trailing slash trimmed, got %q", c.baseURL)
	}
}

func TestListSubjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "test-api-key" {
			t.Errorf("missing or wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/recognition/subjects" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"subjects": ["Alice", "Bob"]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	tool := NewListSubjectsTool(client)

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	count := m["count"].(int)
	if count != 2 {
		t.Fatalf("expected 2 subjects, got %d", count)
	}

	names := m["subjects"].([]string)
	if names[0] != "Alice" || names[1] != "Bob" {
		t.Fatalf("unexpected subjects: %v", names)
	}
}

func TestListSubjects_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key")
	tool := NewListSubjectsTool(client)

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestRecognizeTool_MissingImagePath(t *testing.T) {
	client := NewClient("http://localhost:8080", "key")
	tool := NewRecognizeTool(client)

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing image_path")
	}
}

func TestBatchRecognizeTool_MissingPaths(t *testing.T) {
	client := NewClient("http://localhost:8080", "key")
	tool := NewBatchRecognizeTool(client)

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing image_paths")
	}
}

func TestBatchRecognizeTool_InvalidJSON(t *testing.T) {
	client := NewClient("http://localhost:8080", "key")
	tool := NewBatchRecognizeTool(client)

	_, err := tool.Execute(context.Background(), map[string]any{"image_paths": "not-json"})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestClusterTool_MissingFacesJSON(t *testing.T) {
	tool := NewClusterTool()
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing faces_json")
	}
}

func TestClusterTool_InvalidJSON(t *testing.T) {
	tool := NewClusterTool()
	_, err := tool.Execute(context.Background(), map[string]any{"faces_json": "bad"})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestClusterTool_SimpleClustering(t *testing.T) {
	tool := NewClusterTool()

	// Two identical faces should cluster together
	faces := `[
		{"id": 1, "embedding": [1.0, 0.0, 0.0]},
		{"id": 2, "embedding": [1.0, 0.0, 0.0]},
		{"id": 3, "embedding": [0.0, 1.0, 0.0]},
		{"id": 4, "embedding": [0.0, 1.0, 0.0]}
	]`
	result, err := tool.Execute(context.Background(), map[string]any{
		"faces_json":       faces,
		"min_cluster_size": 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	count := m["count"].(int)
	if count < 1 {
		t.Fatalf("expected at least 1 cluster, got %d", count)
	}
}

func TestClusterTool_TooFewFaces(t *testing.T) {
	tool := NewClusterTool()

	faces := `[{"id": 1, "embedding": [1.0]}]`
	result, err := tool.Execute(context.Background(), map[string]any{
		"faces_json":       faces,
		"min_cluster_size": 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	count := m["count"].(int)
	if count != 1 {
		t.Fatalf("expected 1 face returned (below min_cluster_size), got %d", count)
	}
}

func TestAddSubjectTool_MissingParams(t *testing.T) {
	client := NewClient("http://localhost:8080", "key")
	tool := NewAddSubjectTool(client)

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing parameters")
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []interface{}
		b    []interface{}
		want float64
	}{
		{
			name: "identical vectors",
			a:    []interface{}{1.0, 0.0, 0.0},
			b:    []interface{}{1.0, 0.0, 0.0},
			want: 1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []interface{}{1.0, 0.0},
			b:    []interface{}{0.0, 1.0},
			want: 0.0,
		},
		{
			name: "opposite vectors",
			a:    []interface{}{1.0, 0.0},
			b:    []interface{}{-1.0, 0.0},
			want: -1.0,
		},
		{
			name: "different lengths",
			a:    []interface{}{1.0},
			b:    []interface{}{1.0, 2.0},
			want: 0.0,
		},
		{
			name: "zero vectors",
			a:    []interface{}{0.0, 0.0},
			b:    []interface{}{0.0, 0.0},
			want: 0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cosineSimilarity(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("cosineSimilarity() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestToolSchemas(t *testing.T) {
	client := NewClient("http://localhost:8080", "key")
	tools := []core.Tool{
		NewRecognizeTool(client),
		NewBatchRecognizeTool(client),
		NewClusterTool(),
		NewAddSubjectTool(client),
		NewListSubjectsTool(client),
	}

	for _, tool := range tools {
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatalf("tool %q has invalid schema: %v", tool.Name(), err)
		}
		if typ, ok := parsed["type"].(string); !ok || typ != "object" {
			t.Fatalf("tool %q schema missing type=object", tool.Name())
		}
	}
}

type stubTool struct {
	name string
}

func (s *stubTool) Name() string                                          { return s.name }
func (s *stubTool) Description() string                                    { return "stub" }
func (s *stubTool) Schema() json.RawMessage                                { return json.RawMessage(`{}`) }
func (s *stubTool) Execute(ctx context.Context, args map[string]any) (any, error) { return nil, nil }
