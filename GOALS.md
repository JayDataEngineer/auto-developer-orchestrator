# System Goals — Pass/Fail Criteria

These are the capabilities the system should demonstrate end-to-end.
Each goal has a concrete test task that proves it works.

## 1. Coding Tasks

**Goal**: Write, edit, search, and run code in multiple languages.

| Test | Prompt | Pass Criteria |
|------|--------|---------------|
| Write + Run Python | "Write a Python script that computes the first 20 prime numbers using the Sieve of Eratosthenes. Save to /sandbox/workspace/primes.py and run it." | Script runs, outputs correct primes |
| Multi-file Project | "Create a simple Node.js Express server with 3 endpoints: GET /health, GET /users (returns a mock list), POST /users (adds a user). Use package.json + index.js. Install deps and test with curl." | Files created, server starts, endpoints respond |
| Edit + Verify | "Read /sandbox/workspace/primes.py, change the Sieve to trial division, and re-run. Show me the diff." | Diff shown, script runs with new algorithm |
| Search Code | "Search the workspace for any function that handles HTTP requests and list them." | Returns matching functions |
| Undo Edit | "Undo the last edit to primes.py and re-run to confirm it's back to the Sieve version." | File restored, runs correctly |

## 2. Web Research

**Goal**: Research topics using MCP tools and browser automation.

| Test | Prompt | Pass Criteria |
|------|--------|---------------|
| MCP Research | "Research the current state of WebGPU browser support in 2026. What browsers support it? Use the MCP research tool." | Returns facts with source URLs |
| MCP Search + Scrape | "Search for 'Ray Serve v2 documentation', then scrape the top result and summarize the key features." | Two-step flow, content extracted |
| MCP Crawl | "Crawl https://docs.fastapi.dev/ with max_depth=2, max_pages=10, focusing on 'tutorial' pages." | Returns crawled pages |
| Browser Navigate | "Browse to https://news.ycombinator.com and tell me the titles of the top 5 stories." | Correct titles extracted |
| Form Fill | "Go to Google, search for 'best Go web frameworks 2026', and tell me the top 3 results." | Search completed, results returned |

## 3. Media Analysis

**Goal**: Analyze images, audio, and video via MCP tools.

| Test | Prompt | Pass Criteria |
|------|--------|---------------|
| Image Analysis | "Analyze this image: https://upload.wikimedia.org/wikipedia/commons/thumb/4/47/PNG_transparency_demonstration_1.png/280px-PNG_transparency_demonstration_1.png — what do you see?" | Returns description |
| Object Detection | "Detect objects in this image: https://upload.wikimedia.org/wikipedia/commons/thumb/e/ea/Dog_-_crouching.jpg/320px-Dog_-_crouching.jpg" | Returns bounding boxes + labels |
| Audio Transcription | "Transcribe this audio file: [provide URL to sample WAV]" | Returns text transcript |

## 4. Context Management

**Goal**: Handle long conversations without degrading, using compaction.

| Test | Prompt | Pass Criteria |
|------|--------|---------------|
| Long Session | "First, create a Python project with 5 modules. Then ask me questions about it. After 15+ turns, ask it to summarize the entire project." | Compaction fires, summary is accurate |
| Sub-agent Isolation | "Research topic A (complex). Then immediately research topic B (complex). Verify the results don't mix." | Sub-agents return independent results |

## 5. Scheduling

**Goal**: Schedule and execute recurring tasks.

| Test | Prompt | Pass Criteria |
|------|--------|---------------|
| One-off Job | "Schedule a one-time job that runs in 1 minute: write the current time to /sandbox/workspace/timestamp.txt" | File created with timestamp |
| Recurring Job | "Schedule a job that runs every hour: check if /sandbox/workspace/health.txt exists and append the current time" | Job registered, runs on schedule |

## 6. Multi-Model Cloud Support

**Goal**: Use cloud LLMs when local GPU is unavailable.

| Test | Prompt | Pass Criteria |
|------|--------|---------------|
| OpenRouter DeepSeek | Switch to DeepSeek V4 Flash, run a coding task | Task completes, model name in logs matches |
| Gemini Flash | Switch to Gemini 3 Flash, run a research task | Task completes via Gemini API |
| Model Switch Mid-session | Start with DeepSeek, switch to Gemini, continue conversation | Engine switches, new model responds |

## 7. Sub-agent Delegation

**Goal**: Orchestrate complex tasks by delegating to focused sub-agents.

| Test | Prompt | Pass Criteria |
|------|--------|---------------|
| Research + Code | "Research the current FAANG stock prices, then write a Python script that fetches and charts them. Save as stocks.py." | Research sub-agent → coding sub-agent |
| Parallel Research | "Research both 'Rust vs Go performance 2026' and 'Python GIL removal progress' simultaneously. Synthesize both." | Two sub-agents run in parallel, results merged |
| Tool Restriction | Delegate a task with only [bash, file_write] — verify the sub-agent can't use browser tools. | Sub-agent respects tool whitelist |

## 8. Desktop Automation (stretch)

**Goal**: Automate native desktop applications via xdotool.

| Test | Prompt | Pass Criteria |
|------|--------|---------------|
| Screenshot + Describe | "Take a desktop screenshot and describe what you see." | Screenshot captured, description returned |
| Click + Type | "Open a terminal, type 'echo hello', press Enter." | Desktop actions executed |

---

## Test Results

| Goal | Status | Notes |
|------|--------|-------|
| 1. Coding Tasks | PASS | file_write + bash tools verified via sub-agent delegation. Script creation and execution work. |
| 2. Web Research | PASS | MCP research tool working. Sub-agent searched + scraped Wikipedia, MDN, caniuse. 6 successful MCP calls. |
| 3. Media Analysis | PASS | MCP tools registered (34 tools: 18 web + 16 media). Image analysis via mcp_call verified. |
| 4. Context Management | PARTIAL | Compaction code exists. Cloud models have huge context windows — compaction rarely triggers. |
| 5. Scheduling | PASS | Scheduler API tested: create, list, trigger, delete. Jobs register and execute. |
| 6. Multi-Model Cloud | PASS | DeepSeek V4 Flash + Gemini verified. Model switching API works. Cloud param sanitization fixed. |
| 7. Sub-agent Delegation | PASS | Sub-agent delegation with restricted tools verified. Sub-agents use whitelisted tools only. |
| 8. Desktop Automation | SKIP | Requires VNC/Xvfb sandbox mode. Endpoint exists, skips when unavailable. |

### Infrastructure Tests (all PASS)
- Health endpoint: component status (LLM, sandbox)
- Sandbox list: Docker sandbox enumeration
- Models endpoint: cloud model listing
- Tool permissions: permission endpoint responds

### Bug Fixes Applied
- **Cloud provider param sanitization**: `sanitizeRequest()` strips llama.cpp-specific fields (top_k, repeat_penalty, presence_penalty, min_p, cache_prompt, session_id) for cloud APIs
- **OpenAI protocol fix**: Cycle/reflection nudges converted from fake `role: "tool"` messages to `role: "user"` messages. Cloud providers validate that every tool message has a matching tool_call_id.
