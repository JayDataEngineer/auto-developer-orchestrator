package sandbox

import (
	"time"
)

// SandboxMode represents the current mode of a sandbox
type SandboxMode string

const (
	// ModeCLI is headless CLI-only mode (default, minimal overhead)
	ModeCLI SandboxMode = "cli"
	// ModeBrowser is headless Chrome with CDP (lightweight, ~150MB)
	ModeBrowser SandboxMode = "browser"
	// ModeDesktop is full desktop with VNC (heavy, ~500MB+)
	ModeDesktop SandboxMode = "desktop"
)

// SandboxOptions configures a new sandbox instance
type SandboxOptions struct {
	// ID is the unique sandbox identifier
	ID string
	// ProjectPath is the path to the project directory
	ProjectPath string
	// Policy is the security policy name (e.g., "developer")
	Policy string
	// NetworkAllow is a comma-separated list of allowed hosts
	NetworkAllow string
	// MemoryLimit is the memory limit in MB
	MemoryLimit int
	// CPULimit is the CPU limit in cores
	CPULimit float64
}

// Sandbox represents an OpenShell sandbox instance
type Sandbox struct {
	ID             string        `json:"id"`
	ProjectPath    string        `json:"project_path"`
	Policy         string        `json:"policy"`
	Mode           SandboxMode   `json:"mode"`
	Status         SandboxStatus `json:"status"`
	CreatedAt      time.Time     `json:"created_at"`
	DesktopSession *DesktopSession `json:"desktop_session,omitempty"`
}

// SandboxStatus is the current state of a sandbox
type SandboxStatus string

const (
	StatusPending   SandboxStatus = "pending"
	StatusRunning   SandboxStatus = "running"
	StatusError     SandboxStatus = "error"
	StatusDestroyed SandboxStatus = "destroyed"
)

// DesktopSession represents an active desktop/browser mode session
type DesktopSession struct {
	SandboxID   string      `json:"sandbox_id"`
	Mode        SandboxMode `json:"mode"`  // "browser" or "desktop"
	DisplayNum  int         `json:"display_num"`  // :1, :2, etc. (desktop only)
	VNCPort     int         `json:"vnc_port"`     // 5901, 5902, etc. (desktop only)
	CDPPort     int         `json:"cdp_port"`     // 9222, 9223, etc.
	NoVNCPort   int         `json:"novnc_port"`   // 6081, 6082, etc. (desktop only)
	ViewerURL   string      `json:"viewer_url"`   // URL for the desktop viewer popup
	IsActive    bool        `json:"is_active"`
	StartedAt   time.Time   `json:"started_at"`
}

// PortAllocator manages dynamic port allocation for desktop sessions
type PortAllocator struct {
	nextDisplayNum int
	nextVNCPort    int
	nextCDPPort    int
	nextNoVNCPort  int
}

// NewPortAllocator creates a new port allocator starting from base ports
func NewPortAllocator() *PortAllocator {
	return &PortAllocator{
		nextDisplayNum: 1,
		nextVNCPort:    5901,
		nextCDPPort:    9222,
		nextNoVNCPort:  6081,
	}
}

// AllocatePorts reserves ports for a new desktop session
func (p *PortAllocator) AllocatePorts() (displayNum, vncPort, cdpPort, novncPort int) {
	displayNum = p.nextDisplayNum
	vncPort = p.nextVNCPort
	cdpPort = p.nextCDPPort
	novncPort = p.nextNoVNCPort

	p.nextDisplayNum++
	p.nextVNCPort++
	p.nextCDPPort++
	p.nextNoVNCPort++

	return displayNum, vncPort, cdpPort, novncPort
}
