package extensions

// StartupResult records the result of attempting to start an extension.
type StartupResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Extension represents a discovered extension with its configuration.
type Extension struct {
	Name        string       // Unique identifier, used as MCP prefix and tool package name
	Version     string       // Semantic version
	Description string       // Human-readable description
	Dir         string       // Absolute path to the extension directory
	Server      ServerConfig // How to start the extension's MCP server
}

// ServerConfig defines how to start an extension's MCP server subprocess.
type ServerConfig struct {
	Command string   // Command to run (default: "bun")
	Args    []string // Arguments (default: ["run", "server.ts"])
	Timeout int      // Startup timeout in seconds (default: 15)
}

// ExtensionConfig is the YAML structure for extension.yaml manifests.
type ExtensionConfig struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
	Server      struct {
		Command string   `yaml:"command"`
		Args    []string `yaml:"args"`
		Timeout int      `yaml:"timeout"`
	} `yaml:"server"`
}
