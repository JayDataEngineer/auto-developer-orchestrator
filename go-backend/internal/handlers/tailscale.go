package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

// TailscaleDevice represents a peer from tailscale status.
type TailscaleDevice struct {
	Name       string   `json:"name"`
	Hostname   string   `json:"hostname"`
	TailscaleIPs []string `json:"tailscaleIPs"`
	OS         string   `json:"os"`
	Online     bool     `json:"online"`
}

// TailscaleHandler provides Tailscale device discovery.
type TailscaleHandler struct {
	log *zap.Logger
}

// NewTailscaleHandler creates a new Tailscale handler.
func NewTailscaleHandler(log *zap.Logger) *TailscaleHandler {
	return &TailscaleHandler{log: log}
}

// tailscaleStatus is the JSON structure from `tailscale status --json`.
type tailscaleStatus struct {
	Self   tailscalePeer            `json:"Self"`
	Peer   map[string]tailscalePeer `json:"Peer"`
}

type tailscalePeer struct {
	HostName      string   `json:"HostName"`
	DNSName       string   `json:"DNSName"`
	TailscaleIPs  []string `json:"TailscaleIPs"`
	OS            string   `json:"OS"`
	Online        bool     `json:"Online"`
}

// Devices handles GET /api/tailscale/devices
func (h *TailscaleHandler) Devices(w http.ResponseWriter, r *http.Request) {
	cmd := exec.Command("tailscale", "status", "--json")
	cmd.Env = nil // use default env

	output, err := cmd.Output()
	if err != nil {
		// tailscale not installed or not running
		h.log.Debug("tailscale status failed", zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"devices":   []TailscaleDevice{},
			"error":     "tailscale not available",
		})
		return
	}

	var status tailscaleStatus
	if err := json.Unmarshal(output, &status); err != nil {
		JSONError(w, "failed to parse tailscale status", http.StatusInternalServerError)
		return
	}

	devices := make([]TailscaleDevice, 0, len(status.Peer)+1)

	// Add self
	if status.Self.HostName != "" {
		devices = append(devices, TailscaleDevice{
			Name:         status.Self.DNSName,
			Hostname:     status.Self.HostName,
			TailscaleIPs: status.Self.TailscaleIPs,
			OS:           status.Self.OS,
			Online:       true,
		})
	}

	// Add peers
	for _, peer := range status.Peer {
		if !peer.Online {
			continue
		}
		devices = append(devices, TailscaleDevice{
			Name:         peer.DNSName,
			Hostname:     peer.HostName,
			TailscaleIPs: peer.TailscaleIPs,
			OS:           peer.OS,
			Online:       peer.Online,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
		"devices":   devices,
		"checkedAt": time.Now().Format(time.RFC3339),
	})
}
