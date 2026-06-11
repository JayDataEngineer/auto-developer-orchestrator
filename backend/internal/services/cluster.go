package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
)

// HubBase returns the MCP/Service hub base URL.
// Configurable via MCP_HUB_ENDPOINT env var.
func HubBase() string {
	hub := os.Getenv("MCP_HUB_ENDPOINT")
	if hub == "" {
		hub = "http://100.86.69.57:30080"
	}
	return hub
}

// ClusterClient provides access to all Ray cluster services.
type ClusterClient struct {
	hubBase    string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewClusterClient creates a client for all Ray cluster services.
func NewClusterClient(logger *zap.Logger) *ClusterClient {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ClusterClient{
		hubBase: HubBase(),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger: logger,
	}
}

// healthClient returns an HTTP client with a short timeout for health checks.
func (c *ClusterClient) healthClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// LLMEndpoint returns the cluster LLM endpoint URL (OpenAI-compatible).
func (c *ClusterClient) LLMEndpoint() string {
	return c.hubBase + "/llm"
}

// LLMModel returns the model name on the cluster LLM.
func (c *ClusterClient) LLMModel() string {
	return "qwen3.6-27b-q6_k"
}

// --- TTS ---

// TTSRequest is the request body for TTS synthesis.
type TTSRequest struct {
	Text string `json:"text"`
}

// TTSResponse is the response from a TTS service.
type TTSResponse struct {
	Status string `json:"status"`
	Output struct {
		Type    string `json:"type"`
		Content string `json:"content"` // base64-encoded audio
	} `json:"output"`
}

// TTSHealth is the health status of a TTS service.
type TTSHealth struct {
	Status string `json:"status"`
	Model  string `json:"model"`
	Loaded bool   `json:"loaded"`
}

// TTSService represents an available TTS backend.
type TTSService struct {
	Name   string `json:"name"`
	Route  string `json:"route"`
	Loaded bool   `json:"loaded"`
}

// AvailableTTSServices returns all TTS backends on the cluster.
func AvailableTTSServices() []TTSService {
	return []TTSService{
		{Name: "espeak", Route: "/tts/espeak/"},
		{Name: "kokoro", Route: "/tts/kokoro/"},
		{Name: "vibevoice-cpp", Route: "/tts/vibevoice-cpp/"},
		{Name: "faster-qwen3-tts", Route: "/tts/faster-qwen3-tts/"},
		{Name: "index-tts", Route: "/tts/index-tts/"},
	}
}

// TTSHealth checks health of a specific TTS service (short timeout).
func (c *ClusterClient) TTSHealth(route string) (*TTSHealth, error) {
	url := c.hubBase + route
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	client := c.healthClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts health %s: %w", route, err)
	}
	defer resp.Body.Close()
	var h TTSHealth
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, fmt.Errorf("decode tts health: %w", err)
	}
	h.Status = resp.Status
	return &h, nil
}

// Synthesize sends text to a TTS service and returns the audio content.
// Uses a 30-second timeout per synthesis request.
func (c *ClusterClient) Synthesize(route, text string) ([]byte, string, error) {
	url := c.hubBase + route
	body := TTSRequest{Text: text}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("marshal tts: %w", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("tts request %s: %w", route, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read tts response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("tts %s returned %d: %s", route, resp.StatusCode, string(respBody))
	}
	var ttsResp TTSResponse
	if err := json.Unmarshal(respBody, &ttsResp); err != nil {
		return nil, "", fmt.Errorf("decode tts response: %w", err)
	}
	if ttsResp.Status != "success" {
		return nil, "", fmt.Errorf("tts synthesis failed: status=%s", ttsResp.Status)
	}
	return []byte(ttsResp.Output.Content), ttsResp.Output.Type, nil
}

// --- ASR ---

// ASRRequest is the request body for ASR transcription.
type ASRRequest struct {
	AudioB64 string `json:"audio_b64"`
}

// ASRResponse is the response from the ASR service.
type ASRResponse struct {
	Status string          `json:"status"`
	Output json.RawMessage `json:"output"`
	Error  string          `json:"error,omitempty"`
}

// ASRHealth is the health status of the ASR service.
type ASRHealth struct {
	Status string `json:"status"`
	Model  string `json:"model"`
	Loaded bool   `json:"loaded"`
}

// Transcribe sends base64-encoded audio to Whisper ASR.
func (c *ClusterClient) Transcribe(audioB64 []byte) (*ASRResponse, error) {
	url := c.hubBase + "/asr/whisper/"
	body := ASRRequest{AudioB64: string(audioB64)}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal asr: %w", err)
	}
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("asr request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read asr response: %w", err)
	}
	var asrResp ASRResponse
	if err := json.Unmarshal(respBody, &asrResp); err != nil {
		return nil, fmt.Errorf("decode asr response: %w", err)
	}
	return &asrResp, nil
}

// ASRHealth checks the ASR service health (short timeout).
func (c *ClusterClient) ASRHealth() (*ASRHealth, error) {
	url := c.hubBase + "/asr/whisper/"
	client := c.healthClient()
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("asr health: %w", err)
	}
	defer resp.Body.Close()
	var h ASRHealth
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, fmt.Errorf("decode asr health: %w", err)
	}
	return &h, nil
}

// --- Forge ---

// ForgeGenerateRequest is the request body for Forge generation.
type ForgeGenerateRequest struct {
	Mode  string `json:"mode"`  // "image", "3d", "music", "video"
	Prompt string `json:"prompt"`
	// Optional parameters forwarded to Forge
	Params json.RawMessage `json:"params,omitempty"`
}

// ForgeGenerateResponse is the response from Forge generation.
type ForgeGenerateResponse struct {
	Status string          `json:"status"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// ForgeGenerate sends a generation request to the Forge service.
// Routes to the appropriate sub-generator based on mode.
func (c *ClusterClient) ForgeGenerate(mode, prompt string, params json.RawMessage) (*ForgeGenerateResponse, error) {
	url := c.hubBase + "/forge/generate"
	body := ForgeGenerateRequest{
		Mode:   mode,
		Prompt: prompt,
		Params: params,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal forge request: %w", err)
	}
	client := &http.Client{Timeout: 120 * time.Second} // generation can be slow
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("forge request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read forge response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("forge returned %d: %s", resp.StatusCode, string(respBody))
	}
	var forgeResp ForgeGenerateResponse
	if err := json.Unmarshal(respBody, &forgeResp); err != nil {
		return nil, fmt.Errorf("decode forge response: %w", err)
	}
	return &forgeResp, nil
}

// ForgeHealth checks the Forge master router (short timeout).
func (c *ClusterClient) ForgeHealth() error {
	client := c.healthClient()
	resp, err := client.Get(c.hubBase + "/forge/")
	if err != nil {
		return fmt.Errorf("forge health: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadGateway {
		return nil // BadGateway means it's routing but upstream is down
	}
	return fmt.Errorf("forge returned %d", resp.StatusCode)
}

// --- LLM ---

// LLMHealth checks the cluster LLM health (short timeout).
func (c *ClusterClient) LLMHealth() (*LLMHealth, error) {
	url := c.hubBase + "/llm/health"
	client := c.healthClient()
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("llm health: %w", err)
	}
	defer resp.Body.Close()
	var h LLMHealth
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, fmt.Errorf("decode llm health: %w", err)
	}
	return &h, nil
}

// LLMHealth is the health status of the cluster LLM.
type LLMHealth struct {
	Status string `json:"status"`
	Model  string `json:"model"`
	Loaded bool   `json:"loaded"`
}

// --- S3 Storage (Garage) ---

// S3Config returns S3 connection parameters from env vars.
func S3Config() (endpoint, accessKey, secretKey string) {
	endpoint = os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://100.86.69.57:30390"
	}
	return endpoint, os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY")
}

// NewS3Client creates an S3-compatible client for Garage storage.
// Returns nil if access keys are not configured.
func NewS3Client() (*minio.Client, error) {
	endpoint, accessKey, secretKey := S3Config()
	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY not set")
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false, // HTTP, not HTTPS
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	return client, nil
}

// S3BucketList returns the cluster S3 endpoint and a health check.
func (c *ClusterClient) S3Health() error {
	client, err := NewS3Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.ListBuckets(ctx)
	return err
}

// --- Health ---

// ServiceStatus summarizes the status of a cluster service.
type ServiceStatus struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Loaded  bool   `json:"loaded,omitempty"`
	Error   string `json:"error,omitempty"`
}

// AllHealth returns health status of all cluster services (concurrent, 10s deadline).
func (c *ClusterClient) AllHealth() []ServiceStatus {
	type result struct {
		name string
		s    ServiceStatus
	}
	ch := make(chan result, 10)

	check := func(name, route string, fn func(string) (*TTSHealth, error)) {
		h, err := fn(route)
		if err != nil {
			ch <- result{name, ServiceStatus{Name: name, Healthy: false, Error: err.Error()}}
		} else {
			ch <- result{name, ServiceStatus{Name: name, Healthy: true, Loaded: h.Loaded}}
		}
	}

	// LLM
	go func() {
		h, err := c.LLMHealth()
		if err != nil {
			ch <- result{"llm", ServiceStatus{Name: "llm", Healthy: false, Error: err.Error()}}
		} else {
			ch <- result{"llm", ServiceStatus{Name: "llm", Healthy: true, Loaded: h.Loaded}}
		}
	}()

	// ASR
	go func() {
		h, err := c.ASRHealth()
		if err != nil {
			ch <- result{"asr-whisper", ServiceStatus{Name: "asr-whisper", Healthy: false, Error: err.Error()}}
		} else {
			ch <- result{"asr-whisper", ServiceStatus{Name: "asr-whisper", Healthy: true, Loaded: h.Loaded}}
		}
	}()

	// TTS services
	for _, svc := range AvailableTTSServices() {
		svc := svc // capture
		go check("tts-"+svc.Name, svc.Route, c.TTSHealth)
	}

	// Forge
	go func() {
		if err := c.ForgeHealth(); err != nil {
			ch <- result{"forge", ServiceStatus{Name: "forge", Healthy: false, Error: err.Error()}}
		} else {
			ch <- result{"forge", ServiceStatus{Name: "forge", Healthy: true}}
		}
	}()

	// S3 Storage (Garage)
	go func() {
		if err := c.S3Health(); err != nil {
			ch <- result{"s3-garage", ServiceStatus{Name: "s3-garage", Healthy: false, Error: err.Error()}}
		} else {
			ch <- result{"s3-garage", ServiceStatus{Name: "s3-garage", Healthy: true}}
		}
	}()

	total := 1 + 1 + len(AvailableTTSServices()) + 1 + 1 // LLM + ASR + TTS + Forge + S3
	results := make([]ServiceStatus, 0, total)
	timeout := time.After(10 * time.Second)

	for i := 0; i < total; i++ {
		select {
		case r := <-ch:
			results = append(results, r.s)
		case <-timeout:
			results = append(results, ServiceStatus{Name: "timeout", Healthy: false, Error: "health check timed out"})
			// Drain remaining
			for i < total-1 {
				<-ch
				i++
			}
		}
	}

	return results
}
