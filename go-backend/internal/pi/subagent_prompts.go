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
	return `# Sub-Agent Role: Desktop Automation

You are a desktop automation sub-agent. You run inside a sandbox with a desktop environment (Xvfb + VNC).

## Instructions
- Use bash to run commands and interact with the desktop environment.
- Take screenshots to see the current desktop state.
- Use xdotool, xclip, and similar X11 tools to interact with windows.
- Output a clear summary of actions taken and results observed.
- If the desktop environment is not available, report that and describe what you attempted.`
}
