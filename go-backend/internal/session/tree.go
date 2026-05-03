package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SessionTree implements core.Session using JSONL files with a tree structure.
// Each session is a JSONL file where entries are linked via parentId pointers.
// This enables branching (try alternative approaches), forking (new session from any point),
// and lossless compaction (old messages are replaced by summaries but preserved in the file).
type SessionTree struct {
	filePath string
	session  core.SessionHeader
	current  string // current node ID

	mu      sync.Mutex
	entries map[string]core.SessionEntry // loaded entries indexed by ID
	nodes   map[string]*core.TreeNode    // tree nodes indexed by ID
	file    *os.File
}

// New creates a new session tree rooted at a JSONL file.
func New(filePath string, cwd string) (*SessionTree, error) {
	id := newID("sess")
	t := &SessionTree{
		filePath: filePath,
		session: core.SessionHeader{
			Type:      "session",
			Version:   1,
			ID:        id,
			Timestamp: time.Now(),
			CWD:       cwd,
		},
		current: id,
		entries: make(map[string]core.SessionEntry),
		nodes:   make(map[string]*core.TreeNode),
	}

	// Create root entry
	headerData, _ := json.Marshal(t.session)
	rootEntry := core.SessionEntry{
		ID:        id,
		ParentID:  "",
		Timestamp: t.session.Timestamp,
		Type:      core.EntryTypeSession,
		Data:      headerData,
	}
	t.entries[id] = rootEntry
	t.nodes[id] = &core.TreeNode{Entry: rootEntry}

	// Create/open file
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create session file: %w", err)
	}
	t.file = f

	// Write header
	if err := t.writeEntry(rootEntry); err != nil {
		f.Close()
		return nil, err
	}

	return t, nil
}

// Load loads an existing session tree from a JSONL file.
func Load(filePath string) (*SessionTree, error) {
	f, err := os.OpenFile(filePath, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}

	t := &SessionTree{
		filePath: filePath,
		entries:  make(map[string]core.SessionEntry),
		nodes:    make(map[string]*core.TreeNode),
		file:     f,
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry core.SessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			log.Printf("SessionTree: skipping invalid entry at line %d: %v", lineNum, err)
			continue
		}

		// Parse session header from first line
		if entry.Type == core.EntryTypeSession {
			if err := json.Unmarshal(entry.Data, &t.session); err != nil {
				log.Printf("SessionTree: skipping invalid session header at line %d: %v", lineNum, err)
				continue
			}
		}

		t.entries[entry.ID] = entry
		t.nodes[entry.ID] = &core.TreeNode{Entry: entry}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	// Build tree links (parent → child)
	for _, node := range t.nodes {
		parentID := node.Entry.ParentID
		if parentID != "" {
			if parent, ok := t.nodes[parentID]; ok {
				node.Parent = parent
				parent.Children = append(parent.Children, node)
			}
		}
	}

	// Sort children by timestamp for consistent ordering
	for _, node := range t.nodes {
		sort.Slice(node.Children, func(i, j int) bool {
			return node.Children[i].Entry.Timestamp.Before(node.Children[j].Entry.Timestamp)
		})
	}

	// Set current to the last leaf (most recent message)
	if leaf := t.findLastLeaf(t.nodes[t.session.ID]); leaf != nil {
		t.current = leaf.Entry.ID
	}

	return t, nil
}

// findLastLeaf returns the deepest leaf node from the given node.
// Uses the last child (most recently appended) to follow the primary path.
func (t *SessionTree) findLastLeaf(node *core.TreeNode) *core.TreeNode {
	if node == nil {
		return nil
	}
	if len(node.Children) == 0 {
		return node
	}
	// Follow the last child (most recent)
	return t.findLastLeaf(node.Children[len(node.Children)-1])
}

// ID returns the session identifier.
func (t *SessionTree) ID() string {
	return t.session.ID
}

// AppendMessage appends a message to the current branch.
func (t *SessionTree) AppendMessage(msg core.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	entryType, err := messageToEntryType(msg)
	if err != nil {
		return err
	}

	entryID := newID("msg")
	entryData, _ := json.Marshal(msg)

	entry := core.SessionEntry{
		ID:        entryID,
		ParentID:  t.current,
		Timestamp: time.Now(),
		Type:      entryType,
		Data:      entryData,
	}

	if err := t.writeEntry(entry); err != nil {
		return err
	}

	t.entries[entryID] = entry
	node := &core.TreeNode{Entry: entry, Parent: t.nodes[t.current]}
	t.nodes[entryID] = node
	if parent, ok := t.nodes[t.current]; ok {
		parent.Children = append(parent.Children, node)
	}

	t.current = entryID
	return nil
}

// BuildContext reconstructs the full conversation context for the LLM.
// Walks from root to the current node, replacing compacted ranges with summaries.
func (t *SessionTree) BuildContext(ctx context.Context) ([]core.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var messages []core.Message

	// Walk from root to current node
	path := t.walkPath(t.session.ID, t.current)
	if path == nil {
		return nil, fmt.Errorf("cannot build context: path from %s to %s not found", t.session.ID, t.current)
	}

	compactedSet := make(map[string]bool)

	for _, node := range path {
		if compactedSet[node.Entry.ID] {
			continue // entry is inside a compacted range
		}

		if node.Entry.Type == core.EntryTypeCompaction {
			// Parse compaction data to find the compacted range
			var compData CompactionData
			if err := json.Unmarshal(node.Entry.Data, &compData); err != nil {
				continue
			}
			for _, compacted := range compData.CompactedRange {
				compactedSet[compacted] = true
			}
			// Add the compaction summary as a user message
			messages = append(messages, core.Message{
				Role:    "user",
				Content: "[COMPACTED HISTORY]\n" + compData.Summary + "\nContinue from where you left off.",
			})
			continue
		}

		if node.Entry.Type == core.EntryTypeBranchSummary {
			var summary string
			if node.Entry.Data != nil {
				json.Unmarshal(node.Entry.Data, &summary)
			}
			messages = append(messages, core.Message{
				Role:    "user",
				Content: fmt.Sprintf("[BRANCH SUMMARY: %s]", summary),
			})
			continue
		}

		if node.Entry.Type == core.EntryTypeSession {
			continue // skip session header
		}

		var msg core.Message
		if err := json.Unmarshal(node.Entry.Data, &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// Navigate jumps to a different node in the session tree.
func (t *SessionTree) Navigate(nodeID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.nodes[nodeID]; !ok {
		return fmt.Errorf("node %s not found in session tree", nodeID)
	}

	t.current = nodeID
	return nil
}

// Branch creates a new branch from the current node.
// This creates a placeholder entry that becomes the new current node.
func (t *SessionTree) Branch(label string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	branchID := newID("branch")
	entry := core.SessionEntry{
		ID:        branchID,
		ParentID:  t.current,
		Timestamp: time.Now(),
		Type:      core.EntryTypeBranchSummary,
		Label:     label,
	}

	if err := t.writeEntry(entry); err != nil {
		return "", err
	}

	t.entries[branchID] = entry
	node := &core.TreeNode{Entry: entry, Parent: t.nodes[t.current]}
	t.nodes[branchID] = node
	if parent, ok := t.nodes[t.current]; ok {
		parent.Children = append(parent.Children, node)
	}

	t.current = branchID
	return branchID, nil
}

// Fork creates a new session from the given node.
// The forked session starts a new JSONL file with context up to that node.
func (t *SessionTree) Fork(nodeID string) (core.Session, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.nodes[nodeID]; !ok {
		return nil, fmt.Errorf("node %s not found for fork", nodeID)
	}

	// Create a new session file
	forkPath := t.filePath + ".fork-" + newID("fork")
	forked, err := New(forkPath, t.session.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to create fork session: %w", err)
	}

	// Replay messages from root to fork point into the new session
	path := t.walkPath(t.session.ID, nodeID)
	if path == nil {
		forked.Close()
		return nil, fmt.Errorf("path to fork node not found")
	}

	for _, node := range path {
		if node.Entry.Type == core.EntryTypeSession || node.Entry.Type == core.EntryTypeCompaction || node.Entry.Type == core.EntryTypeBranchSummary {
			continue
		}
		var msg core.Message
		if err := json.Unmarshal(node.Entry.Data, &msg); err != nil {
			continue
		}
		forked.AppendMessage(msg)
	}

	return forked, nil
}

// Compact summarizes older messages to free context space.
// This is a no-op in the base implementation — hooks handle actual compaction.
func (t *SessionTree) Compact(ctx context.Context, llmProvider interface{}) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	compID := newID("comp")
	entry := core.SessionEntry{
		ID:        compID,
		ParentID:  t.current,
		Timestamp: time.Now(),
		Type:      core.EntryTypeCompaction,
		Data: json.RawMessage(`{"summary":"Context compacted","compactedRange":[]}`),
	}

	if err := t.writeEntry(entry); err != nil {
		return "", err
	}

	t.entries[compID] = entry
	node := &core.TreeNode{Entry: entry, Parent: t.nodes[t.current]}
	t.nodes[compID] = node

	t.current = compID
	return compID, nil
}

// TruncateToolResults walks the current path and replaces old tool result
// content with "[tool result truncated]" placeholders, keeping only the
// `keep` most recent tool results intact. Returns the number truncated.
func (t *SessionTree) TruncateToolResults(keep int) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	path := t.walkPath(t.session.ID, t.current)
	if path == nil {
		return 0, fmt.Errorf("cannot resolve path for truncation")
	}

	// Find tool result nodes (walking from end, skip the keep most recent)
	var toolNodes []*core.TreeNode
	for _, node := range path {
		if node.Entry.Type == core.EntryTypeToolResult {
			toolNodes = append(toolNodes, node)
		}
	}

	if len(toolNodes) <= keep {
		return 0, nil
	}

	truncated := 0
	for i := 0; i < len(toolNodes)-keep; i++ {
		node := toolNodes[i]
		if node.Entry.Data == nil {
			continue
		}
		var msg core.Message
		if err := json.Unmarshal(node.Entry.Data, &msg); err != nil {
			continue
		}
		if msg.Content == "" {
			continue
		}
		// Replace with placeholder and re-marshal
		msg.Content = fmt.Sprintf("[tool result truncated: %d bytes]", len(msg.Content))
		if newData, err := json.Marshal(msg); err == nil {
			// Update in-memory entry (data in the node; JSONL file keeps original)
			updated := node.Entry
			updated.Data = newData
			node.Entry = updated
			t.entries[node.Entry.ID] = updated
		}
		truncated++
	}

	return truncated, nil
}

// GetTree returns the session tree for navigation.
func (t *SessionTree) GetTree() *core.TreeNode {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.nodes[t.session.ID]
}

// GetCurrentNode returns the current tree node ID.
func (t *SessionTree) GetCurrentNode() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.current
}

// Close releases the file handle.
func (t *SessionTree) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.file != nil {
		return t.file.Close()
	}
	return nil
}

// FilePath returns the path to the session JSONL file.
func (t *SessionTree) FilePath() string {
	return t.filePath
}

// ── Internal helpers ────────────────────────────────────────────────

// writeEntry appends an entry to the JSONL file.
func (t *SessionTree) writeEntry(entry core.SessionEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal session entry: %w", err)
	}
	if _, err := t.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write session entry: %w", err)
	}
	if err := t.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync session file: %w", err)
	}
	return nil
}

// walkPath returns the path from startID to endID (inclusive).
// Returns nil if no path exists.
func (t *SessionTree) walkPath(startID, endID string) []*core.TreeNode {
	// Build path from end to start, then reverse
	var path []*core.TreeNode
	current := t.nodes[endID]
	for current != nil {
		path = append(path, current)
		if current.Entry.ID == startID {
			// Reverse
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
		current = current.Parent
	}
	return nil
}

// messageToEntryType maps a core.Message role to a session entry type.
func messageToEntryType(msg core.Message) (core.SessionEntryType, error) {
	switch msg.Role {
	case "system":
		return core.EntryTypeSystemMessage, nil
	case "user":
		return core.EntryTypeUserMessage, nil
	case "assistant":
		return core.EntryTypeAssistantMessage, nil
	case "tool":
		return core.EntryTypeToolResult, nil
	default:
		return "", fmt.Errorf("unknown message role: %s", msg.Role)
	}
}

// CompactionData is the data stored in a compaction entry.
type CompactionData struct {
	Summary        string   `json:"summary"`
	CompactedRange []string `json:"compactedRange"`
}


