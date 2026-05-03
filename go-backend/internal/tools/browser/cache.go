package browser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// ElementCache implements the Stagehand-inspired caching pattern for browser element IDs.
// When the model describes an element (e.g., "the Login button"), we snapshot the page,
// find the matching element via SoM labels, and cache the element ID keyed by instruction.
// On cache hit, we skip re-snapshotting and use the cached ID directly.
//
// Self-healing: if a cached click results in an error, the cache is invalidated and
// the next attempt re-snapshots + re-matches.
type ElementCache struct {
	mu       sync.RWMutex
	entries  map[string]*CacheEntry
	maxSize  int // max entries before eviction
	hitCount int64
	missCount int64
}

// CacheEntry holds a cached element resolution.
type CacheEntry struct {
	Key          string // hash of sandboxID + instruction + URL
	ElementID    int    // SoM label number
	ElementIndex int    // index in the labeled elements list
	ElementTag   string // tag name (e.g., "button", "input")
	ElementText  string // visible text on the element
	URL          string // page URL when resolved
	PageTitle    string // page title when resolved
	UsageCount   int    // how many times this cache entry was used
}

// NewElementCache creates an element cache with the given max size.
func NewElementCache(maxSize int) *ElementCache {
	if maxSize <= 0 {
		maxSize = 200
	}
	return &ElementCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
	}
}

// CacheKey generates a deterministic key from sandbox, instruction, and URL.
func CacheKey(sandboxID, instruction, url string) string {
	h := sha256.New()
	h.Write([]byte(sandboxID))
	h.Write([]byte("::"))
	h.Write([]byte(instruction))
	h.Write([]byte("::"))
	h.Write([]byte(url))
	return hex.EncodeToString(h.Sum(nil))[:16] // 16 hex chars = 64 bits
}

// Get retrieves a cached element by instruction.
// Returns nil on cache miss.
func (c *ElementCache) Get(instruction, sandboxID, url string) *CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := CacheKey(sandboxID, instruction, url)
	entry, ok := c.entries[key]
	if !ok {
		c.missCount++
		return nil
	}
	entry.UsageCount++
	c.hitCount++
	return entry
}

// Set caches an element resolution.
func (c *ElementCache) Set(instruction, sandboxID, url string, elementID int, elementTag, elementText, pageTitle string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		var oldestKey string
		var oldestCount int
		for k, v := range c.entries {
			if oldestKey == "" || v.UsageCount < oldestCount {
				oldestKey = k
				oldestCount = v.UsageCount
			}
		}
		delete(c.entries, oldestKey)
	}

	key := CacheKey(sandboxID, instruction, url)
	c.entries[key] = &CacheEntry{
		Key:         key,
		ElementID:   elementID,
		ElementTag:  elementTag,
		ElementText: elementText,
		URL:         url,
		PageTitle:   pageTitle,
		UsageCount:  0,
	}
}

// Invalidate removes a cache entry.
func (c *ElementCache) Invalidate(instruction, sandboxID, url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, CacheKey(sandboxID, instruction, url))
}

// InvalidateAll removes all entries for a sandbox.
func (c *ElementCache) InvalidateAll(sandboxID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for k := range c.entries {
		if _, found := c.entries[k]; found {
			delete(c.entries, k)
			count++
		}
	}
	return count
}

// Stats returns cache statistics.
func (c *ElementCache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"size":    len(c.entries),
		"max":     c.maxSize,
		"hits":    c.hitCount,
		"misses":  c.missCount,
		"hitRate": func() float64 {
			total := c.hitCount + c.missCount
			if total == 0 {
				return 0
			}
			return float64(c.hitCount) / float64(total)
		}(),
	}
}

// String returns a human-readable summary.
func (c *ElementCache) String() string {
	s := c.Stats()
	return fmt.Sprintf("ElementCache(size=%v/%v, hits=%v, misses=%v, rate=%.1f%%)",
		s["size"], s["max"], s["hits"], s["misses"], s["hitRate"].(float64)*100)
}

// ── Page fingerprinting for stagnation detection ───────────────────

// PageFingerprint tracks page content for detecting stagnation.
type PageFingerprint struct {
	mu          sync.Mutex
	fingerprints []string
	maxHistory   int
}

// NewPageFingerprint creates a fingerprint tracker.
func NewPageFingerprint(maxHistory int) *PageFingerprint {
	if maxHistory <= 0 {
		maxHistory = 10
	}
	return &PageFingerprint{
		maxHistory: maxHistory,
	}
}

// Add records a new page fingerprint.
func (p *PageFingerprint) Add(content string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	hash := p.hashContent(content)
	p.fingerprints = append(p.fingerprints, hash)
	if len(p.fingerprints) > p.maxHistory {
		p.fingerprints = p.fingerprints[1:]
	}
}

// StagnantSteps returns how many consecutive steps had the same fingerprint.
func (p *PageFingerprint) StagnantSteps() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.fingerprints) < 2 {
		return 0
	}

	last := p.fingerprints[len(p.fingerprints)-1]
	count := 0
	for i := len(p.fingerprints) - 1; i >= 0; i-- {
		if p.fingerprints[i] == last {
			count++
		} else {
			break
		}
	}
	return count
}

// IsStagnant returns true if the page hasn't changed for the threshold.
func (p *PageFingerprint) IsStagnant(threshold int) bool {
	return p.StagnantSteps() >= threshold
}

// GetWarnings returns warning messages for the model.
func (p *PageFingerprint) GetWarnings() []string {
	stagnant := p.StagnantSteps()
	if stagnant >= 3 {
		return []string{
			fmt.Sprintf("[WARNING: Page unchanged for %d steps. The page may not have loaded correctly or your action had no effect. Try a different approach.]", stagnant),
		}
	}
	return nil
}

func (p *PageFingerprint) hashContent(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))[:8]
}
