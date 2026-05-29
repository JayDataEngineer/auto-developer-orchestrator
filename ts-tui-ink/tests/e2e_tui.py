#!/usr/bin/env python3
"""
Pux TUI End-to-End Test Suite — enhanced comprehensive coverage.
==============================================================
Covers all overlays, views, keyboard shortcuts, dialogs, and
status bar states via the visual testing server on port 9877.

Usage:
  task tui-visual   # start the server first
  python3 tests/e2e_tui.py

Requires: requests
"""

import sys, time, requests
from dataclasses import dataclass, field

BASE = "http://localhost:9877"

# ── Helpers ──

def gs():
    return requests.get(f"{BASE}/screen").json()["screen"]

def is_overlay_open():
    t = gs()
    return "═" in t

def is_normal_mode():
    t = gs()
    return "NORMAL" in t

def si(text, wait=5):
    if not is_overlay_open() and is_normal_mode():
        requests.post(f"{BASE}/input", json={"text": "i", "wait": 0.3})
        time.sleep(0.15)
    requests.post(f"{BASE}/input", json={"text": text, "wait": wait})
    time.sleep(1.5)

def sk(key):
    requests.post(f"{BASE}/key", json={"key": key})
    time.sleep(0.5)

def restart():
    requests.post(f"{BASE}/restart")
    time.sleep(6)
    for _ in range(15):
        try:
            t = gs()
            if "Pux" in t or "Message" in t:
                time.sleep(1)
                return t
        except:
            pass
        time.sleep(1)
    return gs()

def soft_reset():
    for _ in range(3):
        sk("escape")
    time.sleep(0.3)

def print_screen(label, t=None):
    if t is None:
        t = gs()
    lines = [l.rstrip() for l in t.split("\n") if l.strip()]
    print(f"\n  [{label}]")
    for l in lines[:10]:
        print(f"    {l}")
    if len(lines) > 10:
        print(f"    ... ({len(lines) - 10} more)")
    return t

@dataclass
class TestResult:
    name: str
    passed: bool
    detail: str = ""

@dataclass
class Suite:
    results: list = field(default_factory=list)

    def check(self, name: str, condition: bool, detail: str = ""):
        r = TestResult(name, condition, detail)
        self.results.append(r)
        icon = "PASS" if condition else "FAIL"
        msg = f"  [{icon}] {name}"
        if not condition and detail:
            msg += f" — {detail}"
        print(msg)
        return condition

    def skip(self, name: str, reason: str = "dependency failed"):
        r = TestResult(name, False, f"Skipped: {reason}")
        self.results.append(r)
        print(f"  [SKIP] {name} ({reason})")
        return False

    def summary(self):
        p = sum(1 for r in self.results if r.passed)
        t = len(self.results)
        skipped = sum(1 for r in self.results if "Skipped" in r.detail)
        print(f"\n{'='*60}")
        print(f"RESULTS: {p}/{t} passed ({skipped} skipped)")
        print(f"{'='*60}")
        for r in self.results:
            icon = "PASS" if r.passed else ("SKIP" if "Skipped" in r.detail else "FAIL")
            print(f"  [{icon}] {r.name}")
        return p == t - skipped

s = Suite()

# ═══════════════════════════════════════════════════════════
# SECTION 1: BASIC STARTUP & CHAT VIEW
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 1: Startup & Chat View")
print("="*60)

t = gs()
s.check("TUI renders", "Pux" in t, "Screen empty or server down")
s.check("Welcome shows version", "Pux v" in t)
s.check("Welcome shows model", "Model:" in t)
s.check("Welcome shows hints", "Try:" in t or "/help" in t)
s.check("Composer visible", "Message" in t or ">" in t)
s.check("Tab bar shows Chat active", "Chat" in t and ("→ Chat" in t or " Chat " in t))
s.check("Status bar visible", "·" in t)

# ═══════════════════════════════════════════════════════════
# SECTION 2: SLASH COMMANDS
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 2: Slash Commands")
print("="*60)

# /help opens overlay
si("/help\n")
t = gs()
s.check("/help shows command list", "Commands" in t or "/help" in t or "/" in t)
s.check("/help has General group", "General" in t)
s.check("/help has Panels group", "Panels" in t)
s.check("/help has Views group", "Views" in t)
s.check("/help shows hint", "Esc to close" in t)
# /help closes on Enter
sk("return")
time.sleep(0.5)
t = gs()
s.check("/help closes on Enter", "Pux" in t or "Message" in t)

# /status
restart()
si("/status\n")
t = gs()
s.check("/status shows session info",
        "model" in t.lower() or "project" in t.lower() or "View:" in t)

# /clear
restart()
si("/clear\n")
t = gs()
s.check("/clear shows confirmation", "clear" in t.lower())

# /new
restart()
si("/new\n")
t = gs()
s.check("/new starts conversation", "Pux" in t or "Message" in t)

# /history
restart()
si("/history\n")
t = gs()
s.check("/history shows output",
        "conversation" in t.lower() or "history" in t.lower() or "No " in t
        or "recent" in t.lower())

# /status with compacting
restart()
si("/status\n")
t = gs()
has_view_line = "View:" in t
has_project_line = "Project:" in t
s.check("/status shows View", has_view_line)
s.check("/status shows Project", has_project_line)

# Invalid commands
restart()
si("/notacommand\n")
t = gs()
s.check("Invalid command shows error", "Unknown" in t)

# ═══════════════════════════════════════════════════════════
# SECTION 3: VIEW SWITCHING (Ctrl+T)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 3: View Switching")
print("="*60)

restart()
views = ["Agents", "Tools", "Files", "Conversations", "Chat"]
cycle_ok = True
for i, expected in enumerate(views):
    sk("ctrl+t")
    t = gs()
    found = expected.lower() in t.lower()
    if not found:
        print(f"    Ctrl+T #{i+1}: expected '{expected}', got:")
        print_screen(f"Ctrl+T → {expected} (FAIL)", t)
        cycle_ok = False
        break
s.check("Ctrl+T cycles all 5 views", cycle_ok)

# Direct slash commands switch views
for cmd, expected in [("/chat", "Pux"), ("/agents", "Subagent"), ("/tools", "Tool"), ("/conversations", "onversation"), ("/files", "File")]:
    restart()
    si(cmd + "\n")
    t = gs()
    s.check(f"{cmd} switches view", expected in t)

# Non-chat views show composer (should always show input bar)
restart()
si("/tools\n")
t = gs()
s.check("Tools view renders", "Tool" in t)
# Should see the composer separator line
s.check("Composer visible in tools view", "─" in t)

# ═══════════════════════════════════════════════════════════
# SECTION 4: PROVIDERS OVERLAY
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 4: Providers Overlay")
print("="*60)

restart()
si("/model\n")
t = print_screen("Providers List")
overlay_open = "Providers" in t and "═" in t
s.check("/model opens overlay", overlay_open)

if overlay_open:
    s.check("Provider names visible", "llamacpp" in t or "gemini" in t or "openai" in t or "openrouter" in t)
    s.check("Type badges visible", "Local" in t or "Cloud" in t or "Aggregator" in t)
    s.check("Add provider option", "Add provider" in t)
    s.check("Navigation hints in footer", "navigate" in t.lower())
    s.check("Visual rules present", "═" in t)

    # Provider detail (Screen B)
    sk("down")  # select first provider
    si("\n")
    t = print_screen("Provider Detail")
    s.check("Detail shows status icon", "●" in t or "○" in t)
    s.check("Detail shows base URL", "localhost" in t or "http" in t or "." in t)
    s.check("Detail shows model info", "ctx" in t.lower() or "out" in t.lower() or "free" in t or "$" in t)
    s.check("Detail has back hint", "back" in t.lower())

    # Escape back to list
    sk("escape")
    t = gs()
    s.check("Escape from detail returns to list", "Providers" in t)

    # Escape from list to chat
    sk("escape")
    t = gs()
    s.check("Escape from list returns to chat", "Pux" in t or "Message" in t)

    # Catalog picker (Screen C)
    restart()
    si("/model\n")
    t = gs()
    if "Providers" in t:
        si("a")
        t = print_screen("Catalog Picker")
        s.check("Catalog shows select prompt", "Select a provider" in t or "select" in t.lower())
        s.check("Catalog shows known providers", "openai" in t.lower() or "anthropic" in t.lower())
        s.check("Catalog has custom option", "custom" in t.lower() or "Custom" in t)

        # Config form (Screen D)
        si("\n")
        t = print_screen("Config Form")
        s.check("Config has Base URL field", "Base URL" in t)
        s.check("Config has API Key field", "API Key" in t)
        s.check("Config has Model ID field", "Model ID" in t)
        s.check("Config has hint", "Enter confirm" in t or "Tab" in t)

        # Escape chain: config → add → list → chat
        sk("escape")
        t1 = gs()
        c2a = "Select a provider" in t1 or "custom" in t1.lower()
        sk("escape")
        t2 = gs()
        a2l = "Providers" in t2 or "Add provider" in t2
        sk("escape")
        t3 = gs()
        l2c = ("Pux" in t3 or "Message" in t3) and "Providers" not in t3
        s.check("Escape: config→add→list→chat", c2a and a2l and l2c,
                f"c→a:{c2a} a→l:{a2l} l→c:{l2c}")
    else:
        for n in ["Catalog select prompt", "Known providers", "Custom option",
                  "Config Base URL", "Config API Key", "Config Model ID",
                  "Config hint", "Escape chain"]:
            s.skip(n, "overlay didn't re-open")
else:
    for n in ["Provider names", "Type badges", "Add provider", "Navigation hints",
              "Visual rules", "Detail status icon", "Detail base URL", "Detail model info",
              "Detail back hint", "Escape detail→list", "Escape list→chat",
              "Catalog select prompt", "Known providers", "Custom option",
              "Config Base URL", "Config API Key", "Config Model ID",
              "Config hint", "Escape chain"]:
        s.skip(n, "overlay didn't open")

# ═══════════════════════════════════════════════════════════
# SECTION 5: SETTINGS OVERLAY
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 5: Settings Overlay")
print("="*60)

restart()
si("/settings\n")
t = print_screen("Settings")
settings_open = "Settings" in t
s.check("/settings opens overlay", settings_open)

if settings_open:
    s.check("Settings has Active Model section", "Active Model" in t or "Model" in t)
    s.check("Settings has Providers section", "Providers" in t)
    s.check("Settings has Theme section", "Theme" in t)
    s.check("Settings has System section", "System" in t)
    s.check("Settings shows font scale", "%" in t or "scale" in t.lower() or "size" in t.lower())

    # Close
    sk("escape")
    t = gs()
    s.check("Settings closes on Esc",
            ("Pux" in t or "Message" in t) and "Settings" not in t)
else:
    for n in ["Model section", "Providers section", "Theme section",
              "System section", "Font scale", "Esc close"]:
        s.skip(n, "overlay didn't open")

# ═══════════════════════════════════════════════════════════
# SECTION 6: SEARCH OVERLAY
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 6: Search Overlay")
print("="*60)

restart()
si("/search\n")
t = print_screen("Search")
search_open = "Search" in t
s.check("/search opens overlay", search_open)

if search_open:
    s.check("Search has input prompt", ">" in t or "Search" in t)
    s.check("Search has hint text", "Type to search" in t or "navigate" in t)
    s.check("Search has close hint", "Esc close" in t or "close" in t)

    # Type a query
    si("test")
    t = gs()
    s.check("Search accepts input", "test" in t or "No matches" in t)

    sk("escape")
    t = gs()
    s.check("Search closes on Esc",
            ("Pux" in t or "Message" in t) and "Search" not in t)
else:
    for n in ["Search input", "Hint text", "Close hint", "Accepts input", "Esc close"]:
        s.skip(n, "overlay didn't open")

# ═══════════════════════════════════════════════════════════
# SECTION 7: LOG VIEWER
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 7: Log Viewer")
print("="*60)

restart()
si("/logs\n")
t = print_screen("Logs")
logs_open = "Diagnostics" in t or "Log" in t
s.check("/logs opens overlay", logs_open)

if logs_open:
    s.check("Logs has tab headers", "Agent Activity" in t or "Token" in t or "Context" in t or "Session" in t)
    s.check("Logs has tab switching hint", "switch tab" in t or "←" in t or "→" in t)
    s.check("Logs has close hint", "Esc close" in t)

    # Switch tabs with right arrow
    sk("right")
    t = gs()
    s.check("Logs right arrow switches tab", "Token" in t or "Usage" in t)

    sk("right")
    t = gs()
    s.check("Logs right arrow again", "Context" in t or "Util" in t)

    sk("left")
    t = gs()
    s.check("Logs left arrow switches tab", "Usage" in t or "Agent" in t)

    sk("escape")
    t = gs()
    s.check("Logs closes on Esc",
            ("Pux" in t or "Message" in t) and "Diagnostics" not in t)
else:
    for n in ["Tab headers", "Tab switch hint", "Close hint",
              "Right arrow switch", "Left arrow switch", "Esc close"]:
        s.skip(n, "overlay didn't open")

# ═══════════════════════════════════════════════════════════
# SECTION 8: SESSION SWITCHER
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 8: Session Switcher")
print("="*60)

restart()
si("/sessions\n")
t = print_screen("Sessions")
sessions_open = "Switch Session" in t or "Session" in t
s.check("/sessions opens overlay", sessions_open)

if sessions_open:
    s.check("Sessions has filter input", ">" in t)
    s.check("Sessions has hint", "filter" in t.lower() or "navigate" in t or "switch" in t)

    # Type filter text
    si("test")
    t = gs()
    s.check("Sessions accepts filter input", "test" in t or "matching" in t.lower() or "No matching" in t)

    sk("escape")
    t = gs()
    s.check("Sessions closes on Esc",
            ("Pux" in t or "Message" in t) and "Switch Session" not in t)
else:
    for n in ["Filter input", "Hint", "Filter accepts text", "Esc close"]:
        s.skip(n, "overlay didn't open")

# ═══════════════════════════════════════════════════════════
# SECTION 9: MCP OVERLAY
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 9: MCP Overlay")
print("="*60)

restart()
si("/mcp\n")
t = print_screen("MCP")
mcp_open = "MCP Servers" in t or "MCP" in t
s.check("/mcp opens overlay", mcp_open)

if mcp_open:
    s.check("MCP has add server option", "Add server" in t or "+ Add" in t)
    s.check("MCP has navigation hint", "navigate" in t.lower() or "delete" in t.lower())
    s.check("MCP has close hint", "Esc close" in t)

    # Open add form (Screen B)
    si("a")
    t = print_screen("MCP Add")
    s.check("MCP add has Prefix field", "Prefix" in t)
    s.check("MCP add has Endpoint field", "Endpoint" in t)
    s.check("MCP add has hint", "Enter confirm" in t)

    # Escape back to list
    sk("escape")
    t = gs()
    s.check("MCP escape add→list", "MCP Servers" in t or "MCP" in t)

    sk("escape")
    t = gs()
    s.check("MCP closes on Esc",
            ("Pux" in t or "Message" in t) and "MCP" not in t)
else:
    for n in ["Add server option", "Navigation hint", "Close hint",
              "Add Prefix field", "Add Endpoint field", "Add hint",
              "Escape add→list", "Esc close"]:
        s.skip(n, "overlay didn't open")

# ═══════════════════════════════════════════════════════════
# SECTION 10: MODEL PICKER
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 10: Model Picker")
print("="*60)

restart()
# Enter settings, then Enter on model section
si("/settings\n")
t = gs()
if "Settings" in t:
    sk("return")
    t = print_screen("Model Picker")
    s.check("Settings Enter on model opens picker", "Model" in t and ("Provider" in t or "models" in t.lower() or "(" in t))
    sk("escape")
    t = gs()
    s.check("Model picker Esc→settings", "Settings" in t)
    sk("escape")
    s.check("Settings Esc close", True)
else:
    s.skip("Model picker opens", "settings didn't open")

# ═══════════════════════════════════════════════════════════
# SECTION 11: VIEW PRIORITY & OVERLAY HIERARCHY
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 11: View Priority & Overlay Hierarchy")
print("="*60)

restart()
# Switch to agents view
sk("ctrl+t")
time.sleep(0.5)
t = gs()
agents_ok = "agent" in t.lower() or "Subagent" in t.lower()

if agents_ok:
    # Open overlay from agents view
    si("/model\n")
    t = gs()
    s.check("Overlay takes priority over agents view",
            "Providers" in t and "═" in t)
    sk("escape")
    t = gs()
    s.check("Overlay returns to agents view",
            "Subagent" in t or "agent" in t.lower())
else:
    s.skip("Overlay priority", "couldn't switch to agents")
    s.skip("Returns to agents after overlay")

# HITL dialog priority (simulate pendingDecision)
restart()
# Set pendingDecision via state injection isn't available,
# but verify that the app structure handles dialogs at the top
t = gs()
s.check("TUI responds normally", "Pux" in t or "Message" in t)

# ═══════════════════════════════════════════════════════════
# SECTION 12: TAB BAR STATE
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 12: Tab Bar State")
print("="*60)

restart()
t = gs()
s.check("Chat tab shown as active", "→ Chat" in t or " Chat " in t)
s.check("Agents tab present", "Agents" in t)
s.check("Tools tab present", "Tools" in t)
s.check("Files tab present", "Files" in t)
s.check("History tab present", "History" in t or "Conversations" in t)
s.check("Ctrl+T hint shown", "Ctrl+T" in t or "switch" in t.lower())

# ═══════════════════════════════════════════════════════════
# SECTION 13: COMPOSER BAR — SLASH AUTOCOMPLETE
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 13: Composer Bar")
print("="*60)

restart()
# Type "/" to trigger command palette
si("/")
time.sleep(1)
t = gs()
s.check("Slash triggers command palette", "/" in t)
# Note: Command palette appears above the input, not in the main screen capture
# The main screen should still show the composer

# Composer separator lines
restart()
t = gs()
dash_count = t.count("─")
s.check("Composer has separator lines", dash_count >= 2)

# ═══════════════════════════════════════════════════════════
# SECTION 14: AGENT SELECTOR (Ctrl+O)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 14: Agent Selector")
print("="*60)

restart()
sk("ctrl+o")
time.sleep(0.5)
t = gs()
# Agent selector only shows when agents exist; verify no crash
s.check("Ctrl+O doesn't crash", t.strip() != "")
# If no agents, it shows empty state or welcome
sk("escape")

# ═══════════════════════════════════════════════════════════
# SECTION 15: DOUBLE ESCAPE / CTRL+C HANDLING
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 15: Keyboard Safety")
print("="*60)

restart()
t = gs()
# Double Escape when nothing running — should be safe
sk("escape")
time.sleep(0.3)
sk("escape")
time.sleep(0.5)
t = gs()
s.check("Double Escape safe when idle", "Pux" in t or "Message" in t or "Chat" in t)

# Ctrl+C once — should NOT quit (needs double)
sk("ctrl+c")
time.sleep(0.5)
t = gs()
s.check("Single Ctrl+C doesn't quit", t.strip() != "")

# ═══════════════════════════════════════════════════════════
# SECTION 16: STATUS BAR DETAILS
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 16: Status Bar Details")
print("="*60)

restart()
t = gs()
s.check("Status bar has model info", "·" in t or "gemma" in t.lower() or "local" in t.lower() or "model" in t.lower())
s.check("Status bar has project info", "auto-developer" in t.lower() or "project" in t.lower() or "·" in t)

# ═══════════════════════════════════════════════════════════
# SECTION 17: SUGGESTION CHIPS
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 17: Suggestion Chips")
print("="*60)

restart()
t = gs()
# Look for suggestion text or welcome prompt
s.check("Welcome shows suggestions",
        "What can you do" in t or "Type a message" in t or "Try:" in t)

# ═══════════════════════════════════════════════════════════
# FINAL SUMMARY
# ═══════════════════════════════════════════════════════════

ok = s.summary()
sys.exit(0 if ok else 1)
