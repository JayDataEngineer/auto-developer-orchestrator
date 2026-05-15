#!/bin/bash
# verify-web.sh — E2E verification for the web frontend
# Usage: bash scripts/verify-web.sh
# Prereqs: Backend on 3847, Vite on 5175, playwright installed
set -e
PASS=0 FAIL=0

check() {
    local label="$1" result="$2"
    if [ "$result" = "true" ]; then echo "  PASS: $label"; PASS=$((PASS+1))
    else echo "  FAIL: $label"; FAIL=$((FAIL+1)); fi
}

echo "=== Backend API ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3847/api/sandbox/)
check "Backend responds" "$([ "$STATUS" = "200" ] && echo true || echo false)"

BODY=$(curl -s http://localhost:3847/api/sandbox/)
VALID=$(python3 -c "import json,sys; json.load(sys.stdin); print('true')" <<< "$BODY" 2>/dev/null)
check "Sandbox list valid JSON" "${VALID:-false}"

TERM_OK=$(python3 -c "
import asyncio, websockets
async def t():
    async with websockets.connect('ws://localhost:3847/api/terminal/ws?shell=bash&cwd=auto-developer-orchestrator', open_timeout=5) as ws:
        await ws.send('echo OK\n')
        for _ in range(8):
            m = await asyncio.wait_for(ws.recv(), timeout=3)
            if b'OK' in (m if isinstance(m,bytes) else m.encode()): print('true'); return
    print('false')
asyncio.run(t())
" 2>/dev/null)
check "Terminal WS works" "$TERM_OK"

DIFF=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:3847/api/pux/git/diff?project=auto-developer-orchestrator")
check "Git diff endpoint 200" "$([ "$DIFF" = "200" ] && echo true || echo false)"

echo ""
echo "=== Frontend (Playwright) ==="
python3 << 'PYEOF'
from playwright.sync_api import sync_playwright
import time

results = []
def check(label, ok):
    print(f"  {'PASS' if ok else 'FAIL'}: {label}")
    results.append(ok)

with sync_playwright() as p:
    browser = p.chromium.launch()
    page = browser.new_page(viewport={"width":1400,"height":900})
    page.goto("http://localhost:5175", wait_until="networkidle")
    time.sleep(4)

    wb = page.query_selector("div.fixed.inset-y-0.right-0")

    # Sandbox tab
    for b in wb.query_selector_all("button"):
        if (b.inner_text() or "").strip().lower() == "sandbox":
            b.click(); time.sleep(3); break
    iframe = wb.query_selector("iframe[title='Sandbox VNC']")
    start = page.query_selector("button:has-text('Start sandbox')")
    check("Sandbox shows iframe or start button", iframe is not None or (start is not None and start.is_visible()))

    # Editor tab + git diff
    for b in wb.query_selector_all("button"):
        if (b.inner_text() or "").strip().lower() == "editor":
            b.click(); time.sleep(2); break
    git = page.locator('button[title="Show git changes"]')
    if git.count() == 0: git = page.locator('button[title="Show file tree"]')
    check("Git toggle found", git.count() > 0)
    if git.count() > 0:
        git.first.click(); time.sleep(3)
        files = [b for b in wb.query_selector_all("button") if any(
            k in (b.inner_text() or "") for k in ["CLAUDE", "CONTRACT", "Taskfile", "package"]
        )]
        check("Diff files listed", len(files) > 0)
        if files:
            files[0].click(); time.sleep(3)
            check("Monaco editor rendered", wb.query_selector(".monaco-editor") is not None)

    # Terminal
    tb = page.locator('button[aria-label="Toggle terminal"]')
    if tb.count() > 0:
        tb.first.click(); time.sleep(4)
        check("xterm rendered", page.query_selector(".xterm") is not None)
        rows = page.query_selector(".xterm-rows")
        if rows:
            text = rows.inner_text().lower()
            check("Terminal no error", "failed to start" not in text and "no such file" not in text)

    browser.close()

fails = sum(1 for ok in results if not ok)
print(f"\nFrontend: {len(results)-fails}/{len(results)} passed")
PYEOF

echo ""
echo "=== DONE: ${PASS} passed, ${FAIL} failed ==="
[ "$FAIL" -eq 0 ] || exit 1
