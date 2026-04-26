# OS-Symphony

**Repo**: `reference/OS-Symphony/` — Holistic multi-agent framework for computer-using agents

## What It Is

A multi-agent desktop automation framework with specialized agent routing. The orchestrator coordinates a Reflection-Memory Agent (RMA), GUI agent, Code agent, and Search agent. Notable for its text span agent (word-level OCR coordinate mapping) and code-verification pattern.

## Key Insights

### 1. Specialized Agent Routing
- **GUI agent** — visual tasks (click, type, scroll)
- **Code agent** — bulk file operations, script execution
- **Search agent** — knowledge gaps, web tutorials (using "SeeAct" paradigm)
- **Reflection-Memory Agent (RMA)** — long-term strategy and milestone tracking
- Orchestrator routes tasks to the right agent

### 2. Text Span Agent (Most Unique Feature)
- OCR text detection -> word-level coordinate mapping via LLM
- "Click after the word 'Submit'" -> precise pixel coordinates
- Alignment-aware: `start` vs `end` of a phrase
```python
if alignment == "start":
    return [elem["left"], elem["top"] + elem["height"] // 2]
elif alignment == "end":
    return [elem["left"] + elem["width"] + 0.15 * elem["height"], ...]
```
- The most precise text-targeting approach in any reference

### 3. Code/GUI Verification Pattern
- Code agent makes changes -> GUI agent verifies visually
- "Did the button actually appear after running that script?"
- Prevents silent failures in code-only automation

### 4. Multimodal Web Searcher
- Uses "SeeAct" paradigm for learning from web tutorials
- Searches for "how to do X", reads tutorials, extracts steps
- Bridges the gap between knowledge and execution

### 5. Milestone-Driven Memory
- Long-term memory organized around task milestones
- "Phase 1: login completed", "Phase 2: data entry started"
- Different from Claude Code's categorical memory — more task-oriented

## What We've Implemented

Minimal. Sub-agent routing exists (`delegate_to` for web/code/desktop), but no specialized routing logic, no text span agent, no verification pattern.

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P1 | Code/GUI verification pattern | Low | Run code then visually verify. High impact for reliability |
| P2 | Text span agent (word-level OCR targeting) | Medium | Precise text selection is a common failure mode |
| P2 | Intelligent task routing (GUI vs Code vs Search) | Medium | We have sub-agents but routing is manual/"delegate_to" |
| P2 | Multimodal web searcher (SeeAct) | Medium | Tutorial-based learning for complex tasks |
| P3 | Milestone-driven long-term memory | Medium | Task-phase memory vs categorical memory |

### Key Architectural Insight
The text span agent is the most underrated idea in OS-Symphony. "Click after the word 'Submit'" and "select text between 'Name' and 'Email'" are common user intents that current coordinate-based systems handle poorly. Even a basic OCR + fuzzy text matching approach would be a step up from our current pure-vision guessing.
