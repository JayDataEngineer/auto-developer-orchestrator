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

	// Collect self IPs to filter them out
	selfIPs := map[string]bool{}
	for _, ip := range status.Self.TailscaleIPs {
		selfIPs[ip] = true
	}

	devices := make([]TailscaleDevice, 0, len(status.Peer))

	// Add peers only (skip self — no point SSHing into yourself)
	for _, peer := range status.Peer {
		if !peer.Online {
			continue
		}
		// Skip funnel ingress nodes — they have no SSH server
		if peer.HostName == "funnel-ingress-node" {
			continue
		}
		// Skip any device whose IPs overlap with self
		isSelf := true
		for _, ip := range peer.TailscaleIPs {
			if !selfIPs[ip] {
				isSelf = false
				break
			}
		}
		if isSelf {
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
