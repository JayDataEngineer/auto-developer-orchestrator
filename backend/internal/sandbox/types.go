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

// VNCBackend identifies which VNC server the sandbox container uses.
type VNCBackend string

const (
	// BackendStandard uses TigerVNC + noVNC + websockify (default, lightweight)
	BackendStandard VNCBackend = "standard"
	// BackendKasm uses KasmVNC with H.264/WebRTC (low latency)
	BackendKasm VNCBackend = "kasm"
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
	// InitialMode sets the sandbox mode after creation (cli, browser, desktop).
	// Default is "browser" — the OpenShell image already runs Chrome via supervisord.
	InitialMode SandboxMode
	// Tier controls sandbox isolation: "isolated" (default), "bridged", "native"
	Tier SandboxTier
	// Image overrides the default sandbox image (OPENSHELL_IMAGE env or
	// pux-sandbox:latest). When non-empty, the manager pulls if missing and
	// uses this image for the container. Used by org mode to honor the
	// org's declared specialized sandbox image (e.g. video-production's
	// manim+kokoro image). Empty = use default.
	Image string
	// Env appends additional environment variables to the sandbox container
	// (format: "KEY=VALUE"). Used by org mode to propagate the org's declared
	// sandbox env (e.g. VIDEO_PRODUCTION_ROOT). Empty = no extras.
	Env []string
	// Volumes appends additional Docker bind mounts to the sandbox container.
	// Used by org mode to honor org-declared volumes (e.g. video-production's
	// named workspace volume). Empty = only the default project/policies/tmp/persist
	// binds apply.
	Volumes []SandboxVolume
	// IdleShutdownSecs controls the watchdog's idle-teardown threshold for
	// this sandbox. 0 = never auto-shutdown (preserves pre-PR4 behavior).
	// Non-zero = if no tool execution touches the sandbox for this many
	// seconds, the watchdog calls ShutdownByProjectLabel.
	//
	// Plumbed in from org.toml [sandbox].idle_shutdown_secs at sandbox
	// creation. Default 0 (off).
	IdleShutdownSecs int
}

// SandboxVolume mirrors one row of pux.yaml's sandbox.volumes: block. The
// org.toml schema validator (scripts/org_build.py) already enforces that
// type=volume has name and type=bind has host; both require container.
type SandboxVolume struct {
	// Type is "volume" (Docker named volume) or "bind" (host path mount).
	Type string `yaml:"type" json:"type"`
	// Name is the local logical name for a named volume. Used as the bind
	// source when DockerName is empty.
	Name string `yaml:"name" json:"name,omitempty"`
	// DockerName is the actual Docker volume name (may differ from Name when
	// the org wants a stable external name like research_video_prod_workspace).
	// Empty = use Name as the Docker volume name.
	DockerName string `yaml:"docker_name" json:"docker_name,omitempty"`
	// Host is the host-side path for type=bind. Ignored for type=volume.
	Host string `yaml:"host" json:"host,omitempty"`
	// Container is the in-container mount point. Always required.
	Container string `yaml:"container" json:"container"`
}

// BindString renders the volume in Docker --mount/-v bind syntax:
//
//	type=volume → "<dockerName-or-name>:<container>"
//	type=bind   → "<host>:<container>"
//
// Returns empty string when the entry is incomplete (missing container or
// the type-specific source) so callers can skip without building broken binds.
func (v SandboxVolume) BindString() string {
	if v.Container == "" {
		return ""
	}
	switch v.Type {
	case "bind":
		if v.Host == "" {
			return ""
		}
		return v.Host + ":" + v.Container
	case "", "volume":
		src := v.DockerName
		if src == "" {
			src = v.Name
		}
		if src == "" {
			return ""
		}
		return src + ":" + v.Container
	default:
		return ""
	}
}

// Sandbox represents an OpenShell sandbox instance
type Sandbox struct {
	ID             string          `json:"id"`
	ContainerID    string          `json:"container_id,omitempty"`
	ProjectPath    string          `json:"project_path"`
	Policy         string          `json:"policy"`
	Mode           SandboxMode     `json:"mode"`
	Status         SandboxStatus   `json:"status"`
	CreatedAt      time.Time       `json:"created_at"`
	DesktopSession *DesktopSession `json:"desktop_session,omitempty"`
	Tier           SandboxTier     `json:"tier,omitempty"`
	VNCBackend     VNCBackend      `json:"vnc_backend,omitempty"`
	// LastActivityAt is updated on every tool execution that touches this
	// sandbox (ExecInSandbox, CopyToSandbox, PipInstall, WriteEnvFile).
	// The watchdog goroutine reads it to decide idle-teardown.
	// Zero value = treat as CreatedAt (sandbox just booted, not idle yet).
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
	// IdleShutdownSecs mirrors SandboxOptions.IdleShutdownSecs. 0 = off.
	// Set once at CreateSandbox and unchanged for the sandbox's lifetime.
	IdleShutdownSecs int `json:"idle_shutdown_secs,omitempty"`
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
	SandboxID  string      `json:"sandbox_id"`
	Mode       SandboxMode `json:"mode"`           // "browser" or "desktop"
	DisplayNum int         `json:"display_num"`    // :1, :2, etc. (desktop only)
	VNCPort    int         `json:"vnc_port"`       // 5901, 5902, etc. (desktop only)
	CDPPort    int         `json:"cdp_port"`       // 9222, 9223, etc.
	NoVNCPort  int         `json:"novnc_port"`     // 6081/8444, web viewer port
	ViewerURL  string      `json:"viewer_url"`     // URL for the desktop viewer popup
	IsActive   bool        `json:"is_active"`
	StartedAt  time.Time   `json:"started_at"`
	Backend    VNCBackend  `json:"backend,omitempty"` // "standard" or "kasm"
}

// PortAllocator manages dynamic port allocation for desktop sessions
type PortAllocator struct {
	nextDisplayNum int
	nextVNCPort    int
	nextCDPPort    int
	nextNoVNCPort  int
	usedDisplays   map[int]bool
	usedVNC        map[int]bool
	usedCDP        map[int]bool
	usedNoVNC      map[int]bool
}

// NewPortAllocator creates a new port allocator starting from base ports
func NewPortAllocator() *PortAllocator {
	return &PortAllocator{
		nextDisplayNum: 1,
		nextVNCPort:    5901,
		nextCDPPort:    9223, // 9222 is used by base image's Chrome
		nextNoVNCPort:  6081,
		usedDisplays:   make(map[int]bool),
		usedVNC:        make(map[int]bool),
		usedCDP:        make(map[int]bool),
		usedNoVNC:      make(map[int]bool),
	}
}

// AllocatePorts reserves ports for a new desktop session
func (p *PortAllocator) AllocatePorts() (displayNum, vncPort, cdpPort, novncPort int) {
	// Find next available display number
	for p.usedDisplays[p.nextDisplayNum] {
		p.nextDisplayNum++
	}
	displayNum = p.nextDisplayNum
	p.usedDisplays[displayNum] = true
	p.nextDisplayNum++

	// Find next available VNC port (range: 5901-5920)
	for p.usedVNC[p.nextVNCPort] && p.nextVNCPort <= 5920 {
		p.nextVNCPort++
	}
	vncPort = p.nextVNCPort
	p.usedVNC[vncPort] = true
	if p.nextVNCPort < 5920 {
		p.nextVNCPort++
	}

	// Find next available CDP port (range: 9222-9241)
	for p.usedCDP[p.nextCDPPort] && p.nextCDPPort <= 9241 {
		p.nextCDPPort++
	}
	cdpPort = p.nextCDPPort
	p.usedCDP[cdpPort] = true
	if p.nextCDPPort < 9241 {
		p.nextCDPPort++
	}

	// Find next available noVNC port (range: 6081-6100)
	for p.usedNoVNC[p.nextNoVNCPort] && p.nextNoVNCPort <= 6100 {
		p.nextNoVNCPort++
	}
	novncPort = p.nextNoVNCPort
	p.usedNoVNC[novncPort] = true
	if p.nextNoVNCPort < 6100 {
		p.nextNoVNCPort++
	}

	return displayNum, vncPort, cdpPort, novncPort
}

// ReleasePorts frees ports previously allocated for a desktop session
func (p *PortAllocator) ReleasePorts(displayNum, vncPort, cdpPort, novncPort int) {
	delete(p.usedDisplays, displayNum)
	delete(p.usedVNC, vncPort)
	delete(p.usedCDP, cdpPort)
	delete(p.usedNoVNC, novncPort)

	// Reset cursors if released ports are below current position
	if displayNum < p.nextDisplayNum {
		p.nextDisplayNum = displayNum
	}
	if vncPort < p.nextVNCPort {
		p.nextVNCPort = vncPort
	}
	if cdpPort < p.nextCDPPort {
		p.nextCDPPort = cdpPort
	}
	if novncPort < p.nextNoVNCPort {
		p.nextNoVNCPort = novncPort
	}
}
