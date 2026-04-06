// Computer Use Mode - LLM Tool Definition
// This tool allows the LLM agent to request visual desktop access

package pi

// ComputerUseModeTool is the tool definition for enabling desktop mode
const ComputerUseModeTool = `{
  "name": "enter_computer_use_mode",
  "description": "Enable visual desktop access for browser automation or GUI applications. This launches a desktop environment with Chrome browser and VNC access, allowing you to interact with graphical applications. Use this when you need to:\n- Use a web browser with visual feedback\n- Access Telegram Desktop or other GUI apps\n- Perform tasks that require visual monitoring\n- Show the user what you're doing in real-time",
  "parameters": {
    "type": "object",
    "properties": {
      "reason": {
        "type": "string",
        "description": "Explanation of why visual mode is needed (e.g., 'need to use browser to test web app', 'need to access Telegram Desktop to reply to messages', 'need to download files from web interface')"
      },
      "estimated_duration": {
        "type": "string",
        "description": "Estimated time needed in desktop mode (e.g., '5 minutes', '30 minutes')"
      }
    },
    "required": ["reason"]
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
