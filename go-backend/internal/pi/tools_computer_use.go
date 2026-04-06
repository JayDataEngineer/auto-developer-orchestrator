// Computer Use Mode - LLM Tool Definition
// This tool allows the LLM agent to request visual desktop access

package pi

// ComputerUseModeTool is the tool definition for enabling desktop mode
const ComputerUseModeTool = `{
  "name": "enter_computer_use_mode",
  "description": "Enable visual desktop access for GUI tasks (open apps, install software, use browsers, interact with desktop applications). This launches a desktop environment and spawns a computer_use sub-agent to handle the task.",
  "parameters": {
    "type": "object",
    "properties": {
      "task": {
        "type": "string",
        "description": "What the sub-agent should do on the desktop (e.g., 'install telegram-desktop', 'open browser and navigate to example.com')"
      }
    },
    "required": ["task"]
  }
}`

// ComputerUseModeResponse is returned when desktop mode is enabled
type ComputerUseModeResponse struct {
	// SandboxID is the sandbox identifier
	SandboxID string `json:"sandbox_id"`
	// DesktopMode indicates if desktop mode is active
	DesktopMode bool `json:"desktop_mode"`
	// Session contains desktop session details if active
	Session *DesktopSessionInfo `json:"session,omitempty"`
	// Message is a human-readable status message
	Message string `json:"message"`
}

// DesktopSessionInfo contains desktop session connection details
type DesktopSessionInfo struct {
	// DisplayNum is the X display number (:1, :2, etc.)
	DisplayNum int `json:"display_num"`
	// VNCPort is the VNC server port
	VNCPort int `json:"vnc_port"`
	// CDPPort is the Chrome DevTools Protocol port
	CDPPort int `json:"cdp_port"`
	// NoVNCPort is the noVNC web viewer port
	NoVNCPort int `json:"novnc_port"`
	// ViewerURL is the URL to open the desktop viewer popup
	ViewerURL string `json:"viewer_url"`
	// StartedAt is when desktop mode was activated
	StartedAt string `json:"started_at"`
}
