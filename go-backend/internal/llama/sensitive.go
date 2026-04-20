package llama

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// CredentialStore manages domain-scoped credentials with placeholder injection.
// The model uses <secret>domain.key</secret> placeholders in its tool arguments.
// The executor resolves them to real values before execution and redacts them
// from results before feeding back to the model.
type CredentialStore struct {
	mu    sync.RWMutex
	creds map[string]map[string]string // domain → {key: value}
}

// NewCredentialStore creates an empty credential store.
func NewCredentialStore() *CredentialStore {
	return &CredentialStore{
		creds: make(map[string]map[string]string),
	}
}

// Set adds a credential for a domain.
func (c *CredentialStore) Set(domain, key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.creds[domain] == nil {
		c.creds[domain] = make(map[string]string)
	}
	c.creds[domain][key] = value
}

// Domains returns the list of configured domains.
func (c *CredentialStore) Domains() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	domains := make([]string, 0, len(c.creds))
	for d := range c.creds {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	return domains
}

// Keys returns the keys for a domain.
func (c *CredentialStore) Keys(domain string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.creds[domain] == nil {
		return nil
	}
	keys := make([]string, 0, len(c.creds[domain]))
	for k := range c.creds[domain] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Resolve replaces <secret>domain.key</secret> placeholders in args with actual values.
// Only resolves string values in the args map (including nested maps).
func (c *CredentialStore) Resolve(args map[string]interface{}) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	resolved := make(map[string]interface{}, len(args))
	for k, v := range args {
		resolved[k] = c.resolveValue(v)
	}
	return resolved
}

// resolveValue recursively resolves placeholders in a value.
func (c *CredentialStore) resolveValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return c.resolveString(val)
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			result[k] = c.resolveValue(v2)
		}
		return result
	default:
		return v
	}
}

// resolveString replaces all <secret>domain.key</secret> with actual values.
func (c *CredentialStore) resolveString(s string) string {
	for domain, keys := range c.creds {
		for key, value := range keys {
			placeholder := fmt.Sprintf("<secret>%s.%s</secret>", domain, key)
			s = strings.ReplaceAll(s, placeholder, value)
		}
	}
	return s
}

// Redact replaces actual credential values in text with <secret>domain.key</secret> placeholders.
// Sorted by value length (longest first) to prevent partial redaction.
func (c *CredentialStore) Redact(text string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Build sorted list of (value, placeholder) pairs, longest value first
	type replacement struct {
		value       string
		placeholder string
	}
	var replacements []replacement
	for domain, keys := range c.creds {
		for key, value := range keys {
			if value == "" {
				continue
			}
			replacements = append(replacements, replacement{
				value:       value,
				placeholder: fmt.Sprintf("<secret>%s.%s</secret>", domain, key),
			})
		}
	}
	sort.Slice(replacements, func(i, j int) bool {
		return len(replacements[i].value) > len(replacements[j].value)
	})

	for _, r := range replacements {
		text = strings.ReplaceAll(text, r.value, r.placeholder)
	}
	return text
}

// IsEmpty returns true if no credentials are configured.
func (c *CredentialStore) IsEmpty() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.creds) == 0
}

// LoadFromText parses credentials from a text string (same format as secrets file).
func LoadFromText(text string) *CredentialStore {
	store := NewCredentialStore()
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, " ", 3)
		var domain, kv string
		if len(parts) == 1 {
			domain = "default"
			kv = parts[0]
		} else if len(parts) >= 2 {
			if strings.Contains(parts[0], "=") {
				domain = "default"
				kv = parts[0]
			} else {
				domain = parts[0]
				kv = parts[1]
			}
		}

		kvParts := strings.SplitN(kv, "=", 2)
		if len(kvParts) != 2 {
			continue
		}
		key := strings.TrimSpace(kvParts[0])
		value := strings.TrimSpace(kvParts[1])
		if key == "" || value == "" {
			continue
		}

		store.Set(domain, key, value)
	}
	return store
}
