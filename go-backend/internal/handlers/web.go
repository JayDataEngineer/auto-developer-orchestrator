package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/browser"
	"go.uber.org/zap"
)

// WebHandler handles HTTP requests for browser automation
type WebHandler struct {
	browserClient *browser.BrowserClient
	visionClient  *browser.VisionClient
	logger        *zap.Logger
}

// NewWebHandler creates a new web handler
func NewWebHandler(browserClient *browser.BrowserClient, visionClient *browser.VisionClient, logger *zap.Logger) *WebHandler {
	return &WebHandler{
		browserClient: browserClient,
		visionClient:  visionClient,
		logger:        logger,
	}
}

// RegisterRoutes registers web handler routes on a chi.Router
func (h *WebHandler) RegisterRoutes(r interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
	Delete(string, http.HandlerFunc)
}) {
	r.Post("/session", h.CreateSession)
	r.Delete("/session", h.CloseSession)
	r.Post("/navigate", h.Navigate)
	r.Post("/click", h.Click)
	r.Post("/type", h.Type)
	r.Post("/scroll", h.Scroll)
	r.Get("/screenshot", h.GetScreenshot)
	r.Get("/state", h.GetState)
	r.Post("/describe", h.DescribePage)
}

// CreateSession creates a new browser session
// POST /api/pi/web/session
func (h *WebHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Generate a session ID if none provided
		req.SessionID = fmt.Sprintf("web-%d", r.Context().Value("request_id"))
	}

	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("web-%d", generateID())
	}

	if err := h.browserClient.CreateSession(r.Context(), req.SessionID); err != nil {
		h.logger.Error("failed to create browser session", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sessionId": req.SessionID})
}

// CloseSession closes a browser session
// DELETE /api/pi/web/session
func (h *WebHandler) CloseSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.browserClient.CloseSession(req.SessionID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// Navigate navigates to a URL
// POST /api/pi/web/navigate
func (h *WebHandler) Navigate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL       string `json:"url"`
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	info, err := h.browserClient.Navigate(r.Context(), req.SessionID, req.URL)
	if err != nil {
		h.logger.Error("navigate failed", zap.Error(err), zap.String("url", req.URL))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// Click clicks an element
// POST /api/pi/web/click
func (h *WebHandler) Click(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ElementID int    `json:"elementId"`
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	info, err := h.browserClient.Click(r.Context(), req.SessionID, req.ElementID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// Type types text into an element
// POST /api/pi/web/type
func (h *WebHandler) Type(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ElementID int    `json:"elementId"`
		Text      string `json:"text"`
		Submit    bool   `json:"submit"`
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	info, err := h.browserClient.Type(r.Context(), req.SessionID, req.ElementID, req.Text, req.Submit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// Scroll scrolls the page
// POST /api/pi/web/scroll
func (h *WebHandler) Scroll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Direction string `json:"direction"` // "up" or "down"
		Amount    int    `json:"amount"`
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Amount == 0 {
		req.Amount = 300
	}

	info, err := h.browserClient.Scroll(r.Context(), req.SessionID, req.Direction, req.Amount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// GetScreenshot returns the current screenshot as raw PNG
// GET /api/pi/web/screenshot?sessionId=X
func (h *WebHandler) GetScreenshot(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "sessionId required", http.StatusBadRequest)
		return
	}

	png, err := h.browserClient.GetScreenshot(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

// GetState returns current page state without a screenshot
// GET /api/pi/web/state?sessionId=X
func (h *WebHandler) GetState(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "sessionId required", http.StatusBadRequest)
		return
	}

	info, err := h.browserClient.GetState(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// DescribePage sends the current screenshot to the vision model
// POST /api/pi/web/describe
func (h *WebHandler) DescribePage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	screenshot, err := h.browserClient.GetScreenshot(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	description, err := h.visionClient.DescribePage(r.Context(), screenshot)
	if err != nil {
		h.logger.Error("vision describe failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"description": description})
}

// generateID returns a simple unique ID for session names
func generateID() int64 {
	return rng.Int63()
}
