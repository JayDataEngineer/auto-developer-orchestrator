package core

import (
	"context"
	"encoding/json"
	"time"
)

// Session is the interface for session management.
// Implementations include SessionTree (JSONL tree model) and KVSession (llama-server KV cache).
// The agent loop uses this to accumulate conversation history and build context for the LLM.
type Session interface {
	// ID returns the unique session identifier.
	ID() string

	// AppendMessage appends a message to the current branch.
	AppendMessage(msg Message) error

	// BuildContext reconstructs the full conversation context for the LLM.
	// Walks from root to the current node, applying compaction summaries as needed.
	BuildContext(ctx context.Context) ([]Message, error)

	// Navigate jumps to a different node in the session tree.
	// Returns an error if the node doesn't exist.
	Navigate(nodeID string) error

	// Branch creates a new branch from the current node.
	// Returns the new branch node ID.
	Branch(label string) (string, error)

	// Fork creates a new session from any node in the current tree.
	// The forked session is a new JSONL file with context up to that node.
	Fork(nodeID string) (Session, error)

	// Compact summarizes older messages to free context space.
	// The summary string replaces the compacted range in the context.
	// Returns the compaction entry ID.
	Compact(ctx context.Context, summary string) (string, error)

	// TruncateToolResults replaces old tool result content with a short placeholder.
	// Keeps the `keep` most recent tool results intact, truncates older ones.
	// Returns the number of tool results truncated.
	TruncateToolResults(keep int) (int, error)

	// ReplaceToolResults replaces old tool result content using a custom function.
	// The replace function receives (index, toolName, currentContent) and returns
	// the new content. Keeps the `keep` most recent results intact.
	// Returns the number of tool results replaced.
	ReplaceToolResults(replace func(i int, name, content string) string, keep int) (int, error)

	// GetTree returns the session tree for navigation.
	GetTree() *TreeNode

	// GetCurrentNode returns the current tree node ID.
	GetCurrentNode() string

	// GetUserCheckpoints returns user message entries along the current path.
	// Used by the rewind feature to show conversation checkpoints.
	GetUserCheckpoints() []Checkpoint

	// Close releases any resources held by the session.
	Close() error
}

// Checkpoint is a user message entry that can be rewound to.
type Checkpoint struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Preview   string    `json:"preview"` // truncated user message text
}

// TreeNode is a node in the session tree.
type TreeNode struct {
	Entry    SessionEntry
	Children []*TreeNode
	Parent   *TreeNode
}

// SessionEntryType identifies the type of a session entry.
type SessionEntryType string

const (
	EntryTypeSession         SessionEntryType = "session"
	EntryTypeUserMessage     SessionEntryType = "user_message"
	EntryTypeAssistantMessage SessionEntryType = "assistant_message"
	EntryTypeToolResult      SessionEntryType = "tool_result"
	EntryTypeCompaction      SessionEntryType = "compaction"
	EntryTypeBranchSummary   SessionEntryType = "branch_summary"
	EntryTypeSystemMessage   SessionEntryType = "system_message"
)

// SessionEntry is a single entry in the JSONL session file.
type SessionEntry struct {
	ID        string          `json:"id"`
	ParentID  string          `json:"parentId"`
	Timestamp time.Time       `json:"timestamp"`
	Type      SessionEntryType `json:"type"`
	Label     string          `json:"label,omitempty"` // optional human-readable label for branching
	Data      json.RawMessage `json:"data,omitempty"`
}

// SessionHeader is the first line of a session file.
type SessionHeader struct {
	Type          string    `json:"type"` // always "session"
	Version       int       `json:"version"`
	ID            string    `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	CWD           string    `json:"cwd"`
	ParentSession string    `json:"parentSession,omitempty"`
}
