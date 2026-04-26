# Gemini CLI (Google)

**Repo**: `reference/gemini-cli/` — Google's Gemini CLI with hierarchical memory and event-driven scheduler

## What It Is

Google's official CLI agent for Gemini models. Its most distinctive features are the hierarchical memory system and the event-driven scheduler with policy checks. Less studied than other references but contains the best memory architecture of any agent system.

## Key Insights

### 1. Hierarchical Memory System (Best in Class)
```
global -> extension -> project -> user-project
```
- **Global**: universal preferences, identity, safety rules
- **Extension**: per-extension/plugin memory
- **Project**: project-specific conventions, knowledge
- **User-project**: user's personal preferences within a project
- Each level overrides/inherits from the level above
- Memory patches for incremental updates (not full rewrites)

### 2. Automatic Skill Extraction
- Identifies repeated conversation patterns
- Extracts them into reusable skills
- "I notice you always ask me to lint after changes. Should I add that as a skill?"
- Reduces manual configuration over time

### 3. Lock Coordination
- Concurrent access to memory requires locks
- Prevents race conditions when memory is updated mid-conversation
- Relevant for multi-agent setups

### 4. Event-Driven Scheduler
- `Scheduler` class coordinates tool execution
- Policy checks before execution (confirmation, sandboxing, rate limiting)
- Async tool execution with abort signals
- Not a simple sequential loop — tools can be queued and interleaved

### 5. Tool Confirmation System
- Policy-based: "confirm if operation affects >10 files"
- Configurable per tool/per operation type
- Abort signals for long-running operations

## What We've Implemented

Nothing directly from gemini-cli. Our memory is session-scoped only, and execution is purely sequential.

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P2 | Project-scoped memory persistence | High | Remember conventions across sessions |
| P2 | Skill extraction from repeated patterns | High | Auto-learn user preferences |
| P2 | Policy-based tool confirmation | Medium | Confirm risky operations vs auto-allow safe ones |
| P3 | Hierarchical memory (global/project/user) | High | Complex but powerful for long-running assistants |
| P3 | Lock coordination for concurrent memory access | Medium | Only relevant with parallelism |
| P3 | Event-driven scheduler | High | Fundamental architecture change |

### Key Architectural Insight
The hierarchical memory approach is the most sophisticated memory model in any reference. For our simpler use case, the practical takeaway is: **persist project-scoped memory across sessions**. Even a simple JSON file of "what we learned about this project" would improve the agent's effectiveness dramatically over multiple sessions.
