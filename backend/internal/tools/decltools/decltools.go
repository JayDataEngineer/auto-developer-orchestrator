// Package decltools turns YAML tool declarations into core.Tool instances.
//
// Each DeclarativeTool specifies a command template with {{param}} placeholders
// and a parameter list. The factory substitutes placeholders with shell-quoted
// values (single-quote-and-escape, matching adapters.shQ) and runs the command
// via bash.Executor — the same interface used by the bash tool itself.
//
// Why: before Phase 4, every small wrapper around a script needed its own Go
// file (struct + Name + Description + Schema + Execute + registration).
// Declarative tools turn that 80-LOC-per-tool pattern into a 10-line YAML entry
// inside the capability that owns the tool. See pux-declarative-stack.md RFC
// axis 2.
package decltools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
)

// Tool is a core.Tool backed by a DeclarativeTool + bash.Executor.
type Tool struct {
	dt   common.DeclarativeTool
	exec bash.Executor
}

// Build turns a single DeclarativeTool declaration into a core.Tool.
// The executor is the same bash.Executor used by the bash tool — caller picks
// host vs sandbox per the newBashTool pattern at orchestrator.go:114.
func Build(dt common.DeclarativeTool, exec bash.Executor) core.Tool {
	return &Tool{dt: dt, exec: exec}
}

// BuildAll walks every loaded tool package, picks up DeclTools from the
// active implementation (or legacy top-level if no implementations[]), and
// returns a slice of tools ready to drop into a ToolRegistry.
//
// Duplicate names are skipped with a warning — the first definition wins.
// This matches how the global ToolRegistry already handles collisions
// (NewToolRegistry silently lets later registrants overwrite earlier ones;
// we log instead to surface the conflict).
func BuildAll(pkgs map[string]*common.ToolPackage, exec bash.Executor) []core.Tool {
	if exec == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []core.Tool
	for capName, pkg := range pkgs {
		decls := declToolsForPackage(pkg)
		for _, dt := range decls {
			if seen[dt.Name] {
				fmt.Printf("decltools: skipping duplicate %q (already registered)\n", dt.Name)
				continue
			}
			seen[dt.Name] = true
			out = append(out, Build(dt, exec))
			fmt.Printf("decltools: registered %q from capability=%s\n", dt.Name, capName)
		}
	}
	return out
}

// declToolsForPackage picks the active implementation's DeclTools when the
// resolver set one, else returns nil. Legacy tool packages (no
// implementations[]) cannot carry decltools today — they're a flat list of
// tool names, not declarations.
func declToolsForPackage(pkg *common.ToolPackage) []common.DeclarativeTool {
	if pkg == nil {
		return nil
	}
	if pkg.ActiveImpl != nil {
		return pkg.ActiveImpl.DeclTools
	}
	return nil
}

func (t *Tool) Name() string { return t.dt.Name }

func (t *Tool) Description() string { return t.dt.Description }

func (t *Tool) Schema() json.RawMessage {
	return json.RawMessage(buildSchema(t.dt.Parameters))
}

// TimeoutHint implements core.ToolMetadata. The agent loop checks for this
// interface and uses the hint instead of the default tool timeout.
// Returns 0 when dt.Timeout is 0 — meaning "use the default."
func (t *Tool) TimeoutHint() time.Duration {
	if t.dt.Timeout <= 0 {
		return 0
	}
	return time.Duration(t.dt.Timeout) * time.Second
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Validate + apply defaults before substitution.
	resolved, err := resolveArgs(t.dt.Parameters, args)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	command, err := substitute(t.dt.Command, resolved)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	// Honor per-tool timeout. The agent loop also wraps Execute with a
	// deadline based on TimeoutHint, but that's belt-and-suspenders —
	// nested context ensures we kill runaway commands even if a future
	// caller forgets to consult TimeoutHint.
	execCtx := ctx
	if t.dt.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(t.dt.Timeout)*time.Second)
		defer cancel()
	}

	out, err := t.exec.Exec(execCtx, command)
	if err != nil {
		return map[string]any{
			"error":  err.Error(),
			"stdout": truncate.Tail(out, truncate.FileMaxLines, truncate.BashMaxChars).Content,
		}, nil
	}
	return map[string]any{
		"stdout": truncate.Tail(out, truncate.FileMaxLines, truncate.BashMaxChars).Content,
	}, nil
}

// resolveArgs validates required params, applies defaults, and coerces the
// incoming args map into a string-keyed map of values ready for substitution.
// Missing required params return an error naming the offender.
func resolveArgs(params []common.ToolParam, args map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(params))
	for _, p := range params {
		val, present := args[p.Name]
		if !present {
			if p.Required {
				return nil, fmt.Errorf("missing required parameter %q", p.Name)
			}
			if p.Default != nil {
				val = p.Default
			} else {
				// Optional + no default → skip substitution entirely so
				// the template can decide what to do with an empty value.
				continue
			}
		}
		out[p.Name] = val
	}
	return out, nil
}

// substitute replaces {{name}} with shell-quoted string representations of
// the corresponding values. Unknown placeholders are left in place — this
// surfaces typos in the command template at call time (the script will see
// the literal {{foo}} and complain).
func substitute(template string, vals map[string]any) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(template) {
		if i+1 < len(template) && template[i] == '{' && template[i+1] == '{' {
			end := strings.Index(template[i+2:], "}}")
			if end < 0 {
				// Unbalanced opening — emit literally, let the shell fail.
				b.WriteString(template[i:])
				break
			}
			name := strings.TrimSpace(template[i+2 : i+2+end])
			val, ok := vals[name]
			if !ok {
				// Unknown placeholder — leave the literal {{name}} in.
				b.WriteString(template[i : i+2+end+2])
			} else {
				b.WriteString(shellQuote(fmt.Sprint(val)))
			}
			i = i + 2 + end + 2
			continue
		}
		b.WriteByte(template[i])
		i++
	}
	return b.String(), nil
}

// shellQuote wraps a value in single quotes, escaping any embedded quotes.
// Matches adapters.shQ so the same input produces the same command line in
// both code paths. A value like `'; rm -rf /; '` becomes `''\'''; rm -rf /; ''\'''`
// — the shell sees a single quoted string with literal text, no command
// execution.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildSchema produces a JSON-schema object matching the declared parameters.
// Each parameter becomes a typed property; required ones land in the
// "required" array. Types map 1:1 from ToolParam.Type to JSON schema types.
func buildSchema(params []common.ToolParam) string {
	var propSB strings.Builder
	propSB.WriteString("{\n  \"type\": \"object\",\n  \"properties\": {")
	for i, p := range params {
		if i > 0 {
			propSB.WriteString(",")
		}
		fmt.Fprintf(&propSB, "\n    %q: {\"type\": %q, \"description\": %q}",
			p.Name, jsonSchemaType(p.Type), p.Description)
	}
	propSB.WriteString("\n  }")
	required := []string{}
	for _, p := range params {
		if p.Required {
			required = append(required, p.Name)
		}
	}
	if len(required) > 0 {
		raw, _ := json.Marshal(required)
		fmt.Fprintf(&propSB, ",\n  \"required\": %s", string(raw))
	}
	propSB.WriteString("\n}")
	return propSB.String()
}

// jsonSchemaType normalizes the declared type. Unknown types default to
// "string" — safer than emitting malformed schema.
func jsonSchemaType(t string) string {
	switch t {
	case "string", "integer", "number", "boolean", "array", "object":
		return t
	default:
		return "string"
	}
}
