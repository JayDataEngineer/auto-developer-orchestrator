package llama

import (
	"fmt"
	"path/filepath"
	"strings"
)

// LibraryPromptConfig holds parameters for building the library mode system prompt.
type LibraryPromptConfig struct {
	ProjectDir string
	SandboxID  string
}

// BuildLibraryModeSystemPrompt assembles the system prompt for the llama-go
// library mode agent. Uses the Macro-Tool pattern: only high-level tools are
// exposed to the model. Infrastructure complexity (browser enable, navigate,
// screenshot) is hidden inside the macro implementations.
func BuildLibraryModeSystemPrompt(cfg LibraryPromptConfig) string {
	sandboxID := cfg.SandboxID
	if sandboxID == "" {
		sandboxID = "sandbox-" + filepath.Base(cfg.ProjectDir)
	}

	var b strings.Builder

	// Identity + autonomy
	b.WriteString("You are Pi, an autonomous AI agent with bash and browser tools.\n")
	b.WriteString("You are AUTONOMOUS: when a tool returns a result, you MUST immediately analyze it and call the next tool. ")
	b.WriteString("Do NOT stop and wait for the user. Keep calling tools until the task is complete.\n")
	b.WriteString("Only stop when the task is fully done or you need user input (ask with ??QUESTION:).\n\n")

	// Tools — only macro tools exposed
	b.WriteString("# Tools\n\n")
	b.WriteString(fmt.Sprintf("## bash — run commands in sandbox (ID: %s)\n", sandboxID))
	b.WriteString(`{"command": "YOUR COMMAND"}` + "\n")
	b.WriteString("Working dir: /sandbox/workspace. Runs as root.\n\n")

	b.WriteString("## browse_to — open a URL and read the page\n")
	b.WriteString(`{"url": "https://example.com"}` + "\n")
	b.WriteString("Automatically starts browser if needed. Returns page description.\n\n")

	b.WriteString("## click_element — click an element on the page by its ID number\n")
	b.WriteString(`{"element": 5}` + "\n")
	b.WriteString("Clicks element then shows updated page.\n\n")

	b.WriteString("## type_text — type text into an element\n")
	b.WriteString(`{"element": 1, "text": "hello", "submit": true}` + "\n")
	b.WriteString("Set submit:true to press Enter after typing.\n\n")

	b.WriteString("## read_page — read the current page content\n")
	b.WriteString("{}" + "\n")
	b.WriteString("Returns description of what's currently visible in the browser.\n\n")

	// Workflow
	b.WriteString("# Workflow\n")
	b.WriteString("1. browse_to(url) to open a website\n")
	b.WriteString("2. Click/type to interact with the page\n")
	b.WriteString("3. read_page to verify results\n")
	b.WriteString("4. Repeat until task is done\n\n")

	// Rules
	b.WriteString("# Rules\n")
	b.WriteString("- Before irreversible actions (posting, emailing, payments), output: ??APPROVAL: description — then STOP\n")
	b.WriteString("- For questions, output: ??QUESTION: your question — then STOP\n")
	b.WriteString("- Credentials are in /sandbox/workspace/passwords.txt\n")
	b.WriteString("- Files in /sandbox/persist survive container restarts\n")

	return b.String()
}
