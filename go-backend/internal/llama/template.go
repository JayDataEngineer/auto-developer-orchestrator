package llama

import (
	"bytes"
	"embed"
	"fmt"
	"sync"
	"text/template"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

// tmplCache holds parsed templates, lazily initialized.
var (
	tmplOnce  sync.Once
	tmplCache *template.Template
)

// PromptData is passed to all prompt templates.
type PromptData struct {
	SandboxID     string
	Tools         string // Pre-rendered tool reference block from FormatToolList
	MCPReference  string // MCP tool parameter reference for delegation (NOT callable tools)
	Identity      string // Identity + rules block (for monolithic template)
	Examples      []Example
}

// Example is a few-shot example for the model.
type Example struct {
	Title   string
	Content string
}

// GoalNudgeData is passed to the goal_nudge template.
type GoalNudgeData struct {
	Round         int
	MaxRounds     int
	StepsLeft     int
	BudgetWarning bool
}

// getTemplates parses and caches all .tmpl files from the embedded FS.
func getTemplates() *template.Template {
	tmplOnce.Do(func() {
		var err error
		tmplCache, err = template.ParseFS(promptFS, "prompts/*.tmpl")
		if err != nil {
			panic(fmt.Sprintf("failed to parse prompt templates: %v", err))
		}
	})
	return tmplCache
}

// tmplName converts a short name like "web" to the template lookup name "web.tmpl".
// ParseFS uses the base filename (without directory) as the template name.
func tmplName(name string) string {
	return name + ".tmpl"
}

// RenderTemplate executes a named template with the given data.
// Name is the short filename without path/extension, e.g. "web", "code", "orchestrator".
func RenderTemplate(name string, data interface{}) (string, error) {
	tmpl := getTemplates()
	fullName := tmplName(name)
	t := tmpl.Lookup(fullName)
	if t == nil {
		return "", fmt.Errorf("template %q not found", fullName)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template %q execution failed: %w", fullName, err)
	}
	return buf.String(), nil
}
