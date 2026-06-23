// Package secrets exposes org-scoped credential management tools.
//
// The model never sees secret values directly. Instead:
//   - list_secrets returns domains + keys (no values)
//   - inject_secret returns a <secret>domain.key</secret> placeholder
//
// The bash tool resolves the placeholder to the real value before exec.
// Real values are scrubbed from bash stdout by sensitive.ScrubText.
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
)

// ListTool lists available secret domains and keys (no values).
type ListTool struct {
	store *sensitive.Store
}

func NewListTool(store *sensitive.Store) *ListTool { return &ListTool{store: store} }

func (t *ListTool) Name() string { return "list_secrets" }
func (t *ListTool) Description() string {
	return "List available secrets (domains + keys only, no values). Use this to discover what credentials are available, then call inject_secret to get a placeholder for use in bash commands. Never attempt to read secret values from disk (.env files, credentials files) — they are blocked."
}
func (t *ListTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (t *ListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return map[string]any{"domains": []string{}, "note": "no secret store configured"}, nil
	}
	domains := t.store.Domains()
	out := make(map[string][]string, len(domains))
	for _, d := range domains {
		out[d] = t.store.Keys(d)
	}
	return map[string]any{
		"domains": out,
		"usage":   "Call inject_secret(domain, key) to get a placeholder for bash commands. Example: curl -H \"Authorization: Bearer <secret>openrouter.api_key</secret>\" ...",
	}, nil
}

// InjectTool returns a <secret>domain.key</secret> placeholder for use in bash.
type InjectTool struct {
	store *sensitive.Store
}

func NewInjectTool(store *sensitive.Store) *InjectTool { return &InjectTool{store: store} }

func (t *InjectTool) Name() string { return "inject_secret" }
func (t *InjectTool) Description() string {
	return "Get a <secret>domain.key</secret> placeholder for a credential. Use this placeholder verbatim in bash commands — the bash executor resolves it to the real value at execution time. The real value never appears in your context. Example: curl -H \"Authorization: Bearer <secret>openrouter.api_key</secret>\" https://api.openrouter.ai/..."
}
func (t *InjectTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"domain": {"type": "string", "description": "Secret domain (use list_secrets to see available domains)"},
			"key":    {"type": "string", "description": "Secret key under the domain"}
		},
		"required": ["domain", "key"]
	}`)
}

func (t *InjectTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	domain, _ := args["domain"].(string)
	key, _ := args["key"].(string)
	if domain == "" || key == "" {
		return nil, core.NewToolError("inject_secret", "domain and key are required")
	}
	if t.store == nil {
		return nil, core.NewToolError("inject_secret", "no secret store configured")
	}

	// Validate that the secret exists — don't leak whether the value is empty
	keys := t.store.Keys(domain)
	found := false
	for _, k := range keys {
		if k == key {
			found = true
			break
		}
	}
	if !found {
		availDomains := t.store.Domains()
		domainList := fmt.Sprintf("(available: %v)", availDomains)
		return map[string]any{
			"error":     fmt.Sprintf("secret %s.%s not found %s", domain, key, domainList),
			"available": t.store.Keys(domain),
		}, nil
	}

	placeholder := fmt.Sprintf("<secret>%s.%s</secret>", domain, key)
	return map[string]any{
		"placeholder": placeholder,
		"usage":       "Place this verbatim in your bash command. The bash executor resolves it to the real value when the command runs.",
		"warning":     "Do not echo, cat, or print the placeholder — it is for command use only.",
	}, nil
}

// AllTools returns the full set of secrets tools.
func AllTools(store *sensitive.Store) []core.Tool {
	if store == nil {
		return nil
	}
	return []core.Tool{
		NewListTool(store),
		NewInjectTool(store),
	}
}

// SortedKeys returns keys sorted for deterministic output.
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
