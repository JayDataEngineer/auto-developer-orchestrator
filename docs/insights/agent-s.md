# Agent-S

**Repo**: `reference/Agent-S/` — SOTA GUI agent grounding and reflection (72.6% on OSWorld)

## What It Is

A GUI agent framework with three versions (S1, S2, S3). S3 achieved 72.6% on OSWorld by removing complexity — dropping DAG-based planning and accessibility trees in favor of pure visual grounding (UI-TARS-1.5-7B) plus a reflection agent for cycle detection.

## Key Insights

### 1. Simplicity Wins (S3 > S1/S2)
- **S1/S2**: DAG-based task planning, accessibility tree, knowledge base, RAG
- **S3**: Removed ALL of that. Single Worker agent, pure visual grounding, reflection agent
- **Result**: S3 = 72.6%, S2 = 65.3%, S1 = 55.2%
- This is the foundational insight for our "Simple over clever" design principle

### 2. Reflection Agent (The Key Innovation)
- Detects repetitive action patterns **without suggesting specific fixes**
- If the model is stuck in a loop, the reflection says: "I notice you've clicked the same button 5 times. Have you considered a different approach?"
- **Critical**: does NOT say "try clicking the blue button instead" — that would bias the model
- Just flags the loop, lets the model decide how to break it

### 3. UI-TARS Visual Grounding
- Separate grounding model (UI-TARS-1.5-7B) from decision model
- Converts natural language descriptions to pixel coordinates
- Coordinate scaling: model space (1920x1080) -> actual screen
- Temperature=0.05 for deterministic coordinate output

### 4. Perceptual Hashing for Loop Detection
- SSIM (Structural Similarity) between screenshots
- Detects visual similarity even when DOM structure differs
- More robust than text-based cycle detection alone

### 5. Unicode Text via Clipboard
- Characters with Unicode are copied to clipboard and pasted (`ctrl+v`)
- Simple characters are typed directly
- Avoids character-by-character typing failures on special chars

### 6. Screenshot Preprocessing
- pyautogui.screenshot() -> resize to max 2400px -> PNG -> base64
- Optional WEBP compression for smaller payloads
- Keeps screenshots within model context limits

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| Cycle detection | `grounding.go:45` | `CycleDetector` — "From Agent-S: reflection/cycle detection without suggesting fixes" |
| Cycle nudge | `grounding.go:100` | `CycleNudge()` — "detects the loop without suggesting specific fixes (Agent-S pattern)" |
| Flat agent loops | Architecture | Design principle #4: "Simple over clever. Flat agent loops beat deep hierarchies" |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P1 | Visual similarity loop detection (SSIM/perceptual hash) | Medium | More reliable than text-only cycle detection. Catches visual stuck states |
| P1 | Reflection prompt after each action | Low | "Did I make progress? Am I stuck in a loop?" — cheap, high impact |
| P1 | Unicode text via clipboard paste | Low | Currently fails on special characters in direct typing |
| P2 | Screenshot resize to max 2400px | Low | Prevents context bloat |
| P3 | Visual grounding model (UI-TARS) | High | 7-8GB VRAM for a separate model. Only justified if desktop automation is core |

### Key Architectural Insight
Agent-S S3's "simplicity wins" result is the most important empirical finding in GUI agents. It validates our architecture choice of a single orchestrator agent with flat tool loops. The reflection agent pattern (detect loops, don't prescribe solutions) is elegant and we've already implemented it in `grounding.go`.
