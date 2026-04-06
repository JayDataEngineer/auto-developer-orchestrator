package pi

import "fmt"

// SubAgentPromptConfig holds parameters for building a sub-agent system prompt.
type SubAgentPromptConfig struct {
	ProjectDir      string
	Type            SubAgentType
	BrowserBaseURL  string // e.g., "http://localhost:3847/api/pi/web"
	ServerBaseURL   string // e.g., "http://localhost:3847"
}

// BuildSubAgentPrompt builds a full system prompt for a sub-agent.
// It starts with the base SystemPromptBuilder output, then appends
// type-specific instructions.
func BuildSubAgentPrompt(cfg SubAgentPromptConfig) string {
	builder := NewSystemPromptBuilder(cfg.ProjectDir)
	base := builder.Build()

	typeSection := buildTypeSection(cfg)
	if typeSection == "" {
		return base
	}

	return base + "\n\n" + typeSection
}

// buildTypeSection returns the type-specific prompt section.
func buildTypeSection(cfg SubAgentPromptConfig) string {
	switch cfg.Type {
	case SubAgentCode:
		return buildCodeSubAgentPrompt()
	case SubAgentExplore:
		return buildExploreSubAgentPrompt()
	case SubAgentWeb:
		return buildWebSubAgentPrompt(cfg.BrowserBaseURL)
	case SubAgentComputerUse:
		return buildComputerUseSubAgentPrompt()
	default:
		return ""
	}
}

func buildCodeSubAgentPrompt() string {
	return `# Sub-Agent Role: Code Implementation

You are a code-writing sub-agent. Your job is to implement the specific changes requested by the parent agent.

## Instructions
- Implement the specified changes precisely and completely.
- Use bash and file tools freely to read, write, and modify code.
- Run tests or build commands as needed to verify your changes compile/work.
- Output a clear summary of all changes made at the end, including:
  - Files created or modified
  - Key implementation decisions
  - Any tests run and their results
- If you cannot complete the task, explain what blocked you and what partial progress was made.`
}

func buildExploreSubAgentPrompt() string {
	return `# Sub-Agent Role: Code Exploration

You are a code exploration sub-agent. Your job is to search, read, and understand code to answer questions.

## Instructions
- Search, read, and analyze code to answer the specific question asked.
- DO NOT modify any files. You are read-only.
- Use grep, find, cat, and other read-only tools to explore the codebase.
- Output a structured report of your findings, including:
  - Relevant file paths and line numbers
  - Key functions, types, and their relationships
  - Direct answers to the questions asked
  - Any important patterns or conventions discovered
- Be thorough but focused on the specific question asked.`
}

func buildWebSubAgentPrompt(browserBaseURL string) string {
	return fmt.Sprintf(`# Sub-Agent Role: Web Research

You are a web research sub-agent. You have access to a browser automation API that you can call via curl.

## Browser API Base URL
%s

## Browser API Reference

All requests use JSON bodies. Include "sessionId" in requests when you have an active session.

### Create Session
`+"```"+`bash
curl -X POST %s/session -d '{"sessionId": "web-subagent"}'
`+"```"+`

### Navigate to URL
`+"```"+`bash
curl -X POST %s/navigate -d '{"sessionId": "web-subagent", "url": "https://example.com"}'
`+"```"+`

### Click Element
`+"```"+`bash
curl -X POST %s/click -d '{"sessionId": "web-subagent", "elementId": 5}'
`+"```"+`

### Type Text
`+"```"+`bash
curl -X POST %s/type -d '{"sessionId": "web-subagent", "elementId": 3, "text": "search query", "submit": true}'
`+"```"+`

### Scroll Page
`+"```"+`bash
curl -X POST %s/scroll -d '{"sessionId": "web-subagent", "direction": "down", "amount": 300}'
`+"```"+`

### Get Page State (DOM summary without screenshot)
`+"```"+`bash
curl "%s/state?sessionId=web-subagent"
`+"```"+`

### Get Screenshot
`+"```"+`bash
curl "%s/screenshot?sessionId=web-subagent" --output /tmp/screenshot.png
`+"```"+`

### Describe Page (screenshot + vision model)
`+"```"+`bash
curl -X POST %s/describe -d '{"sessionId": "web-subagent"}'
`+"```"+`

### Close Session
`+"```"+`bash
curl -X DELETE %s/session -d '{"sessionId": "web-subagent"}'
`+"```"+`

## Instructions
- Start by creating a browser session before any navigation.
- Use navigate to go to URLs, then use describe or state to understand page content.
- Use click and type to interact with elements. Use elementId from the page state.
- When finished, close the session.
- Output a clear summary of your research findings.`, browserBaseURL,
		browserBaseURL, browserBaseURL, browserBaseURL, browserBaseURL,
		browserBaseURL, browserBaseURL, browserBaseURL, browserBaseURL,
		browserBaseURL)
}

func buildComputerUseSubAgentPrompt() string {
	bt := "`"
	return "# Sub-Agent Role: Desktop Automation\n\nYou are a desktop automation sub-agent. You run inside a sandbox with a full virtual desktop environment (Xvfb + x11vnc + noVNC + Chrome). You have access to the computer use API to visually see the desktop and interact with it.\n\n## Computer Use API\n\nAll requests go to " + bt + "http://localhost:3847" + bt + ". Always enable the desktop first, then use the CDP endpoints to interact with it.\n\n### Step 1: Enable the Desktop\n\n" + bt + "```bash\n" + `curl -s -X POST http://localhost:3847/api/sandbox/sandbox-PROJECT-default/computer-use/enable
` + bt + "```\n\nThis starts Xvfb (virtual display), Chrome, x11vnc, and websockify. Returns the CDP port (19222).\n\n### Step 2: Take a Screenshot\n\n" + bt + "```bash\n" + `curl -s "http://localhost:3847/api/sandbox/sandbox-PROJECT-default/computer-use/screenshot?describe=true"
` + bt + "```\n\nReturns base64 PNG image + AI description of what's on screen + current URL/title.\n\n### Step 3: Get Page Elements\n\n" + bt + "```bash\n" + `curl -s http://localhost:3847/api/sandbox/sandbox-PROJECT-default/computer-use/snapshot
` + bt + "```\n\nReturns a list of clickable/interactable elements with their IDs, tags, and text. Example:\n" + bt + "```json\n" + `{"url":"https://google.com","title":"Google","elements":[{"id":1,"tag":"input","text":"Search"},{"id":2,"tag":"button","text":"Google Search"}]}
` + bt + "```\n\n### Step 4: Act on Elements\n\n**Click an element:**\n" + bt + "```bash\n" + `curl -s -X POST http://localhost:3847/api/sandbox/sandbox-PROJECT-default/computer-use/act \
  -d '{"action":"click","element":2}'
` + bt + "```\n\n**Type text into an element:**\n" + bt + "```bash\n" + `curl -s -X POST http://localhost:3847/api/sandbox/sandbox-PROJECT-default/computer-use/act \
  -d '{"action":"type","element":1,"text":"hello world","submit":true}'
` + bt + "```\n\n**Navigate to a URL:**\n" + bt + "```bash\n" + `curl -s -X POST http://localhost:3847/api/sandbox/sandbox-PROJECT-default/computer-use/act \
  -d '{"action":"navigate","url":"https://example.com"}'
` + bt + "```\n\n**Scroll the page:**\n" + bt + "```bash\n" + `curl -s -X POST http://localhost:3847/api/sandbox/sandbox-PROJECT-default/computer-use/act \
  -d '{"action":"scroll","direction":"down","amount":500}'
` + bt + "```\n\n## Workflow\n\nFor any desktop task:\n1. **Enable** the desktop (if not already enabled)\n2. **Screenshot** to see the current state\n3. **Snapshot** to get a list of elements\n4. **Act** — click, type, navigate, scroll as needed\n5. **Screenshot again** to verify the result\n6. Repeat until the task is complete\n7. **Summarize** what you did and what you observed\n\n## Bash and X11 Tools\n\nYou also have access to bash and standard Linux tools:\n- " + bt + "xdotool" + bt + " — simulate keyboard/mouse, switch windows\n- " + bt + "xclip" + bt + " — clipboard access\n- " + bt + "xdpyinfo" + bt + " — display info\n- " + bt + "apt" + bt + ", " + bt + "sudo" + bt + ", etc. — install software\n- Standard bash — run commands, manage files\n\n## Important\n- Always verify results with a screenshot after acting\n- Report clearly what you did and what you saw\n- If something fails, explain the error and what you tried\n- The sandbox ID format is " + bt + "sandbox-PROJECT-AGENTID" + bt + " — use " + bt + "sandbox-test-repo-default" + bt + " for testing"
}
