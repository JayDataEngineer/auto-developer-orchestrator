package face

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Client is a CompreFace HTTP client.
type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewClient creates a new CompreFace client.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) uploadFile(ctx context.Context, path, field, filePath string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(field, filePath)
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}

	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}

	return io.ReadAll(resp.Body)
}

// RecognizeTool recognizes faces in an image.
type RecognizeTool struct {
	client *Client
}

// NewRecognizeTool creates a new face recognize tool.
func NewRecognizeTool(client *Client) *RecognizeTool {
	return &RecognizeTool{client: client}
}

func (t *RecognizeTool) Name() string        { return "face_recognize" }
func (t *RecognizeTool) Description() string { return "Recognize faces in an image" }

func (t *RecognizeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"image_path": {"type": "string", "description": "Path to image file"},
			"min_similarity": {"type": "number", "description": "Minimum similarity threshold (0-1)"}
		},
		"required": ["image_path"]
	}`)
}

func (t *RecognizeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	imagePath, _ := args["image_path"].(string)
	if imagePath == "" {
		return nil, fmt.Errorf("missing required parameter 'image_path'")
	}

	minSim := 0.5
	if ms, ok := args["min_similarity"].(float64); ok {
		minSim = ms
	}

	resp, err := t.client.uploadFile(ctx, "/api/v1/recognition/recognize", "file", imagePath)
	if err != nil {
		return nil, fmt.Errorf("compreface request: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	faces, _ := result["result"].([]interface{})
	var matches []map[string]any
	for _, f := range faces {
		fm, _ := f.(map[string]any)
		embedding, _ := fm["embedding"].(map[string]any)
		vector, _ := embedding["vector"].([]interface{})
		similarity, _ := fm["similarity"].(float64)

		if similarity >= minSim {
			subject, _ := fm["subject"].(string)
			matches = append(matches, map[string]any{
				"subject":   subject,
				"similarity": similarity,
				"embedding": vector,
			})
		}
	}

	return map[string]any{"faces": matches, "count": len(matches)}, nil
}

// BatchRecognizeTool recognizes faces across multiple images.
type BatchRecognizeTool struct {
	client *Client
}

// NewBatchRecognizeTool creates a new batch recognize tool.
func NewBatchRecognizeTool(client *Client) *BatchRecognizeTool {
	return &BatchRecognizeTool{client: client}
}

func (t *BatchRecognizeTool) Name() string        { return "face_batch_recognize" }
func (t *BatchRecognizeTool) Description() string { return "Recognize faces across multiple images" }

func (t *BatchRecognizeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"image_paths": {"type": "string", "description": "JSON array of image file paths"}
		},
		"required": ["image_paths"]
	}`)
}

func (t *BatchRecognizeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pathsJSON, _ := args["image_paths"].(string)
	if pathsJSON == "" {
		return nil, fmt.Errorf("missing required parameter 'image_paths'")
	}

	var paths []string
	if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
		return nil, fmt.Errorf("parse paths: %w", err)
	}

	results := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		resp, err := t.client.uploadFile(ctx, "/api/v1/recognition/recognize", "file", path)
		if err != nil {
			results = append(results, map[string]any{"path": path, "error": err.Error()})
			continue
		}

		var result map[string]any
		json.Unmarshal(resp, &result)
		results = append(results, map[string]any{"path": path, "faces": result})
	}

	return map[string]any{"results": results, "count": len(results)}, nil
}

// ClusterTool clusters face embeddings using HDBSCAN-like approach.
type ClusterTool struct{}

// NewClusterTool creates a new face cluster tool.
func NewClusterTool() *ClusterTool {
	return &ClusterTool{}
}

func (t *ClusterTool) Name() string        { return "face_cluster_identities" }
func (t *ClusterTool) Description() string { return "Cluster face embeddings to identify unique identities" }

func (t *ClusterTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"faces_json": {"type": "string", "description": "JSON array of face embeddings"},
			"min_cluster_size": {"type": "integer", "description": "Minimum cluster size"}
		},
		"required": ["faces_json"]
	}`)
}

func (t *ClusterTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	facesJSON, _ := args["faces_json"].(string)
	if facesJSON == "" {
		return nil, fmt.Errorf("missing required parameter 'faces_json'")
	}

	minClusterSize := 2
	if mcs, ok := args["min_cluster_size"].(float64); ok {
		minClusterSize = int(mcs)
	}

	var faces []map[string]any
	if err := json.Unmarshal([]byte(facesJSON), &faces); err != nil {
		return nil, fmt.Errorf("parse faces: %w", err)
	}

	if len(faces) < minClusterSize {
		return map[string]any{"clusters": faces, "count": len(faces)}, nil
	}

	clusters := make([][]map[string]any, 0)
	assigned := make(map[int]bool)

	for i := range faces {
		if assigned[i] {
			continue
		}

		var cluster []map[string]any
		cluster = append(cluster, faces[i])
		assigned[i] = true

		for j := i + 1; j < len(faces); j++ {
			if assigned[j] {
				continue
			}

			emb1, _ := faces[i]["embedding"].([]interface{})
			emb2, _ := faces[j]["embedding"].([]interface{})

			sim := cosineSimilarity(emb1, emb2)
			if sim > 0.8 {
				cluster = append(cluster, faces[j])
				assigned[j] = true
			}
		}

		if len(cluster) >= minClusterSize {
			clusters = append(clusters, cluster)
		}
	}

	return map[string]any{"clusters": clusters, "count": len(clusters)}, nil
}

func cosineSimilarity(a, b []interface{}) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		va, _ := a[i].(float64)
		vb, _ := b[i].(float64)
		dot += va * vb
		normA += va * va
		normB += vb * vb
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (normA * normB)
}

// AddSubjectTool adds a known face subject.
type AddSubjectTool struct {
	client *Client
}

// NewAddSubjectTool creates a new add subject tool.
func NewAddSubjectTool(client *Client) *AddSubjectTool {
	return &AddSubjectTool{client: client}
}

func (t *AddSubjectTool) Name() string        { return "face_add_subject" }
func (AddSubjectTool *AddSubjectTool) Description() string { return "Add a known face subject" }

func (t *AddSubjectTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Subject name"},
			"image_path": {"type": "string", "description": "Path to subject's face image"}
		},
		"required": ["name", "image_path"]
	}`)
}

func (t *AddSubjectTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	imagePath, _ := args["image_path"].(string)

	if name == "" || imagePath == "" {
		return nil, fmt.Errorf("missing required parameters")
	}

	_, err := t.client.uploadFile(ctx, "/api/v1/recognition/subjects/"+name, "file", imagePath)
	if err != nil {
		return nil, fmt.Errorf("add subject: %w", err)
	}

	return map[string]any{"added": true, "subject": name}, nil
}

// ListSubjectsTool lists known subjects.
type ListSubjectsTool struct {
	client *Client
}

// NewListSubjectsTool creates a new list subjects tool.
func NewListSubjectsTool(client *Client) *ListSubjectsTool {
	return &ListSubjectsTool{client: client}
}

func (t *ListSubjectsTool) Name() string        { return "face_list_subjects" }
func (t *ListSubjectsTool) Description() string { return "List known face subjects" }

func (t *ListSubjectsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ListSubjectsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	resp, err := t.client.do(ctx, "GET", "/api/v1/recognition/subjects", nil)
	if err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	subjects, _ := result["subjects"].([]interface{})
	var names []string
	for _, s := range subjects {
		if name, ok := s.(string); ok {
			names = append(names, name)
		}
	}

	return map[string]any{"subjects": names, "count": len(names)}, nil
}

func RegisterAll(tools []core.Tool, baseURL, apiKey string) []core.Tool {
	if baseURL == "" || apiKey == "" {
		return tools
	}
	client := NewClient(baseURL, apiKey)
	return append(tools,
		NewRecognizeTool(client),
		NewBatchRecognizeTool(client),
		NewClusterTool(),
		NewAddSubjectTool(client),
		NewListSubjectsTool(client),
	)
}