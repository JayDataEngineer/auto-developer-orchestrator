package perms

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// BashCommandRule is a user-defined bash command filtering rule.
// Pattern uses prefix matching: "rm" matches "rm -rf file", "docker*" matches "docker ps".
type BashCommandRule struct {
	ID      string          `json:"id"`
	Pattern string          `json:"pattern"`
	Level   PermissionLevel `json:"level"`
}

// BashRuleStore manages user-defined bash command rules.
// Thread-safe. Auto-saves to disk on every mutation.
type BashRuleStore struct {
	mu       sync.RWMutex
	rules    []BashCommandRule
	filePath string
	logger   *zap.Logger
}

// NewBashRuleStore creates an empty rule store.
func NewBashRuleStore(logger *zap.Logger) *BashRuleStore {
	return &BashRuleStore{
		rules:  make([]BashCommandRule, 0),
		logger: logger,
	}
}

// Load reads user rules from a JSON file. Missing file is not an error.
func (s *BashRuleStore) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var rules []BashCommandRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return err
	}
	// Validate loaded rules
	valid := make([]BashCommandRule, 0, len(rules))
	for _, r := range rules {
		if !isValidPattern(r.Pattern) {
			s.logger.Warn("Skipping bash rule with invalid pattern", zap.String("pattern", r.Pattern))
			continue
		}
		switch r.Level {
		case PermAutoApprove, PermRequireApproval, PermDeny:
			valid = append(valid, r)
		default:
			s.logger.Warn("Skipping bash rule with invalid level", zap.String("level", string(r.Level)))
		}
	}
	s.mu.Lock()
	s.rules = valid
	s.filePath = path
	s.mu.Unlock()
	s.logger.Debug("Loaded bash command rules", zap.String("path", path), zap.Int("count", len(valid)))
	return nil
}

// Save writes rules to disk.
func (s *BashRuleStore) Save(path string) error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.rules, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AllRules returns a snapshot of all user rules.
func (s *BashRuleStore) AllRules() []BashCommandRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]BashCommandRule, len(s.rules))
	copy(out, s.rules)
	return out
}

// AddRule creates a new rule. Returns the created rule.
// Validates pattern and level before adding.
func (s *BashRuleStore) AddRule(pattern string, level PermissionLevel) (BashCommandRule, error) {
	pattern = strings.TrimSpace(pattern)
	if !isValidPattern(pattern) {
		return BashCommandRule{}, errInvalidPattern(pattern)
	}
	switch level {
	case PermAutoApprove, PermRequireApproval, PermDeny:
	default:
		return BashCommandRule{}, errInvalidLevel(level)
	}

	id, _ := generateID()
	rule := BashCommandRule{ID: id, Pattern: pattern, Level: level}

	s.mu.Lock()
	s.rules = append(s.rules, rule)
	savePath := s.filePath
	s.mu.Unlock()

	if savePath != "" {
		_ = s.Save(savePath)
	}
	return rule, nil
}

// RemoveRule deletes a rule by ID. Returns false if not found.
func (s *BashRuleStore) RemoveRule(id string) bool {
	s.mu.Lock()
	idx := -1
	for i, r := range s.rules {
		if r.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return false
	}
	s.rules = append(s.rules[:idx], s.rules[idx+1:]...)
	savePath := s.filePath
	s.mu.Unlock()

	if savePath != "" {
		_ = s.Save(savePath)
	}
	return true
}

// Match checks if a command matches any user rule. Returns the first match.
// Pattern matching rules:
//   - "docker*" → strings.HasPrefix(cmd, "docker")
//   - "rm" → first token of cmd is exactly "rm"
//   - "git push*" → strings.HasPrefix(cmd, "git push")
func (s *BashRuleStore) Match(cmd string) (PermissionLevel, bool) {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	parts := strings.Fields(lower)
	if len(parts) == 0 {
		return "", false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, rule := range s.rules {
		pattern := strings.ToLower(rule.Pattern)
		if strings.HasSuffix(pattern, "*") {
			prefix := pattern[:len(pattern)-1]
			if strings.HasPrefix(lower, prefix) {
				return rule.Level, true
			}
		} else {
			// Exact first-token match
			if parts[0] == pattern {
				return rule.Level, true
			}
		}
	}
	return "", false
}

// SetFilePath enables auto-save.
func (s *BashRuleStore) SetFilePath(path string) {
	s.mu.Lock()
	s.filePath = path
	s.mu.Unlock()
}

// ── Helpers ──

func generateID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// isValidPattern rejects regex metacharacters. Allows alphanumeric, spaces,
// hyphens, underscores, dots, forward slashes, and a trailing *.
func isValidPattern(p string) bool {
	if p == "" {
		return false
	}
	// Check for disallowed characters
	for i, c := range p {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == ' ' || c == '-' || c == '_' || c == '.' || c == '/':
		case c == '*' && i == len(p)-1: // trailing * is ok
		default:
			return false
		}
	}
	return true
}

type patternError struct {
	pattern string
}

func (e *patternError) Error() string { return "invalid pattern: " + e.pattern }

func errInvalidPattern(p string) *patternError { return &patternError{pattern: p} }

type levelError struct {
	level string
}

func (e *levelError) Error() string { return "invalid level: " + e.level }

func errInvalidLevel(l PermissionLevel) *levelError { return &levelError{level: string(l)} }
