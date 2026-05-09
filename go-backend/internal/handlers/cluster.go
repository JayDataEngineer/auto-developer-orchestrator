package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/services"
	"go.uber.org/zap"
)

// ClusterHandler exposes Ray cluster services (LLM, TTS, ASR, Forge) via API.
type ClusterHandler struct {
	client *services.ClusterClient
	logger *zap.Logger
}

// NewClusterHandler creates a handler for cluster services.
func NewClusterHandler(logger *zap.Logger) *ClusterHandler {
	return &ClusterHandler{
		client: services.NewClusterClient(logger),
		logger: logger,
	}
}

// ClusterStatus handles GET /api/cluster/status — health of all cluster services.
func (h *ClusterHandler) ClusterStatus(w http.ResponseWriter, r *http.Request) {
	status := h.client.AllHealth()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hub":      services.HubBase(),
		"services": status,
	})
}

// TTSServices handles GET /api/cluster/tts — list available TTS services.
func (h *ClusterHandler) TTSServices(w http.ResponseWriter, r *http.Request) {
	svcs := services.AvailableTTSServices()
	results := make([]map[string]interface{}, 0, len(svcs))
	for _, svc := range svcs {
		health, err := h.client.TTSHealth(svc.Route)
		s := map[string]interface{}{
			"name":  svc.Name,
			"route": svc.Route,
		}
		if err != nil {
			s["healthy"] = false
			s["error"] = err.Error()
		} else {
			s["healthy"] = true
			s["loaded"] = health.Loaded
		}
		results = append(results, s)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": results,
	})
}

// TTSRequest is the request body for TTS synthesis via API.
type TTSRequest struct {
	Service string `json:"service"`
	Text    string `json:"text"`
}

// SynthesizeSpeech handles POST /api/cluster/tts/synthesize.
func (h *ClusterHandler) SynthesizeSpeech(w http.ResponseWriter, r *http.Request) {
	var req TTSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Text == "" {
		JSONError(w, "missing 'text' field", http.StatusBadRequest)
		return
	}

	// Default to espeak if no service specified
	route := "/tts/espeak/"
	if req.Service != "" {
		route = "/tts/" + req.Service + "/"
	}

	audio, audioType, err := h.client.Synthesize(route, req.Text)
	if err != nil {
		h.logger.Warn("TTS synthesis failed", zap.String("service", req.Service), zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"audio":      string(audio),
		"audio_type": audioType,
	})
}

// ASRStatus handles GET /api/cluster/asr — ASR service health.
func (h *ClusterHandler) ASRStatus(w http.ResponseWriter, r *http.Request) {
	health, err := h.client.ASRHealth()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"healthy": false,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"healthy": true,
		"loaded":  health.Loaded,
		"model":   health.Model,
	})
}

// TranscribeAudio handles POST /api/cluster/asr/transcribe.
func (h *ClusterHandler) TranscribeAudio(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AudioB64 string `json:"audio_b64"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.AudioB64 == "" {
		JSONError(w, "missing 'audio_b64' field", http.StatusBadRequest)
		return
	}

	result, err := h.client.Transcribe([]byte(req.AudioB64))
	if err != nil {
		h.logger.Warn("ASR transcription failed", zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	if result.Status != "success" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   result.Error,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"output":  string(result.Output),
	})
}

// ForgeStatus handles GET /api/cluster/forge — Forge router health.
func (h *ClusterHandler) ForgeStatus(w http.ResponseWriter, r *http.Request) {
	err := h.client.ForgeHealth()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"healthy": false,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"healthy": true,
	})
}

// StorageStatus handles GET /api/cluster/storage — S3 (Garage) status.
func (h *ClusterHandler) StorageStatus(w http.ResponseWriter, r *http.Request) {
	s3client, err := services.NewS3Client()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"healthy": false,
			"error":   err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	buckets, err := s3client.ListBuckets(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"healthy": false,
			"error":   err.Error(),
		})
		return
	}

	endpoint, _, _ := services.S3Config()
	names := make([]string, len(buckets))
	for i, b := range buckets {
		names[i] = b.Name
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"healthy":  true,
		"endpoint": endpoint,
		"buckets":  names,
	})
}

// StorageBuckets handles GET /api/cluster/storage/buckets — list all buckets.
func (h *ClusterHandler) StorageBuckets(w http.ResponseWriter, r *http.Request) {
	s3client, err := services.NewS3Client()
	if err != nil {
		JSONError(w, "S3 not configured: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	buckets, err := s3client.ListBuckets(ctx)
	if err != nil {
		JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	names := make([]string, len(buckets))
	for i, b := range buckets {
		names[i] = b.Name
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"buckets": names})
}
