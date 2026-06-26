// skills_tool.go exposes list_skills + load_skill as MCP tools. Skills are
// discovered from <project>/skills/ — see backend/internal/skills/.
//
// The model calls list_skills to see what specialized guidance exists for
// the current project, then load_skill to actually read a skill's body.
// This is the progressive-disclosure pattern: cheap list, on-demand load.

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/skills"
)

// ListSkillsTool returns every SKILL.md found under <project>/skills/.
// One call, no arguments, small response (metadata only — Content is
// omitted; use load_skill to read the body).
type ListSkillsTool struct {
	root string
}

// NewListSkillsTool binds the tool to a project root. The root is read
// fresh on every Execute call, so newly added skill files show up without
// a server restart.
func NewListSkillsTool(projectRoot string) *ListSkillsTool {
	return &ListSkillsTool{root: projectRoot}
}

func (t *ListSkillsTool) Name() string { return "list_skills" }

func (t *ListSkillsTool) Description() string {
	return "List SKILL.md files under the project's skills/ directory. " +
		"Each skill is operator-authored markdown with model-facing instructions " +
		"(debugging recipes, codebase conventions, domain knowledge). " +
		"Call this when starting work on a project to see what specialized " +
		"guidance is available; then call load_skill to read the ones that apply."
}

func (t *ListSkillsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`)
}

func (t *ListSkillsTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	ss, err := skills.Discover(t.root)
	if err != nil {
		return nil, fmt.Errorf("discover skills: %w", err)
	}
	items := make([]map[string]any, len(ss))
	for i, s := range ss {
		items[i] = map[string]any{
			"name":        s.Name,
			"description": s.Description,
			"path":        s.Path,
		}
	}
	return map[string]any{
		"skills": items,
		"count":  len(items),
	}, nil
}

// LoadSkillTool reads one SKILL.md body by name. Use after list_skills to
// pull a skill's full markdown into context.
type LoadSkillTool struct {
	root string
}

func NewLoadSkillTool(projectRoot string) *LoadSkillTool {
	return &LoadSkillTool{root: projectRoot}
}

func (t *LoadSkillTool) Name() string { return "load_skill" }

func (t *LoadSkillTool) Description() string {
	return "Load one skill's full markdown body by name (use list_skills " +
		"first to discover names). Returns name, description, source path, " +
		"and the markdown content. Read the content carefully — it carries " +
		"operator-authored instructions specific to this project."
}

func (t *LoadSkillTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Skill name (the 'name' field from list_skills)"
			}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)
}

func (t *LoadSkillTool) Execute(_ context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("load_skill", "missing required parameter 'name'")
	}
	s, err := skills.Load(t.root, name)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"name":        s.Name,
		"description": s.Description,
		"path":        s.Path,
		"content":     s.Content,
	}, nil
}
