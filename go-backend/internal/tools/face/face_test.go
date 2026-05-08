package face

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

func TestRegisterAll_EmptyConfig(t *testing.T) {
	result := RegisterAll(nil, "", "")
	if result != nil {
		t.Fatalf("expected nil with empty config, got %d tools", len(result))
	}
}

func TestRegisterAll_ValidConfig(t *testing.T) {
	tools := RegisterAll(nil, "http://localhost:8080", "test-key")
	expected := []string{
		"face_recognize",
		"face_batch_recognize",
		"face_cluster_identities",
		"face_add_subject",
		"face_list_subjects",
	}
	testutil.AssertToolNames(t, tools, expected)
	testutil.AssertValidSchemas(t, tools)
}

func TestRegisterAll_PreservesExisting(t *testing.T) {
	existing := []core.Tool{testutil.NewStubTool("bash")}
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

func TestListSubjects_Success(t *testing.T) {
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
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertIntField(t, m, "count", 2)
	subjects := m["subjects"].([]string)
	if len(subjects) != 2 || subjects[0] != "Alice" || subjects[1] != "Bob" {
		t.Errorf("unexpected subjects: %v", subjects)
	}
}

func TestListSubjects_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	tool := NewListSubjectsTool(NewClient(server.URL, "key"))
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestTools_MissingParams(t *testing.T) {
	tests := []struct {
		name      string
		tool      core.Tool
		paramName string
	}{
		{name: "face_recognize", tool: NewRecognizeTool(NewClient("http://localhost:8080", "key")), paramName: "image_path"},
		{name: "face_batch_recognize", tool: NewBatchRecognizeTool(NewClient("http://localhost:8080", "key")), paramName: "image_paths"},
		{name: "face_cluster_identities", tool: NewClusterTool(), paramName: "faces_json"},
		{name: "face_add_subject", tool: NewAddSubjectTool(NewClient("http://localhost:8080", "key")), paramName: "name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.AssertMissingParam(t, tt.tool, tt.paramName)
		})
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
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	count, ok := m["count"].(int)
	if !ok {
		if f, ok := m["count"].(float64); ok {
			count = int(f)
		}
	}
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
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertIntField(t, m, "count", 1)
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []interface{}
		b    []interface{}
		want float64
	}{
		{name: "identical vectors", a: []interface{}{1.0, 0.0, 0.0}, b: []interface{}{1.0, 0.0, 0.0}, want: 1.0},
		{name: "orthogonal vectors", a: []interface{}{1.0, 0.0}, b: []interface{}{0.0, 1.0}, want: 0.0},
		{name: "opposite vectors", a: []interface{}{1.0, 0.0}, b: []interface{}{-1.0, 0.0}, want: -1.0},
		{name: "different lengths", a: []interface{}{1.0}, b: []interface{}{1.0, 2.0}, want: 0.0},
		{name: "zero vectors", a: []interface{}{0.0, 0.0}, b: []interface{}{0.0, 0.0}, want: 0.0},
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
	testutil.AssertValidSchemas(t, tools)
}
