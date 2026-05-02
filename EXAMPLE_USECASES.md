# Example Use Cases — End-to-End Workflows

These are real-world multi-step tasks the orchestrator should handle.
Each use case has a concrete prompt, the expected tool chain, and observable success criteria.
Use these to verify capabilities haven't regressed after changes.

---

## UC-01: Build & Test a Python CLI Tool

**Prompt**:
> Create a Python CLI tool called `wordfreq` that reads a text file and prints the top N most frequent words. It should accept a `--file` argument and an optional `--top` argument (default 10). Use `argparse`. Save it to `/sandbox/workspace/wordfreq.py`, create a sample text file with at least 50 words, install any needed deps, run it, and show the output.

**Tool Chain**: `file_write` → `file_write` (sample.txt) → `bash` (run script) → `bash` (verify output)

**Success Criteria**:
- `wordfreq.py` exists and is valid Python
- Script runs without errors
- Output shows word frequencies sorted by count
- `--top` argument works (e.g., `--top 5` shows only 5 words)

**Notes**: Tests `file_write` for code generation, `bash` for execution and debugging.

---

## UC-02: Research & Summarize a Topic

**Prompt**:
> Research the current state of the Python GIL removal effort (PEP 703 and related). What's the latest status? Which Python version will have a free-threaded build? Use MCP research tools. Provide a summary with source URLs.

**Tool Chain**: `mcp_research` → `mcp_scrape` (optional follow-up) → `synthesize`

**Success Criteria**:
- Agent uses MCP research tool (not just web search)
- Returns factual, current information
- Includes source URLs or references
- Summary is coherent and accurate

**Notes**: Requires MCP web-research server running on Ray cluster (100.86.69.57:8327).

---

## UC-03: Web Form Automation

**Prompt**:
> Browse to https://httpbin.org/forms/post, fill in the form with: name "Alice", email "alice@example.com", and a message "Hello from the orchestrator". Submit the form and tell me what the response page shows.

**Tool Chain**: `browse_to` → `observe` (detect elements) → `type_text` (name) → `type_text` (email) → `type_text` (message) → `click_element` (submit) → `read_page`

**Success Criteria**:
- Agent navigates to the correct URL
- Form fields are filled correctly
- Submit button is clicked
- Response page shows the submitted data (httpbin echoes form data as JSON)

**Notes**: Requires browser mode enabled (auto-enabled on sandbox creation since v0.2). Uses SoM labeling for element detection.

---

## UC-04: Refactor Across Multiple Files

**Prompt**:
> Create a small Python project with 3 files: `models.py` (a User and Post class), `database.py` (CRUD functions using dicts), and `main.py` (CLI that uses both). Then refactor: rename the `User` class to `Account` everywhere it appears, add a `created_at` field, and update all references. Run the code to verify it still works.

**Tool Chain**: `file_write` × 3 → `file_read` → `file_edit` × 3 → `bash` (run)

**Success Criteria**:
- All 3 files created initially
- After refactor: no references to `User` remain (only `Account`)
- `created_at` field present in the class
- Code runs without `NameError` or similar

**Notes**: Tests cross-file refactoring, `file_edit` with search/replace, and `bash` for verification.

---

## UC-05: Parallel Research + Merge

**Prompt**:
> I need two things done simultaneously: 1) Research the latest version of the Go programming language and its key features. 2) Research the latest version of Rust and its key features. After both are done, create a comparison table summarizing the differences.

**Tool Chain**: `delegate_to` (async, research Go) → `delegate_to` (async, research Rust) → `collect_results` → `synthesize`

**Success Criteria**:
- Two sub-agents launched (check for `subagent_start` events)
- Each returns independent research results
- Final response includes a comparison table
- No cross-contamination between research results

**Notes**: Tests async delegation, sub-agent isolation, and result synthesis. High complexity — may take 5-10 minutes.

---

## UC-06: Scheduled Monitoring Job

**Prompt**:
> Schedule a recurring job that runs every 30 minutes. It should curl the health endpoint of this server (http://localhost:3847/api/health) and append the timestamp and status to `/sandbox/workspace/health_log.txt`. Name the job "health-monitor".

**API Call**:
```bash
curl -X POST http://localhost:3847/api/scheduler \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "health-monitor",
    "project": "test-repo",
    "message": "Run the health check and log the result",
    "scheduleType": "cron",
    "cronExpr": "0 */30 * * * *"
  }'
```

**Tool Chain**: Scheduler API (`POST /api/scheduler`) → verify via `GET /api/scheduler` → trigger via `POST /api/scheduler/{jobId}/trigger`

**Success Criteria**:
- Job created with name "health-monitor"
- 6-field cron expression (`0 */30 * * * *`) accepted
- Job appears in scheduler list
- Manual trigger returns `{"success": true}`

**Notes**: Low complexity. Tests scheduler CRUD. Cron uses 6 fields (seconds + standard 5). `message` field is required (the prompt sent to the agent when the job fires).

**Notes**: Low complexity. Tests scheduler CRUD without requiring LLM for execution.

---

## UC-07: Image Analysis Pipeline

**Prompt**:
> Download this image: https://upload.wikimedia.org/wikipedia/commons/thumb/3/3a/Cat03.jpg/1200px-Cat03.jpg — then analyze it three ways: 1) Describe what you see, 2) Detect and list all objects with bounding boxes, 3) Extract the dominant colors. Present all results together.

**Tool Chain**: `bash` (curl download) → `mcp_analyze_image` → `mcp_detect_objects` → `mcp_extract_colors`

**Success Criteria**:
- Image downloaded to sandbox
- Description is accurate (mentions cat)
- Object detection returns bounding boxes with "cat" label
- Color palette extracted (5+ colors)
- All three results presented in a unified response

**Notes**: Requires MCP media-analysis server on Ray cluster (100.86.69.57:8001). The media-analysis prefix is `mcp_`, so tools are `mcp_analyze_image`, `mcp_detect_objects`, `mcp_extract_colors`.

---

## UC-08: Full-Stack Project Scaffolding

**Prompt**:
> Create a full-stack project in /sandbox/workspace/fullstack-demo:
> - Backend: Python FastAPI with endpoints GET /api/items (returns a list), POST /api/items (adds an item), DELETE /api/items/{id} (removes one). Store items in memory.
> - Frontend: A single index.html with vanilla JS that fetches and displays items, has a form to add new items, and a delete button for each.
> - Install deps, start the server, and verify the API works with curl.

**Tool Chain**: `file_write` (main.py) → `file_write` (index.html) → `bash` (pip install) → `bash` (start server) → `bash` (curl tests)

**Success Criteria**:
- Both files created with correct structure
- FastAPI server starts without errors
- `GET /api/items` returns `[]` initially
- `POST /api/items` adds an item and returns it
- `DELETE /api/items/{id}` removes the item
- index.html has working fetch/add/delete JS

**Notes**: High complexity. Tests multi-file creation, dependency installation, server lifecycle, and API verification.

---

## UC-09: Code Review & Patch

**Prompt**:
> I have a Python file at /sandbox/workspace/review_target.py that should compute Fibonacci numbers but it has bugs. Read it, identify the issues, fix them, and show me a diff of what you changed. If the fix is wrong, undo it and try again.

**Setup** (write this file first):
```python
def fibonacci(n):
    if n <= 0:
        return []
    if n == 1:
        return [0]
    fib = [0, 1]
    for i in range(2, n):
        fib.append(fib[i-1] + fib[i-2])  # bug: wrong indexing
    return fib

def fibonacci_recursive(n):
    if n <= 0:
        return 0
    if n == 1:
        return 1
    return fibonacci_recursive(n-1) + fibonacci_recursive(n-2)  # bug: returns nth value not list

print(fibonacci(10))
print(fibonacci_recursive(10))
```

**Tool Chain**: `file_write` (create buggy file) → prompt → `file_read` → `file_edit` → `bash` (run and verify) → `undo_edit` (if wrong)

**Success Criteria**:
- Agent reads the file and identifies issues
- `file_edit` applied with correct fixes
- Diff output shown (unified diff format)
- Code runs correctly after fix
- If first fix is wrong, `undo_edit` is used

**Notes**: Tests code analysis, `file_edit` with diff output, `undo_edit` rollback, and iterative debugging.

---

## UC-10: Desktop App Interaction

**Prompt**:
> Take a desktop screenshot and tell me what you see. Then open a terminal (if one isn't already visible), type `echo "Hello from the orchestrator"` and press Enter. Take another screenshot to confirm the output appears.

**Tool Chain**: `desktop_screenshot` → `desktop_click` (open terminal) → `desktop_type` → `desktop_key` (Enter) → `desktop_screenshot`

**Success Criteria**:
- First screenshot captured and described
- Terminal opened or focused
- Command typed correctly
- Second screenshot shows "Hello from the orchestrator" in terminal output

**Notes**: Requires desktop mode enabled (XFCE4 + Xvfb running in sandbox). The `desktop_*` tools use xdotool under the hood. Display `:99` is the browser mode default.

---

## Test Results

| Use Case | Status | Notes |
|----------|--------|-------|
| UC-01: Build & Test CLI | PASS | 14 tools used. Created wordfreq.py + sample.txt, ran successfully. 1257 chars response. |
| UC-02: Research & Summarize | PASS | 28 tools used. MCP research tool called 23x. Delegated to sub-agent. 13801 chars response with sources. |
| UC-03: Web Form Automation | NOT TESTED | Requires browser automation — pending vision model. |
| UC-04: Refactor Across Files | PASS | 13 tools used. User→Account rename + created_at field across 3 files. Runs clean after refactor. |
| UC-05: Parallel Research | NOT TESTED | Requires async delegation + collect_results. Pending. |
| UC-06: Scheduled Monitoring | PASS | Job CRUD verified: create (6-field cron), list, trigger all succeed. |
| UC-07: Image Analysis Pipeline | NOT TESTED | Requires MCP media server + vision. Pending. |
| UC-08: Full-Stack Scaffolding | NOT TESTED | High complexity, pending. |
| UC-09: Code Review & Patch | PASS | 9 tools used. Read buggy code, identified issues, fixed, ran. 1536 chars response. |
| UC-10: Desktop Interaction | NOT TESTED | Requires desktop mode + vision. Pending. |

### Anti-Bot Bypass (SeleniumBase)

| Test | Status | Notes |
|------|--------|-------|
| Cloudflare Turnstile | PASS | Solved in ~9.6s via `sb.solve_captcha()`. Token obtained, success image visible. |
| Bot Detection (sannysoft.com) | PASS | Not detected as bot. `navigator.webdriver` hidden. |
| Regular Browsing (HN) | PASS | Normal site navigation works. |

Setup: `pip install seleniumbase` + `apt install python3-tk` in sandbox. Helper at `/usr/local/bin/sb_stealth.py`.

### Quick Smoke Test (UC-01 + UC-02 + UC-06)

All three pass. Run after any change to verify core capabilities:
```bash
# UC-06 (API only, instant)
curl -X POST http://localhost:3847/api/scheduler \
  -H 'Content-Type: application/json' \
  -d '{"name":"smoke-test","project":"test-repo","message":"echo smoke","scheduleType":"cron","cronExpr":"0 */30 * * * *"}'

# UC-01 + UC-02 require LLM — use test script:
# python /tmp/test_uc_sh.py
```

---

## Regression Checklist

When making changes, verify these still work:

- [ ] UC-01: Code generation + execution
- [ ] UC-02: MCP research + synthesis
- [ ] UC-03: Browser form filling
- [ ] UC-04: Cross-file refactoring
- [ ] UC-05: Parallel delegation
- [ ] UC-06: Scheduler job creation
- [ ] UC-07: Media analysis pipeline
- [ ] UC-08: Full-stack scaffolding
- [ ] UC-09: Code review + patch
- [ ] UC-10: Desktop automation

### Quick Smoke Test

For a fast check after small changes, run these three:
1. **UC-01** (coding) — proves bash + file tools work
2. **UC-02** (research) — proves MCP tools work
3. **UC-06** (scheduling) — proves API endpoints work

If all three pass, most likely nothing is broken.
