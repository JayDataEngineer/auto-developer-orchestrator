package sensitive

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Store manages domain-scoped credentials with <secret> placeholder injection.
// The model uses <secret>domain.key</secret> placeholders in tool arguments.
// The executor resolves them before execution and redacts them from results.
type Store struct {
	mu    sync.RWMutex
	creds map[string]map[string]string
}

var secretRe = regexp.MustCompile(`<secret>([^<]+)</secret>`)

// NewStore creates an empty credential store.
func NewStore() *Store {
	return &Store{creds: make(map[string]map[string]string)}
}

// Set adds a credential for a domain.
func (s *Store) Set(domain, key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creds[domain] == nil {
		s.creds[domain] = make(map[string]string)
	}
	s.creds[domain][key] = value
}

// Resolve replaces all <secret>domain.key</secret> placeholders in text.
func (s *Store) Resolve(text string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return secretRe.ReplaceAllStringFunc(text, func(match string) string {
		path := secretRe.FindStringSubmatch(match)
		if len(path) < 2 {
			return match
		}
		parts := strings.SplitN(path[1], ".", 2)
		if len(parts) != 2 {
			return match
		}
		domain, key := parts[0], parts[1]
		if s.creds[domain] != nil {
			if val, ok := s.creds[domain][key]; ok {
				return val
			}
		}
		return match
	})
}

// ResolveArgs resolves secrets in a map of arguments.
func (s *Store) ResolveArgs(args map[string]any) map[string]any {
	resolved := make(map[string]any, len(args))
	for k, v := range args {
		if str, ok := v.(string); ok {
			resolved[k] = s.Resolve(str)
		} else {
			resolved[k] = v
		}
	}
	return resolved
}

// Redact replaces known credential values with <secret>domain.key</secret>.
func (s *Store) Redact(text string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for domain, keys := range s.creds {
		for key, val := range keys {
			text = strings.ReplaceAll(text, val, fmt.Sprintf("<secret>%s.%s</secret>", domain, key))
		}
	}
	return text
}

// Domains returns configured domain names.
func (s *Store) Domains() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	domains := make([]string, 0, len(s.creds))
	for d := range s.creds {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	return domains
}

// Keys returns keys for a domain.
func (s *Store) Keys(domain string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creds[domain] == nil {
		return nil
	}
	keys := make([]string, 0, len(s.creds[domain]))
	for k := range s.creds[domain] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Delete removes a credential.
func (s *Store) Delete(domain, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creds[domain] != nil {
		delete(s.creds[domain], key)
	}
}
