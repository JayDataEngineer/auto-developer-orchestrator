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

// allTools is the master registry. Every tool the system knows lives here.
var allTools = []ToolSpec{
	// Execution
	{
		Name:             "bash",
		Category:         CategoryExecution,
		Description:      "run a shell command",
		Schema:           `{"command": "ls -la"}`,
		Returns:          "Runs in sandbox as root. Working dir: /sandbox/workspace",
		ParametersSchema: `{"type":"object","properties":{"command":{"type":"string","description":"Shell command to run"}},"required":["command"]}`,
	},

	// File Operations (Claude Code pattern: read before edit)
	{
		Name:             "file_read",
		Category:         CategoryExecution,
		Description:      "read a file with line numbers",
		Schema:           `{"file_path": "/sandbox/workspace/main.go", "offset": 10, "limit": 50}`,
		Returns:          "Returns file content with line numbers. Must read before editing.",
		ParametersSchema: `{"type":"object","properties":{"file_path":{"type":"string","description":"Absolute path to file in sandbox"},"offset":{"type":"integer","description":"Starting line number (1-based, optional)"},"limit":{"type":"integer","description":"Max lines to return (optional)"}},"required":["file_path"]}`,
	},
	{
		Name:             "file_write",
		Category:         CategoryExecution,
		Description:      "create a new file (cannot overwrite — use file_edit for existing files)",
		Schema:           `{"file_path": "/sandbox/workspace/newfile.go", "content": "package main\nfunc main() {}"}`,
		Returns:          "Creates parent dirs automatically. REFUSES to overwrite existing files — use file_edit instead.",
		ParametersSchema: `{"type":"object","properties":{"file_path":{"type":"string","description":"Absolute path in sandbox — must NOT already exist"},"content":{"type":"string","description":"File content to write"}},"required":["file_path","content"]}`,
	},
	{
		Name:             "file_edit",
		Category:         CategoryExecution,
		Description:      "replace exact string in a file (must read first)",
		Schema:           `{"file_path": "/sandbox/workspace/main.go", "old_string": "fmt.Println(\"hello\")", "new_string": "fmt.Println(\"world\")"}`,
		Returns:          "Finds and replaces old_string with new_string. Set replace_all for multiple matches. File must be read first.",
		ParametersSchema: `{"type":"object","properties":{"file_path":{"type":"string","description":"Absolute path in sandbox"},"old_string":{"type":"string","description":"Exact text to find and replace"},"new_string":{"type":"string","description":"Replacement text"},"replace_all":{"type":"boolean","description":"Replace all occurrences (default: false)"}},"required":["file_path","old_string","new_string"]}`,
	},
	{
		Name:             "file_grep",
		Category:         CategoryExecution,
		Description:      "search files with regex (like ripgrep)",
		Schema:           `{"pattern": "func main", "path": "/sandbox/workspace", "output_mode": "content", "context_lines": 3}`,
		Returns:          "Returns matching lines with context. Modes: content, files_with_matches, count.",
		ParametersSchema: `{"type":"object","properties":{"pattern":{"type":"string","description":"Regex pattern to search for"},"path":{"type":"string","description":"Directory or file to search (default: /sandbox/workspace)"},"glob":{"type":"string","description":"File filter: '*.go', '*.{ts,tsx}'"},"output_mode":{"type":"string","description":"Output format: content, files_with_matches, count","enum":["content","files_with_matches","count"]},"context_lines":{"type":"integer","description":"Lines of context around matches"},"case_insensitive":{"type":"boolean","description":"Case-insensitive search"},"head_limit":{"type":"integer","description":"Max number of results (default: 50)"}},"required":["pattern"]}`,
	},
	{
		Name:             "file_glob",
		Category:         CategoryExecution,
		Description:      "find files by name pattern",
		Schema:           `{"pattern": "*.go", "path": "/sandbox/workspace"}`,
		Returns:          "Returns file paths sorted by modification time.",
		ParametersSchema: `{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern: *.go, **/*.ts, test_*.py"},"path":{"type":"string","description":"Directory to search (default: /sandbox/workspace)"}},"required":["pattern"]}`,
	},
	{
		Name:             "code_search",
		Category:         CategoryExecution,
		Description:      "search code with structured operations (find references, definitions, symbols)",
		Schema:           `{"operation": "find_references", "symbol": "handleRequest", "path": "/sandbox/workspace", "file_type": "go"}`,
		Returns:          "Operations: find_references, find_definition, list_symbols, hover. Uses ripgrep with language-aware patterns.",
		ParametersSchema: `{"type":"object","properties":{"operation":{"type":"string","description":"Operation: find_references, find_definition, list_symbols, hover","enum":["find_references","find_definition","list_symbols","hover"]},"symbol":{"type":"string","description":"Symbol name to search for"},"path":{"type":"string","description":"Directory to search (default: /sandbox/workspace)"},"file_type":{"type":"string","description":"Language: go, py, js, ts, rs, java, rb"}},"required":["operation"]}`,
	},
	{
		Name:             "image_read",
		Category:         CategoryExecution,
		Description:      "read and describe an image file using vision model",
		Schema:           `{"file_path": "/sandbox/workspace/screenshot.png", "prompt": "What does this show?"}`,
		Returns:          "Sends the image to the vision model and returns a text description. Supports PNG, JPG, GIF, WebP.",
		ParametersSchema: `{"type":"object","properties":{"file_path":{"type":"string","description":"Path to image file in sandbox"},"prompt":{"type":"string","description":"What to describe (default: general description)"}},"required":["file_path"]}`,
	},
	{
		Name:             "http_request",
		Category:         CategoryExecution,
		Description:      "make an HTTP request to any URL (APIs, local services, webhooks)",
		Schema:           `{"method": "POST", "url": "http://localhost:8188/prompt", "headers": {"Content-Type": "application/json"}, "body": "{\"workflow\": ...}", "timeout": 30}`,
		Returns:          "Returns status code, response headers, and body. Supports GET, POST, PUT, DELETE, PATCH. Body can be string or JSON object.",
		ParametersSchema: `{"type":"object","properties":{"method":{"type":"string","description":"HTTP method: GET, POST, PUT, DELETE, PATCH","enum":["GET","POST","PUT","DELETE","PATCH"]},"url":{"type":"string","description":"Full URL to request"},"headers":{"type":"object","description":"Request headers as key-value pairs"},"body":{"type":"string","description":"Request body (string or JSON)"},"timeout":{"type":"integer","description":"Timeout in seconds (default: 30, max: 120)"}},"required":["method","url"]}`,
	},

	// Browser
	{
		Name:             "search_web",
		Category:         CategoryBrowser,
		Description:      "search Google and return results (ONE step)",
		Schema:           `{"query": "cats"}`,
		Returns:          "Returns search results. Use this for any search task.",
		ParametersSchema: `{"type":"object","properties":{"query":{"type":"string","description":"Search query"}},"required":["query"]}`,
	},
	{
		Name:             "browse_to",
		Category:         CategoryBrowser,
		Description:      "open a URL",
		Schema:           `{"url": "https://example.com"}`,
		Returns:          "Returns page with clickable elements.",
		ParametersSchema: `{"type":"object","properties":{"url":{"type":"string","description":"URL to navigate to"}},"required":["url"]}`,
	},
	{
		Name:             "click_element",
		Category:         CategoryBrowser,
		Description:      "click element by ID number",
		Schema:           `{"element": 5}`,
		ParametersSchema: `{"type":"object","properties":{"element":{"type":"integer","description":"Element ID to click"}},"required":["element"]}`,
	},
	{
		Name:             "type_text",
		Category:         CategoryBrowser,
		Description:      "type text into an element",
		Schema:           `{"element": 1, "text": "hello", "submit": true}`,
		Returns:          "submit:true presses Enter after typing.",
		ParametersSchema: `{"type":"object","properties":{"element":{"type":"integer","description":"Element ID to type into"},"text":{"type":"string","description":"Text to type"},"submit":{"type":"boolean","description":"Press Enter after typing"}},"required":["element","text"]}`,
	},
	{
		Name:             "read_page",
		Category:         CategoryBrowser,
		Description:      "re-read current page content",
		Schema:           `{}`,
		Returns:          "Use when you need to see the page again.",
		ParametersSchema: `{"type":"object","properties":{}}`,
	},
	{
		Name:             "observe",
		Category:         CategoryBrowser,
		Description:      "observe current page: screenshot + DOM elements + AI description",
		Schema:           `{}`,
		Returns:          "Returns structured page state: elements with IDs, page title, URL, and AI description of what's visible. Use this to understand a page before acting.",
		ParametersSchema: `{"type":"object","properties":{}}`,
	},
	{
		Name:             "scroll_page",
		Category:         CategoryBrowser,
		Description:      "scroll the browser page up or down",
		Schema:           `{"direction": "down"}`,
		Returns:          "direction: 'up' or 'down'. Scrolls one viewport height.",
		ParametersSchema: `{"type":"object","properties":{"direction":{"type":"string","description":"Scroll direction: up or down","enum":["up","down"]}},"required":["direction"]}`,
	},
	{
		Name:             "scrape",
		Category:         CategoryBrowser,
		Description:      "fetch a URL and return its content as clean text",
		Schema:           `{"url": "https://example.com"}`,
		Returns:          "Returns page content as markdown. Use instead of browse_to when you just need the text.",
		ParametersSchema: `{"type":"object","properties":{"url":{"type":"string","description":"URL to fetch"}},"required":["url"]}`,
	},
	{
		Name:             "mcp_call",
		Category:         CategoryBrowser,
		Description:      "call any tool on the MCP research server (search, scrape, extract, map, crawl, docs, stats, proxy, etc.)",
		Schema:           `{"tool": "research", "arguments": {"query": "golang concurrency", "max_results": 3}}`,
		Returns:          "Returns the raw result from the MCP server as text. Use for any web research, extraction, or crawling task.",
		ParametersSchema: `{"type":"object","properties":{"tool":{"type":"string","description":"MCP tool name: research, search, scrape, extract, list_schemas, map, crawl, docs_list_sources, docs_fetch_docs, domains, stats, reset, clear_blacklist, proxy_status, proxy_test, proxy_rotate"},"arguments":{"type":"object","description":"Tool-specific arguments as a JSON object"}},"required":["tool","arguments"]}`,
	},

	// Desktop
	{
		Name:             "computer_use_enable",
		Category:         CategoryDesktop,
		Description:      "start the desktop environment",
		Schema:           `{}`,
		Returns:          "Starts desktop if not running. Returns CDP port.",
		ParametersSchema: `{"type":"object","properties":{}}`,
	},
	{
		Name:             "computer_use_screenshot",
		Category:         CategoryDesktop,
		Description:      "take screenshot",
		Schema:           `{} or {"describe": true}`,
		Returns:          "Takes screenshot. With describe:true, returns AI description.",
		ParametersSchema: `{"type":"object","properties":{"describe":{"type":"boolean","description":"Return AI description of screenshot"}}}`,
	},
	{
		Name:             "computer_use_snapshot",
		Category:         CategoryDesktop,
		Description:      "get interactive elements with IDs",
		Schema:           `{}`,
		Returns:          `Returns page elements: [ID] <tag> "text"`,
		ParametersSchema: `{"type":"object","properties":{}}`,
	},
	{
		Name:             "computer_use_act",
		Category:         CategoryDesktop,
		Description:      "click, type, navigate, scroll",
		Schema:           `{"action": "navigate", "url": "https://example.com"}`,
		Returns:          "Actions: navigate, click, type, scroll",
		ParametersSchema: `{"type":"object","properties":{"action":{"type":"string","description":"Action to perform: navigate, click, type, scroll","enum":["navigate","click","type","scroll"]},"url":{"type":"string","description":"URL for navigate action"},"element":{"type":"integer","description":"Element ID for click/type"},"text":{"type":"string","description":"Text for type action"},"direction":{"type":"string","description":"Scroll direction: up or down"}},"required":["action"]}`,
	},
	{
		Name:             "desktop_screenshot",
		Category:         CategoryDesktop,
		Description:      "X11 desktop screenshot",
		Schema:           `{}`,
		ParametersSchema: `{"type":"object","properties":{}}`,
	},
	{
		Name:             "desktop_click",
		Category:         CategoryDesktop,
		Description:      "X11 desktop click at coordinates",
		Schema:           `{"x": 100, "y": 200}`,
		ParametersSchema: `{"type":"object","properties":{"x":{"type":"integer","description":"X coordinate"},"y":{"type":"integer","description":"Y coordinate"}},"required":["x","y"]}`,
	},
	{
		Name:             "desktop_type",
		Category:         CategoryDesktop,
		Description:      "X11 desktop type text",
		Schema:           `{"text": "hello"}`,
		ParametersSchema: `{"type":"object","properties":{"text":{"type":"string","description":"Text to type"}},"required":["text"]}`,
	},
	{
		Name:             "desktop_key",
		Category:         CategoryDesktop,
		Description:      "X11 desktop press key",
		Schema:           `{"key": "Return"}`,
		ParametersSchema: `{"type":"object","properties":{"key":{"type":"string","description":"Key name to press"}},"required":["key"]}`,
	},

	// Orchestration
	{
		Name:             "delegate_to",
		Category:         CategoryOrchestration,
		Description:      "spawn a focused sub-agent with custom instructions and selected tools",
		Schema:           `{"task": "Search for Raspberry Pi prices", "instructions": "You are a price researcher. Search each store, record prices, summarize in a table.", "tools": ["mcp_call", "scrape", "search_web"]}`,
		Returns:          `Returns the sub-agent's output as an artifact. The sub-agent runs in an isolated context with only the tools you specify.`,
		ParametersSchema: `{"type":"object","properties":{"task":{"type":"string","description":"What the sub-agent should accomplish"},"instructions":{"type":"string","description":"Custom system prompt for this sub-agent. Write focused instructions that tell the sub-agent exactly how to approach the task."},"tools":{"type":"array","items":{"type":"string"},"description":"Tool names to give the sub-agent"},"max_rounds":{"type":"integer","description":"Max tool rounds (default: 15)"},"thinking_budget":{"type":"integer","description":"Max thinking tokens per turn (default: 2048)"},"temperature":{"type":"number","description":"Temperature for generation (default: 0.4)"}},"required":["task","instructions","tools"]}`,
	},
	{
		Name:             "delegate_async",
		Category:         CategoryOrchestration,
		Description:      "launch a sub-agent task in the background (returns immediately)",
		Schema:           `{"task": "Search for prices", "instructions": "You are a price researcher...", "tools": ["mcp_call", "scrape"], "task_id": "price-search"}`,
		Returns:          `Returns task_id immediately. Call collect_results to wait for all async tasks.`,
		ParametersSchema: `{"type":"object","properties":{"task":{"type":"string","description":"What the sub-agent should accomplish"},"instructions":{"type":"string","description":"Custom system prompt for this sub-agent"},"tools":{"type":"array","items":{"type":"string"},"description":"Tool names to give the sub-agent"},"task_id":{"type":"string","description":"Unique identifier for this async task"},"max_rounds":{"type":"integer","description":"Max tool rounds (default: 15)"},"thinking_budget":{"type":"integer","description":"Max thinking tokens per turn (default: 2048)"},"temperature":{"type":"number","description":"Temperature for generation (default: 0.4)"}},"required":["task","instructions","tools","task_id"]}`,
	},
	{
		Name:             "collect_results",
		Category:         CategoryOrchestration,
		Description:      "wait for all async delegates to complete and collect their results",
		Schema:           `{}`,
		Returns:          `Returns map of task_id → result for all pending async delegates.`,
		ParametersSchema: `{"type":"object","properties":{}}`,
	},
	{
		Name:             "create_plan",
		Category:         CategoryOrchestration,
		Description:      "create a step-by-step plan (pauses for user approval when plan-approval mode is on)",
		Schema:           `{"steps": ["Step 1 description", "Step 2 description"]}`,
		ParametersSchema: `{"type":"object","properties":{"steps":{"type":"array","items":{"type":"string"},"description":"List of step descriptions"}},"required":["steps"]}`,
	},
	{
		Name:             "update_plan",
		Category:         CategoryOrchestration,
		Description:      "mark a step as done/failed, or flag discovered work",
		Schema:           `{"step_index": 0, "status": "done", "note": "Found: price is $45", "discovered": false}`,
		ParametersSchema: `{"type":"object","properties":{"step_index":{"type":"integer","description":"Step index to update"},"status":{"type":"string","description":"New status: done, failed, or pending"},"note":{"type":"string","description":"Optional note about the result"},"discovered":{"type":"boolean","description":"Set true when new unlisted work is found (triggers approval in plan-approval mode)"}},"required":["step_index","status"]}`,
	},
	{
		Name:             "clarify",
		Category:         CategoryOrchestration,
		Description:      "ask the user clarifying questions before planning (max 5, plan-approval mode only)",
		Schema:           `{"questions": ["Which framework?", "What database?"]}`,
		ParametersSchema: `{"type":"object","properties":{"questions":{"type":"array","items":{"type":"string"},"description":"1-5 clarifying questions for the user","minItems":1,"maxItems":5}},"required":["questions"]}`,
	},
	{
		Name:             "synthesize",
		Category:         CategoryOrchestration,
		Description:      "present the final answer to the user",
		Schema:           `{"conclusion": "Here is the answer..."}`,
		ParametersSchema: `{"type":"object","properties":{"conclusion":{"type":"string","description":"Final answer or summary"}},"required":["conclusion"]}`,
	},

	// Meta
	{
		Name:             "update_memory",
		Category:         CategoryMeta,
		Description:      "save important information to project memory for future sessions",
		Schema:           `{"content": "Key finding: database uses PostgreSQL 15...", "section": "Project Facts"}`,
		Returns:          "Saves to MEMORY.md in the project directory. Persisted across sessions. Max 200 lines. Sections: User Preferences, Project Facts, Feedback, Reference.",
		ParametersSchema: `{"type":"object","properties":{"content":{"type":"string","description":"Information to save to project memory."},"section":{"type":"string","description":"Optional section to update: User Preferences, Project Facts, Feedback, Reference. Without this, replaces entire memory."}},"required":["content"]}`,
	},
	{
		Name:             "wait",
		Category:         CategoryMeta,
		Description:      "wait for a specified duration before proceeding",
		Schema:           `{"seconds": 2}`,
		Returns:          "Waits the specified number of seconds. Use after navigation or actions that need time to take effect.",
		ParametersSchema: `{"type":"object","properties":{"seconds":{"type":"integer","description":"Seconds to wait (1-30)"}},"required":["seconds"]}`,
	},
	{
		Name:             "yield_artifact",
		Category:         CategoryMeta,
		Description:      "signal task completion and return output to orchestrator",
		Schema:           `{"output": "Task completed. Summary of results..."}`,
		Returns:          "Call this when your assigned task is done.",
		ParametersSchema: `{"type":"object","properties":{"output":{"type":"string","description":"Task output or summary"}},"required":["output"]}`,
	},
	{
		Name:             "ask_user",
		Category:         CategoryMeta,
		Description:      "ask the user a clarifying question and wait for their response",
		Schema:           `{"question": "Which framework would you like to use: React or Vue?"}`,
		Returns:          "Returns the user's answer as text. Use this before starting complex tasks.",
		ParametersSchema: `{"type":"object","properties":{"question":{"type":"string","description":"Question to ask the user"}},"required":["question"]}`,
	},
}

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
		// The orchestrator IS the unified agent — has ALL tools.
		return []string{
			// Execution
			"bash",
			// File operations (Claude Code pattern)
			"file_read", "file_write", "file_edit", "file_grep", "file_glob",
			// Code intelligence
			"code_search",
			// Browser
			"search_web", "browse_to", "click_element", "type_text", "read_page", "observe", "scroll_page", "scrape",
			// MCP research server (direct access to all MCP tools)
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
delegate_to{"task":"Research golang concurrency patterns","instructions":"Search for golang concurrency patterns. For each pattern (goroutines, channels, select, sync.WaitGroup), find a code example and explain it. Cite sources.","tools":["mcp_call","scrape","search_web"]}
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
