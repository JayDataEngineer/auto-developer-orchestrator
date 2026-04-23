package llama

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
)

// CodeSearchOps provides language-aware code search using ripgrep.
// Pragmatic approach: pattern-based heuristics instead of full LSP server.
// Gives 80% of the value at 20% of the cost.
type CodeSearchOps struct {
	fileOps *SandboxFileOps
}

// NewCodeSearchOps creates a code search helper backed by SandboxFileOps.
func NewCodeSearchOps(fileOps *SandboxFileOps) *CodeSearchOps {
	return &CodeSearchOps{fileOps: fileOps}
}

// Search performs a code search operation.
// Operations: find_references, find_definition, list_symbols, hover
func (c *CodeSearchOps) Search(ctx context.Context, operation, symbol, searchPath, fileType string) (string, error) {
	if searchPath == "" {
		searchPath = "/sandbox/workspace"
	}
	if err := sandbox.ValidatePath(searchPath); err != nil {
		return "", err
	}

	// Ensure ripgrep is available
	if err := c.fileOps.ensureRipgrep(ctx); err != nil {
		return "", fmt.Errorf("ripgrep not available: %w", err)
	}

	switch operation {
	case "find_references":
		return c.findReferences(ctx, symbol, searchPath, fileType)
	case "find_definition":
		return c.findDefinition(ctx, symbol, searchPath, fileType)
	case "list_symbols":
		return c.listSymbols(ctx, searchPath, fileType)
	case "hover":
		return c.hover(ctx, symbol, searchPath, fileType)
	default:
		return "", fmt.Errorf("unknown operation: %s (use: find_references, find_definition, list_symbols, hover)", operation)
	}
}

// findReferences searches for all usages of a symbol.
func (c *CodeSearchOps) findReferences(ctx context.Context, symbol, path, fileType string) (string, error) {
	glob := fileTypeGlob(fileType)
	// Use word boundary matching to avoid partial matches
	pattern := fmt.Sprintf("\\b%s\\b", symbolRegex(symbol))
	return c.fileOps.Grep(ctx, pattern, path, glob, "content", 2, false, 50)
}

// findDefinition searches for symbol definitions.
func (c *CodeSearchOps) findDefinition(ctx context.Context, symbol, path, fileType string) (string, error) {
	// Build language-specific definition patterns
	var patterns []string
	switch fileType {
	case "go":
		patterns = []string{
			fmt.Sprintf("func %s\\b", symbol),
			fmt.Sprintf("func \\([^)]+\\) %s\\b", symbol),
			fmt.Sprintf("type %s\\b", symbol),
			fmt.Sprintf("var %s\\b", symbol),
			fmt.Sprintf("const %s\\b", symbol),
			fmt.Sprintf("interface %s\\b", symbol),
		}
	case "py", "python":
		patterns = []string{
			fmt.Sprintf("def %s\\b", symbol),
			fmt.Sprintf("class %s\\b", symbol),
		}
	case "js", "ts", "tsx", "jsx":
		patterns = []string{
			fmt.Sprintf("function %s\\b", symbol),
			fmt.Sprintf("const %s\\b", symbol),
			fmt.Sprintf("let %s\\b", symbol),
			fmt.Sprintf("class %s\\b", symbol),
			fmt.Sprintf("export.*%s\\b", symbol),
		}
	default:
		// Generic: look for common definition patterns
		patterns = []string{
			fmt.Sprintf("(func|def|class|type|interface|const|var|let|function)\\s+%s\\b", symbol),
		}
	}

	combined := strings.Join(patterns, "|")
	glob := fileTypeGlob(fileType)
	return c.fileOps.Grep(ctx, combined, path, glob, "content", 3, false, 20)
}

// listSymbols lists all symbols in the given path.
func (c *CodeSearchOps) listSymbols(ctx context.Context, path, fileType string) (string, error) {
	var pattern string
	switch fileType {
	case "go":
		pattern = "^[[:space:]]*(func |type |var |const |interface )[[:alpha:]]"
	case "py", "python":
		pattern = "^[[:space:]]*(def |class )[[:alpha:]]"
	case "js", "ts", "tsx", "jsx":
		pattern = "^[[:space:]]*(function |const |let |class |export |async function )[[:alpha:]]"
	default:
		pattern = "^[[:space:]]*(func |def |class |type |interface |const |var |let |function |export )[[:alpha:]]"
	}

	glob := fileTypeGlob(fileType)
	return c.fileOps.Grep(ctx, pattern, path, glob, "content", 0, false, 100)
}

// hover returns documentation for a symbol using language-specific tools.
func (c *CodeSearchOps) hover(ctx context.Context, symbol, path, fileType string) (string, error) {
	switch fileType {
	case "go":
		// Use go doc for Go symbols
		cmd := fmt.Sprintf("cd /sandbox/workspace && go doc %s 2>/dev/null || echo 'No documentation found'", symbol)
		output, err := c.fileOps.exec(ctx, cmd)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(output) == "No documentation found" {
			// Fall back to grep with context
			return c.findDefinition(ctx, symbol, path, fileType)
		}
		return output, nil
	case "py", "python":
		cmd := fmt.Sprintf("cd /sandbox/workspace && python3 -c \"import pydoc; print(pydoc.render_doc('%s', renderer=pydoc.plaintext))\" 2>/dev/null || echo 'No documentation found'", symbol)
		output, err := c.fileOps.exec(ctx, cmd)
		if err != nil {
			return c.findDefinition(ctx, symbol, path, fileType)
		}
		return output, nil
	default:
		// For other languages, just find the definition with context
		return c.findDefinition(ctx, symbol, path, fileType)
	}
}

// fileTypeGlob converts a file type string to a ripgrep glob pattern.
func fileTypeGlob(fileType string) string {
	switch fileType {
	case "go":
		return "*.go"
	case "py", "python":
		return "*.py"
	case "js":
		return "*.js"
	case "ts":
		return "*.ts"
	case "tsx":
		return "*.tsx"
	case "jsx":
		return "*.jsx"
	case "rs", "rust":
		return "*.rs"
	case "java":
		return "*.java"
	case "rb", "ruby":
		return "*.rb"
	default:
		return ""
	}
}

// symbolRegex escapes a symbol name for use in regex.
func symbolRegex(s string) string {
	s = strings.ReplaceAll(s, ".", `\.`)
	s = strings.ReplaceAll(s, "*", `\*`)
	s = strings.ReplaceAll(s, "(", `\(`)
	s = strings.ReplaceAll(s, ")", `\)`)
	return s
}
