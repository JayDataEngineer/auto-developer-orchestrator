package llama

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ToolCategory groups related tools.
type ToolCategory string

const (
	CategoryExecution     ToolCategory = "execution"
	CategoryBrowser       ToolCategory = "browser"
	CategoryDesktop       ToolCategory = "desktop"
	CategoryOrchestration ToolCategory = "orchestration"
	CategoryMeta          ToolCategory = "meta"
)

// ToolSpec describes a single tool the model can call.
// This is the single source of truth — prompts, validation, and API tool definitions all read from here.
type ToolSpec struct {
	Name             string       // Tool name
	Category         ToolCategory // Grouping for persona whitelists
	Description      string       // One-line description for prompts
	Schema           string       // JSON example showing args (for system prompt text)
	ParametersSchema string       // JSON Schema for parameters (for tools API param). If empty, inferred from Schema.
	Returns          string       // What the tool returns (optional)
}

// allTools is the master registry. Each category file (tool_defs_*.go) appends
// its tools via its init() function, which runs before this file's init().
var allTools []ToolSpec

// toolIndex maps tool name → ToolSpec for O(1) lookup.
var toolIndex map[string]*ToolSpec

func init() {
	toolIndex = make(map[string]*ToolSpec, len(allTools))
	for i := range allTools {
		toolIndex[allTools[i].Name] = &allTools[i]
	}
}

// LookupTool returns the ToolSpec for a tool name, or nil if not found.
func LookupTool(name string) *ToolSpec {
	return toolIndex[name]
}

// ToolsByNames returns ToolSpecs for the given tool names (in order).
// Unknown names are silently skipped.
func ToolsByNames(names []string) []ToolSpec {
	var specs []ToolSpec
	for _, n := range names {
		if s := LookupTool(n); s != nil {
			specs = append(specs, *s)
		}
	}
	return specs
}

// SubAgentToolSpecs returns ToolSpecs for the given tool names, always including yield_artifact.
// Used by delegate_to to build the sub-agent's tool set. Unknown names are silently skipped.
func SubAgentToolSpecs(names []string) []ToolSpec {
	seen := make(map[string]bool, len(names)+1)
	var result []ToolSpec

	// Always include yield_artifact
	result = append(result, *LookupTool("yield_artifact"))
	seen["yield_artifact"] = true

	for _, n := range names {
		if seen[n] {
			continue
		}
		if s := LookupTool(n); s != nil && s.Category != CategoryOrchestration {
			// Sub-agents cannot use orchestration tools (delegate_to, create_plan, etc.)
			result = append(result, *s)
			seen[n] = true
		}
	}
	return result
}

// FormatToolList renders a list of ToolSpecs as the prompt block:
//
//	## tool_name — description
//	{"schema": "..."}
//	Returns: ...
func FormatToolList(specs []ToolSpec) string {
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Name < specs[j].Name
	})

	var b strings.Builder
	for _, s := range specs {
		fmt.Fprintf(&b, "## %s — %s\n%s", s.Name, s.Description, s.Schema)
		if s.Returns != "" {
			b.WriteString("\n" + s.Returns)
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

// PersonaToolNames returns the default tool whitelist for a persona type.
// Only PersonaOrchestrator has a whitelist — sub-agents get dynamic tool sets via delegate_to.
func PersonaToolNames(t PersonaType) []string {
	switch t {
	case PersonaOrchestrator:
		// The orchestrator handles tasks directly with core tools and DELEGATES
		// research/scraping to sub-agents. Individual mcp_* tools are NOT in this
		// list — they're registered globally for SubAgentToolSpecs (sub-agents use them).
		// The orchestrator's prompt includes a reference section describing MCP tools
		// so it can write accurate delegate_to instructions.
		return []string{
			// Execution
			"bash",
			// File operations (Claude Code pattern)
			"file_read", "file_write", "file_edit", "file_grep", "file_glob",
			// Code intelligence
			"code_search",
			// Browser (for navigation tasks the orchestrator handles directly)
			"search_web", "browse_to", "click_element", "type_text", "read_page", "observe", "scroll_page",
			// MCP catch-all for quick lookups (sub-agents get the individual mcp_* tools)
			"mcp_call",
			// Desktop
			"computer_use_enable", "computer_use_screenshot", "computer_use_snapshot", "computer_use_act",
			"desktop_screenshot", "desktop_click", "desktop_type", "desktop_key",
			// Orchestration
			"delegate_to", "delegate_async", "collect_results", "create_plan", "update_plan", "clarify", "synthesize",
			// Meta
			"update_memory", "wait", "ask_user",
		}
	default:
		return nil
	}
}

// PersonaToolSpecs returns ToolSpecs for a persona's tool whitelist.
func PersonaToolSpecs(t PersonaType) []ToolSpec {
	return ToolsByNames(PersonaToolNames(t))
}

// PersonaExamples returns few-shot examples for a persona type.
// Only the orchestrator has examples — sub-agents learn from their custom instructions.
func PersonaExamples(t PersonaType) []Example {
	switch t {
	case PersonaOrchestrator:
		return []Example{
			{
				Title: "Run a shell command",
				Content: `bash{"command":"echo 'Hello World'"}
→ Hello World`,
			},
			{
				Title: "Read and edit a file",
				Content: `file_read{"file_path":"/sandbox/workspace/main.go"}
→      1  package main
→      2  import "fmt"
→      3  func main() {
→      4      fmt.Println("hello")
→      5  }

file_edit{"file_path":"/sandbox/workspace/main.go","old_string":"fmt.Println(\"hello\")","new_string":"fmt.Println(\"world\")"}
→ Replaced 1 occurrence in main.go`,
			},
			{
				Title: "Search code and find files",
				Content: `file_grep{"pattern":"func main","path":"/sandbox/workspace","output_mode":"content"}
→ main.go:3:func main() {

file_glob{"pattern":"*.go","path":"/sandbox/workspace"}
→ main.go, utils.go, handler.go`,
			},
			{
				Title: "Browse a website and interact",
				Content: `browse_to{"url":"https://example.com"}
→ Page loaded. Elements: [1] <a> "More information" [2] <h1> "Example Domain"

click_element{"element":1}
→ Navigated to new page with more content.`,
			},
			{
				Title: "Take a desktop screenshot",
				Content: `desktop_screenshot{}
→ Screenshot captured. Resolution: 1920x1080.

desktop_click{"x":500,"y":300}
→ Clicked at (500, 300).`,
			},
			{
				Title: "Delegate to a sub-agent with custom instructions",
				Content: `create_plan{"steps":["Research golang concurrency","Write demo script","Test it"]}
delegate_to{"task":"Research golang concurrency patterns","instructions":"Search for golang concurrency patterns. For each pattern (goroutines, channels, select, sync.WaitGroup), find a code example and explain it. Cite sources.","tools":["mcp_research","mcp_scrape","search_web"]}
update_plan{"step_index":0,"status":"done","note":"Research complete: goroutines, channels, select"}
delegate_to{"task":"Write a Go script demonstrating goroutines and channels","instructions":"Write a Go script that demonstrates goroutines launching concurrent tasks, channels for communication, and select for multiplexing. Include comments explaining each pattern.","tools":["bash","file_write","file_read"]}
update_plan{"step_index":1,"status":"done"}
synthesize{"conclusion":"Research and script complete."}`,
			},
		}
	default:
		return nil
	}
}

// ── OpenAI tool format conversion ──────────────────────────────────

// ToOpenAITools converts a list of ToolSpecs to OpenAI function calling format
// for the `tools` parameter of /v1/chat/completions.
func ToOpenAITools(specs []ToolSpec) []OpenAITool {
	tools := make([]OpenAITool, len(specs))
	for i, s := range specs {
		var params json.RawMessage
		if s.ParametersSchema != "" {
			params = json.RawMessage(s.ParametersSchema)
		} else {
			params = schemaExampleToJSONSchema(s.Schema)
		}
		tools[i] = OpenAITool{
			Type: "function",
			Function: FunctionDef{
				Name:        s.Name,
				Description: s.Description,
				Parameters:  params,
			},
		}
	}
	return tools
}

// PersonaOpenAITools returns the tools array for a persona's whitelist in OpenAI format.
func PersonaOpenAITools(t PersonaType) []OpenAITool {
	return ToOpenAITools(PersonaToolSpecs(t))
}

// schemaExampleToJSONSchema infers a JSON Schema from a JSON example string.
// Used as fallback when ParametersSchema is not explicitly set.
func schemaExampleToJSONSchema(example string) json.RawMessage {
	example = strings.TrimSpace(example)
	if example == "" || example == `{}` {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(example), &obj); err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}

	props := make(map[string]interface{})
	required := make([]string, 0, len(obj))

	for key, val := range obj {
		prop := map[string]interface{}{}
		switch v := val.(type) {
		case string:
			prop["type"] = "string"
		case float64:
			if v == float64(int(v)) {
				prop["type"] = "integer"
			} else {
				prop["type"] = "number"
			}
		case bool:
			prop["type"] = "boolean"
		case []interface{}:
			prop["type"] = "array"
			prop["items"] = map[string]string{"type": "string"}
		default:
			prop["type"] = "string"
		}
		props[key] = prop
		required = append(required, key)
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}

	b, _ := json.Marshal(schema)
	return json.RawMessage(b)
}
