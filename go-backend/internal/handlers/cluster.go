package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/services"
	"github.com/minio/minio-go/v7"
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

// getS3Client is a helper to create and validate an S3 client, returning a structured error.
func (h *ClusterHandler) getS3Client() (*minio.Client, error) {
	client, err := services.NewS3Client()
	if err != nil {
		return nil, fmt.Errorf("S3 not configured: %w", err)
	}
	return client, nil
}

// StorageBuckets handles GET /api/cluster/storage/buckets — list all buckets.
func (h *ClusterHandler) StorageBuckets(w http.ResponseWriter, r *http.Request) {
	s3client, err := h.getS3Client()
	if err != nil {
		JSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
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

// StorageListObjects handles GET /api/cluster/storage/objects?bucket=xxx — list objects in a bucket.
func (h *ClusterHandler) StorageListObjects(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		JSONError(w, "missing 'bucket' query parameter", http.StatusBadRequest)
		return
	}

	s3client, err := h.getS3Client()
	if err != nil {
		JSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	opts := minio.ListObjectsOptions{
		Recursive: r.URL.Query().Get("recursive") == "true",
	}
	objectCh := s3client.ListObjects(ctx, bucket, opts)

	var objects []map[string]interface{}
	for obj := range objectCh {
		if obj.Err != nil {
			h.logger.Warn("S3 list object error", zap.String("bucket", bucket), zap.Error(obj.Err))
			continue
		}
		objects = append(objects, map[string]interface{}{
			"key":           obj.Key,
			"size":          obj.Size,
			"last_modified": obj.LastModified,
			"etag":          obj.ETag,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bucket":  bucket,
		"objects": objects,
	})
}

// StorageUpload handles POST /api/cluster/storage/upload — upload an object.
// Accepts JSON with fields: bucket, object_name, content (base64), content_type (optional).
func (h *ClusterHandler) StorageUpload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bucket      string `json:"bucket"`
		ObjectName  string `json:"object_name"`
		Content     string `json:"content"` // base64-encoded
		ContentType string `json:"content_type,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Bucket == "" || req.ObjectName == "" || req.Content == "" {
		JSONError(w, "missing required fields: bucket, object_name, content", http.StatusBadRequest)
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		JSONError(w, "invalid base64 content", http.StatusBadRequest)
		return
	}

	s3client, err := h.getS3Client()
	if err != nil {
		JSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	opts := minio.PutObjectOptions{}
	if req.ContentType != "" {
		opts.ContentType = req.ContentType
	}
	info, err := s3client.PutObject(ctx, req.Bucket, req.ObjectName, bytes.NewReader(data), int64(len(data)), opts)
	if err != nil {
		h.logger.Warn("S3 upload failed", zap.String("bucket", req.Bucket), zap.String("object", req.ObjectName), zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"bucket":      info.Bucket,
		"object_name": info.Key,
		"size":        info.Size,
		"etag":        info.ETag,
	})
}

// StorageDownload handles GET /api/cluster/storage/download?bucket=xxx&object=xxx — download an object.
func (h *ClusterHandler) StorageDownload(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	objectName := r.URL.Query().Get("object")
	if bucket == "" || objectName == "" {
		JSONError(w, "missing 'bucket' and/or 'object' query parameters", http.StatusBadRequest)
		return
	}

	s3client, err := h.getS3Client()
	if err != nil {
		JSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	obj, err := s3client.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		h.logger.Warn("S3 download failed", zap.String("bucket", bucket), zap.String("object", objectName), zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	defer obj.Close()

	// Try to stat the object for content type
	if stat, err := obj.Stat(); err == nil {
		if stat.ContentType != "" {
			w.Header().Set("Content-Type", stat.ContentType)
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size))
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, objectName))
	io.Copy(w, obj)
}

// StorageDelete handles DELETE /api/cluster/storage/delete — delete an object from a bucket.
func (h *ClusterHandler) StorageDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bucket     string `json:"bucket"`
		ObjectName string `json:"object_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Bucket == "" || req.ObjectName == "" {
		JSONError(w, "missing required fields: bucket, object_name", http.StatusBadRequest)
		return
	}

	s3client, err := h.getS3Client()
	if err != nil {
		JSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s3client.RemoveObject(ctx, req.Bucket, req.ObjectName, minio.RemoveObjectOptions{}); err != nil {
		h.logger.Warn("S3 delete failed", zap.String("bucket", req.Bucket), zap.String("object", req.ObjectName), zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"bucket":      req.Bucket,
		"object_name": req.ObjectName,
	})
}
