package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"gopkg.in/yaml.v3"
)

// ToOpenAITools converts Tool list to OpenAI format.
func ToOpenAITools(tools []core.Tool) []core.OpenAITool {
	result := make([]core.OpenAITool, 0, len(tools))
	for _, t := range tools {
		result = append(result, core.OpenAITool{
			Type: "function",
			Function: core.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return result
}

// AgentRole holds a loaded agent definition from config/agents/<name>/
type AgentRole struct {
	Name        string
	Description string
	Prompt      string
	Tools       []string
	MaxRounds   int
	Temperature float32
}

// agentConfig is the YAML structure for config/agents/<name>/config.yaml
type agentConfig struct {
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	MaxRounds   int      `yaml:"max_rounds"`
	Temperature float64  `yaml:"temperature"`
}

// promptData holds template variables for the main system prompt.
type promptData struct {
	Tools         string
	Agents        string
	SandboxID     string
	Skills        string
	ProjectContext string
}

var (
	promptTmpl    *template.Template
	promptLoadErr error
	promptOnce    sync.Once

	agentRoles    map[string]*AgentRole
	agentLoadOnce sync.Once
)

// loadPromptTemplate loads and parses config/prompt.md as a Go text/template.
func loadPromptTemplate() (*template.Template, error) {
	promptOnce.Do(func() {
		root := os.Getenv("PROJECT_ROOT")
		path := "config/prompt.md"
		if root != "" {
			path = filepath.Join(root, "config", "prompt.md")
		}

		data, err := os.ReadFile(path)
		if err != nil {
			// Use embedded fallback
			promptTmpl, promptLoadErr = template.New("system").Parse(defaultPrompt)
			return
		}
		promptTmpl, promptLoadErr = template.New("system").Parse(string(data))
	})
	return promptTmpl, promptLoadErr
}

// ReloadPromptTemplate forces a reload of all prompt templates (for development).
func ReloadPromptTemplate() {
	promptOnce = sync.Once{}
	agentLoadOnce = sync.Once{}
}

// LoadAgentRoles reads agent folders from config/agents/ directory.
// Each folder must contain config.yaml (metadata) and prompt.md (system prompt).
// Falls back to loading legacy .md files with YAML frontmatter.
func LoadAgentRoles() map[string]*AgentRole {
	agentLoadOnce.Do(func() {
		agentRoles = make(map[string]*AgentRole)

		root := os.Getenv("PROJECT_ROOT")
		dir := "config/agents"
		if root != "" {
			dir = filepath.Join(root, "config", "agents")
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		for _, entry := range entries {
			if entry.IsDir() {
				// Folder-based agent: config.yaml + prompt.md
				role := loadAgentFromFolder(filepath.Join(dir, entry.Name()))
				if role != nil {
					role.Name = entry.Name()
					agentRoles[entry.Name()] = role
				}
			} else if strings.HasSuffix(entry.Name(), ".md") {
				// Legacy single-file agent
				name := strings.TrimSuffix(entry.Name(), ".md")
				data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
				if err != nil {
					continue
				}
				role := parseLegacyAgentRole(name, string(data))
				agentRoles[name] = role
			}
		}
	})
	return agentRoles
}

// loadAgentFromFolder reads config.yaml + prompt.md from an agent folder.
func loadAgentFromFolder(folder string) *AgentRole {
	cfg, err := os.ReadFile(filepath.Join(folder, "config.yaml"))
	if err != nil {
		return nil
	}

	var ac agentConfig
	if err := yaml.Unmarshal(cfg, &ac); err != nil {
		return nil
	}

	prompt, err := os.ReadFile(filepath.Join(folder, "prompt.md"))
	if err != nil {
		return nil
	}

	maxRounds := ac.MaxRounds
	if maxRounds == 0 {
		maxRounds = 15
	}

	temp := float32(0.4)
	if ac.Temperature != 0 {
		temp = float32(ac.Temperature)
	}

	return &AgentRole{
		Description: ac.Description,
		Prompt:      string(prompt),
		Tools:       ac.Tools,
		MaxRounds:   maxRounds,
		Temperature: temp,
	}
}

// parseLegacyAgentRole parses a legacy .md file with YAML frontmatter.
func parseLegacyAgentRole(name, content string) *AgentRole {
	role := &AgentRole{
		Name:        name,
		MaxRounds:   15,
		Temperature: 0.4,
		Tools:       []string{},
	}

	body := content

	if strings.HasPrefix(content, "---") {
		end := strings.Index(content[3:], "---")
		if end != -1 {
			frontmatter := content[3 : end+3]
			body = strings.TrimSpace(content[end+6:])

			for _, line := range strings.Split(frontmatter, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "description:") {
					role.Description = strings.Trim(strings.TrimPrefix(line, "description:"), " \"'")
				} else if strings.HasPrefix(line, "tools:") {
					toolsStr := strings.Trim(strings.TrimPrefix(line, "tools:"), " []")
					for _, t := range strings.Split(toolsStr, ",") {
						t = strings.TrimSpace(t)
						if t != "" {
							role.Tools = append(role.Tools, t)
						}
					}
				} else if strings.HasPrefix(line, "max_rounds:") {
					fmt.Sscanf(strings.TrimPrefix(line, "max_rounds:"), "%d", &role.MaxRounds)
				} else if strings.HasPrefix(line, "temperature:") {
					var f float64
					fmt.Sscanf(strings.TrimPrefix(line, "temperature:"), "%f", &f)
					role.Temperature = float32(f)
				}
			}
		}
	}

	role.Prompt = body
	return role
}

// GetAgentRole returns a specific agent role by name.
func GetAgentRole(name string) *AgentRole {
	roles := LoadAgentRoles()
	return roles[name]
}

// FormatAgentList returns a formatted list of available agents for the prompt.
func FormatAgentList() string {
	roles := LoadAgentRoles()
	if len(roles) == 0 {
		return "No agent roles loaded from config/agents/"
	}

	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		role := roles[name]
		fmt.Fprintf(&b, "### %s\n%s\nTools: %s\n\n", role.Name, role.Description, strings.Join(role.Tools, ", "))
	}
	return b.String()
}

// BuildOrchestratorPrompt builds the full system prompt using the template.
func BuildOrchestratorPrompt(tools []core.Tool, sandboxID string, projectContext string, examples string) string {
	tmpl, err := loadPromptTemplate()
	if err != nil {
		// Should not happen with fallback, but just in case
		return defaultPromptText(tools, sandboxID)
	}

	// Build tool list
	var toolSection strings.Builder
	for _, t := range tools {
		schema := formatSchema(t.Schema())
		fmt.Fprintf(&toolSection, "## %s — %s\n%s\n\n", t.Name(), t.Description(), schema)
	}

	data := promptData{
		Tools:          toolSection.String(),
		Agents:         FormatAgentList(),
		SandboxID:      sandboxID,
		ProjectContext: projectContext,
	}

	// Skills are handled separately by the caller
	if examples != "" {
		data.Skills = examples
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return defaultPromptText(tools, sandboxID)
	}

	return buf.String()
}

// formatSchema formats a JSON Schema as a readable string.
func formatSchema(schema json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		return string(schema)
	}

	props, _ := m["properties"].(map[string]any)
	if props == nil {
		return string(schema)
	}

	var args []string
	for name, prop := range props {
		pm, _ := prop.(map[string]any)
		desc := ""
		if d, ok := pm["description"].(string); ok {
			desc = d
		}
		typ, _ := pm["type"].(string)
		example := formatExample(typ)
		args = append(args, fmt.Sprintf("%s=%s (%s)", name, example, desc))
	}

	return fmt.Sprintf("Args: %s", strings.Join(args, ", "))
}

func formatExample(typ string) string {
	switch typ {
	case "string":
		return "\"...\""
	case "integer", "number":
		return "0"
	case "boolean":
		return "true"
	case "array":
		return "[]"
	default:
		return "\"...\""
	}
}

// Embedded fallback prompt — used when config/prompt.md is not found.
const defaultPrompt = `You are Pux — an orchestrator that dispatches employees to complete tasks.
You do NOT do the work yourself. Delegate using delegate_to and delegate_async.

# Tools

` + "{{.Tools}}" + `

# Employees

` + "{{.Agents}}" + `

# Rules
1. DELEGATE first, do yourself second
2. Use delegate_to with employee role names (researcher, coder, browser)
3. Synthesize results and respond concisely

{{if .SandboxID}}Sandbox ID: {{.SandboxID}}{{end}}
`

func defaultPromptText(tools []core.Tool, sandboxID string) string {
	var b strings.Builder
	b.WriteString("You are Pux orchestrator.\n\nTools:\n")
	for _, t := range tools {
		b.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
	}
	if sandboxID != "" {
		b.WriteString("\nSandbox ID: " + sandboxID + "\n")
	}
	return b.String()
}
