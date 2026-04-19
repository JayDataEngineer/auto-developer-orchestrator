package llama

import (
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
// This is the single source of truth — prompts and validation all read from here.
type ToolSpec struct {
	Name        string       // Tool name used in <|tool_call|>call:NAME{args}<tool_call|>
	Category    ToolCategory // Grouping for persona whitelists
	Description string       // One-line description for prompts
	Schema      string       // JSON example showing args
	Returns     string       // What the tool returns (optional)
}

// allTools is the master registry. Every tool the system knows lives here.
var allTools = []ToolSpec{
	// Execution
	{
		Name:        "bash",
		Category:    CategoryExecution,
		Description: "run a shell command",
		Schema:      `{"command": "ls -la"}`,
		Returns:     "Runs in sandbox as root. Working dir: /sandbox/workspace",
	},

	// Browser
	{
		Name:        "search_web",
		Category:    CategoryBrowser,
		Description: "search Google and return results (ONE step)",
		Schema:      `{"query": "cats"}`,
		Returns:     "Returns search results. Use this for any search task.",
	},
	{
		Name:        "browse_to",
		Category:    CategoryBrowser,
		Description: "open a URL",
		Schema:      `{"url": "https://example.com"}`,
		Returns:     "Returns page with clickable elements.",
	},
	{
		Name:        "click_element",
		Category:    CategoryBrowser,
		Description: "click element by ID number",
		Schema:      `{"element": 5}`,
	},
	{
		Name:        "type_text",
		Category:    CategoryBrowser,
		Description: "type text into an element",
		Schema:      `{"element": 1, "text": "hello", "submit": true}`,
		Returns:     "submit:true presses Enter after typing.",
	},
	{
		Name:        "read_page",
		Category:    CategoryBrowser,
		Description: "re-read current page content",
		Schema:      `{}`,
		Returns:     "Use when you need to see the page again.",
	},

	// Desktop
	{
		Name:        "computer_use_enable",
		Category:    CategoryDesktop,
		Description: "start the desktop environment",
		Schema:      `{}`,
		Returns:     "Starts desktop if not running. Returns CDP port.",
	},
	{
		Name:        "computer_use_screenshot",
		Category:    CategoryDesktop,
		Description: "take screenshot",
		Schema:      `{} or {"describe": true}`,
		Returns:     "Takes screenshot. With describe:true, returns AI description.",
	},
	{
		Name:        "computer_use_snapshot",
		Category:    CategoryDesktop,
		Description: "get interactive elements with IDs",
		Schema:      `{}`,
		Returns:     "Returns page elements: [ID] <tag> \"text\"",
	},
	{
		Name:        "computer_use_act",
		Category:    CategoryDesktop,
		Description: "click, type, navigate, scroll",
		Schema:      `{"action": "navigate", "url": "https://example.com"}`,
		Returns:     "Actions: navigate, click, type, scroll",
	},
	{
		Name:        "desktop_screenshot",
		Category:    CategoryDesktop,
		Description: "X11 desktop screenshot",
		Schema:      `{}`,
	},
	{
		Name:        "desktop_click",
		Category:    CategoryDesktop,
		Description: "X11 desktop click at coordinates",
		Schema:      `{"x": 100, "y": 200}`,
	},
	{
		Name:        "desktop_type",
		Category:    CategoryDesktop,
		Description: "X11 desktop type text",
		Schema:      `{"text": "hello"}`,
	},
	{
		Name:        "desktop_key",
		Category:    CategoryDesktop,
		Description: "X11 desktop press key",
		Schema:      `{"key": "Return"}`,
	},

	// Orchestration
	{
		Name:        "delegate_to",
		Category:    CategoryOrchestration,
		Description: "assign a task to a sub-agent",
		Schema:      `{"persona": "web", "task": "Search for the price of a Raspberry Pi"}`,
		Returns:     `Personas: "web" (search/browse), "code" (bash/coding), "desktop" (browser automation)`,
	},
	{
		Name:        "create_plan",
		Category:    CategoryOrchestration,
		Description: "create a step-by-step plan",
		Schema:      `{"steps": ["Step 1 description", "Step 2 description"]}`,
	},
	{
		Name:        "update_plan",
		Category:    CategoryOrchestration,
		Description: "mark a step as done/failed",
		Schema:      `{"step_index": 0, "status": "done", "note": "Found: price is $45"}`,
	},
	{
		Name:        "synthesize",
		Category:    CategoryOrchestration,
		Description: "present the final answer to the user",
		Schema:      `{"conclusion": "Here is the answer..."}`,
	},

	// Meta
	{
		Name:        "yield_artifact",
		Category:    CategoryMeta,
		Description: "signal task completion and return output to orchestrator",
		Schema:      `{"output": "Task completed. Summary of results..."}`,
		Returns:     "Call this when your assigned task is done.",
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

// ToolsByCategory returns all tools in the given categories.
func ToolsByCategory(cats ...ToolCategory) []ToolSpec {
	want := make(map[ToolCategory]bool, len(cats))
	for _, c := range cats {
		want[c] = true
	}
	var specs []ToolSpec
	for _, t := range allTools {
		if want[t.Category] {
			specs = append(specs, t)
		}
	}
	return specs
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
// This replaces the hardcoded tool lists in persona.go.
func PersonaToolNames(t PersonaType) []string {
	switch t {
	case PersonaOrchestrator:
		return []string{"delegate_to", "create_plan", "update_plan", "synthesize"}
	case PersonaWeb:
		return []string{"search_web", "browse_to", "click_element", "type_text", "read_page", "bash", "yield_artifact"}
	case PersonaCode:
		return []string{"bash", "yield_artifact"}
	case PersonaDesktop:
		return []string{"computer_use_enable", "computer_use_screenshot", "computer_use_snapshot", "computer_use_act",
			"desktop_screenshot", "desktop_click", "desktop_type", "desktop_key", "bash", "yield_artifact"}
	default:
		return nil
	}
}

// PersonaToolSpecs returns ToolSpecs for a persona's tool whitelist.
func PersonaToolSpecs(t PersonaType) []ToolSpec {
	return ToolsByNames(PersonaToolNames(t))
}

// PersonaExamples returns few-shot examples for a persona type.
// Critical for the 26B model — it learns tool format from these.
func PersonaExamples(t PersonaType) []Example {
	switch t {
	case PersonaOrchestrator:
		return []Example{
			{
				Title: "Simple calculation task",
				Content: `create_plan{"steps":["Calculate 2+2 using bash"]}
delegate_to{"persona":"code","task":"Run: echo $((2+2))"}
update_plan{"step_index":0,"status":"done","note":"Result: 4"}
synthesize{"conclusion":"2+2 = 4"}`,
			},
			{
				Title: "Research task",
				Content: `create_plan{"steps":["Search for Raspberry Pi price","Summarize findings"]}
delegate_to{"persona":"web","task":"Search Google for the current price of a Raspberry Pi 5"}
update_plan{"step_index":0,"status":"done","note":"Found: $45"}
synthesize{"conclusion":"The Raspberry Pi 5 costs approximately $45."}`,
			},
		}
	case PersonaWeb:
		return []Example{
			{
				Title: "Search and browse",
				Content: `search_web{"query":"weather in Tokyo"}
→ Returns search results with links

browse_to{"url":"https://www.google.com"}
→ Returns elements like [6] <textarea> "q"

type_text{"element":6, "text":"cats", "submit":true}
→ Types "cats" and presses Enter

read_page{}
→ Returns updated page with results`,
			},
			{
				Title: "Click a link",
				Content: `read_page{} → see [3] <a> "Click here"
click_element{"element":3}`,
			},
		}
	case PersonaCode:
		return []Example{
			{
				Title: "Create and verify a file",
				Content: `bash{"command":"echo 'hello' > /sandbox/workspace/test.txt"}
bash{"command":"cat /sandbox/workspace/test.txt"}`,
			},
		}
	case PersonaDesktop:
		return []Example{
			{
				Title: "Enable desktop and navigate",
				Content: `computer_use_enable{}
computer_use_screenshot{}
computer_use_act{"action":"navigate","url":"https://example.com"}
computer_use_snapshot{}
→ Returns [3] <a> "More information"
computer_use_act{"action":"click","element":3}`,
			},
		}
	default:
		return nil
	}
}
