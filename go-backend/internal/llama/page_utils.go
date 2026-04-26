package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"
)

// ── Page summary and vision utilities ───────────────────────────────

// interactiveTags are tags the model can meaningfully interact with.
var interactiveTags = map[string]bool{
	"a": true, "button": true, "input": true, "textarea": true,
	"select": true, "option": true, "details": true, "summary": true,
}

// spatialZone returns a position label for an element based on its Y coordinate.
func spatialZone(y, h int) string {
	if y < 100 {
		return "top"
	}
	if y < 400 {
		return "mid-top"
	}
	if y < 700 {
		return "center"
	}
	return "bottom"
}

// extractPageSummary extracts useful text from a page action result.
// Returns XML tree-style output grouped by parent containers (form, nav, etc.).
// Marks NEW elements with [*NEW] and detects page stagnation.
func (e *SandboxToolExecutor) extractPageSummary(result map[string]interface{}) string {
	if result == nil {
		return ""
	}
	var parts []string
	if url, ok := result["url"].(string); ok && url != "" {
		parts = append(parts, "URL: "+url)
	}
	if title, ok := result["title"].(string); ok && title != "" {
		parts = append(parts, "Title: "+title)
	}

	if elements, ok := result["elements"].([]interface{}); ok && len(elements) > 0 {
		newSigs := make(map[string]bool)
		type elemInfo struct {
			id    int
			tag   string
			text  string
			zone  string
			isNew bool
		}
		grouped := make(map[string][]elemInfo)
		var ungrouped []elemInfo

		for _, el := range elements {
			m, ok := el.(map[string]interface{})
			if !ok {
				continue
			}
			tag, _ := m["tag"].(string)
			if !interactiveTags[tag] {
				continue
			}
			id, _ := m["id"].(float64)
			text, _ := m["text"].(string)
			text = truncate(text, 50)
			if text == "" {
				continue
			}
			sig := fmt.Sprintf("%s:%s", tag, text)
			newSigs[sig] = true

			isNew := false
			if e.lastElements != nil {
				if prev, ok := e.lastElements[e.SandboxID]; ok {
					if !prev[sig] {
						isNew = true
					}
				}
			}

			y, _ := m["y"].(float64)
			parent, _ := m["parent"].(string)
			ei := elemInfo{id: int(id), tag: tag, text: text, zone: spatialZone(int(y), 0), isNew: isNew}
			if parent != "" {
				grouped[parent] = append(grouped[parent], ei)
			} else {
				ungrouped = append(ungrouped, ei)
			}
		}

		if e.lastElements == nil {
			e.lastElements = make(map[string]map[string]bool)
		}
		e.lastElements[e.SandboxID] = newSigs

		// Page fingerprinting for stagnation detection
		fp := pageFingerprint{
			url:     "URL: " + parts[0],
			elCount: len(newSigs),
			sig:     fingerprintSig(newSigs),
		}
		if e.pageFingerprints == nil {
			e.pageFingerprints = make(map[string][]pageFingerprint)
		}
		fps := e.pageFingerprints[e.SandboxID]
		fps = append(fps, fp)
		if len(fps) > 10 {
			fps = fps[len(fps)-10:]
		}
		e.pageFingerprints[e.SandboxID] = fps

		if stagnantCount := countStagnant(fps); stagnantCount >= 3 {
			parts = append(parts, fmt.Sprintf(
				"[WARNING: Page unchanged for %d steps. Try a DIFFERENT action — scroll, click elsewhere, or navigate to a new URL.]",
				stagnantCount))
		}

		// Build element index for description→ID resolution
		var idx []indexedElement
		for _, elems := range grouped {
			for _, ei := range elems {
				idx = append(idx, indexedElement{ID: ei.id, Tag: ei.tag, Text: ei.text, Zone: ei.zone})
			}
		}
		for _, ei := range ungrouped {
			idx = append(idx, indexedElement{ID: ei.id, Tag: ei.tag, Text: ei.text, Zone: ei.zone})
		}
		if e.elemIndex == nil {
			e.elemIndex = make(map[string][]indexedElement)
		}
		e.elemIndex[e.SandboxID] = idx

		totalCount := len(ungrouped)
		for _, elems := range grouped {
			totalCount += len(elems)
		}

		// Count element types for page statistics (from browser-use pattern)
		typeCounts := make(map[string]int)
		for _, ei := range ungrouped {
			typeCounts[ei.tag]++
		}
		for _, elems := range grouped {
			for _, ei := range elems {
				typeCounts[ei.tag]++
			}
		}
		statParts := make([]string, 0, len(typeCounts))
		for tag, count := range typeCounts {
			statParts = append(statParts, fmt.Sprintf("%d %s", count, tag))
		}
		sort.Strings(statParts)
		parts = append(parts, fmt.Sprintf("Page elements (%d): %s", totalCount, strings.Join(statParts, ", ")))

		for parent, elems := range grouped {
			parts = append(parts, "<"+parent+">")
			for _, ei := range elems {
				newMarker := ""
				if ei.isNew {
					newMarker = " *NEW*"
				}
				parts = append(parts, fmt.Sprintf("  [%d] <%s> %s \"%s\"%s",
					ei.id, ei.tag, ei.zone, ei.text, newMarker))
			}
			parts = append(parts, "</"+parent+">")
		}
		if len(ungrouped) > 0 {
			parts = append(parts, "<page>")
			for _, ei := range ungrouped {
				newMarker := ""
				if ei.isNew {
					newMarker = " *NEW*"
				}
				parts = append(parts, fmt.Sprintf("  [%d] <%s> %s \"%s\"%s",
					ei.id, ei.tag, ei.zone, ei.text, newMarker))
			}
			parts = append(parts, "</page>")
		}
	}

	if len(parts) == 0 {
		delete(result, "image")
		delete(result, "screenshot")
		b, _ := json.Marshal(result)
		return string(b)
	}
	return strings.Join(parts, "\n")
}

// visionSummary captures a screenshot and describes it via the vision model.
func (e *SandboxToolExecutor) visionSummary(ctx context.Context, sandboxID string) string {
	if !e.VisionEnabled {
		return ""
	}
	result, err := e.CU.Screenshot(ctx, sandboxID, true)
	if err != nil {
		e.Logger.Debug("vision screenshot failed", zap.Error(err))
		return ""
	}
	desc, ok := result["description"].(string)
	if !ok || desc == "" {
		return ""
	}
	if len(desc) > 500 {
		desc = desc[:497] + "..."
	}
	return desc
}

// truncate shortens a string to maxLen characters and collapses whitespace.
func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ── Page fingerprinting for loop/stagnation detection ───────────────

// fingerprintSig creates a compact signature from element signatures.
func fingerprintSig(sigs map[string]bool) string {
	keys := make([]string, 0, len(sigs))
	for k := range sigs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	combined := strings.Join(keys, "|")
	if len(combined) > 64 {
		combined = combined[:64]
	}
	return combined
}

// countStagnant returns how many of the most recent fingerprints are identical.
func countStagnant(fps []pageFingerprint) int {
	if len(fps) < 2 {
		return 0
	}
	last := fps[len(fps)-1]
	count := 0
	for i := len(fps) - 1; i >= 0; i-- {
		if fps[i].url == last.url && fps[i].sig == last.sig {
			count++
		} else {
			break
		}
	}
	return count
}
