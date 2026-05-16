#!/usr/bin/env python3
"""
Pux TUI End-to-End Test Suite
==============================
Tests all slash commands, overlays, views, and key features via the visual
testing server on port 9877.

Usage:
  task tui-visual   # start the server first
  python3 tests/e2e_tui.py

Requires: requests (pip install requests)
"""

import sys
import time
import requests
from dataclasses import dataclass, field

BASE = "http://localhost:9877"

# ── Helpers ──────────────────────────────────────────────

def gs():
    """Get screen text."""
    return requests.get(f"{BASE}/screen").json()["screen"]

def is_overlay_open():
    """Check if an overlay (providers, settings, etc.) is currently shown."""
    t = gs()
    return "═" in t  # Overlays use double-line rules

def is_normal_mode():
    """Check if TUI is in vim NORMAL mode."""
    t = gs()
    return "NORMAL" in t

def si(text, wait=5):
    """Send input text. Ensures we're in INSERT mode first.
    Only enters INSERT when NOT in an overlay (overlays handle input directly)."""
    if not is_overlay_open() and is_normal_mode():
        # In vim NORMAL mode — press 'i' to enter INSERT
        requests.post(f"{BASE}/input", json={"text": "i", "wait": 0.3})
        time.sleep(0.15)
    requests.post(f"{BASE}/input", json={"text": text, "wait": wait})
    time.sleep(1.5)

def sk(key):
    """Send special key."""
    requests.post(f"{BASE}/key", json={"key": key})
    time.sleep(0.5)

def restart():
    """Restart the TUI process to get a clean state."""
    requests.post(f"{BASE}/restart")
    time.sleep(6)
    # Wait until the TUI is responsive
    for _ in range(15):
        try:
            t = gs()
            if "Pux" in t or "Message" in t:
                time.sleep(1)  # Extra settle time after responsive
                return t
        except:
            pass
        time.sleep(1)
    return gs()

def soft_reset():
    """Try to return to chat with Escapes. Only use between tests
    that don't need a perfectly clean state."""
    for _ in range(3):
        sk("escape")
    time.sleep(0.3)

def print_screen(label, t=None):
    """Print screen for debug visibility."""
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
        icon = "✅" if condition else "❌"
        msg = f"  {icon} {name}"
        if not condition and detail:
            msg += f" — {detail}"
        print(msg)
        return condition

    def skip(self, name: str, reason: str = "dependency failed"):
        r = TestResult(name, False, f"Skipped: {reason}")
        self.results.append(r)
        print(f"  ⏭️  {name} (skipped: {reason})")
        return False

    def summary(self):
        p = sum(1 for r in self.results if r.passed)
        t = len(self.results)
        skipped = sum(1 for r in self.results if "Skipped" in r.detail)
        print(f"\n{'='*60}")
        print(f"RESULTS: {p}/{t} passed ({skipped} skipped)")
        print(f"{'='*60}")
        for r in self.results:
            icon = "✅" if r.passed else ("⏭️" if "Skipped" in r.detail else "❌")
            print(f"  {icon} {r.name}")
        return p == t - skipped  # skipped don't count as failures


s = Suite()


# ═══════════════════════════════════════════════════════════
# SECTION 1: BASIC STARTUP & CHAT VIEW
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 1: Startup & Chat View")
print("="*60)

t = gs()
s.check("TUI renders", "Pux" in t, "Screen empty or server down")
s.check("Welcome visible", "Type a message" in t or "Try:" in t)
s.check("Chat is default view", "Chat" in t)
s.check("Composer visible", "Message" in t or ">" in t)


# ═══════════════════════════════════════════════════════════
# SECTION 2: SLASH COMMANDS (non-overlay)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 2: Slash Commands")
print("="*60)

# /help
si("/help\n")
t = gs()
s.check("/help shows commands", "help" in t.lower() or "/" in t)

# /status
restart()
si("/status\n")
t = gs()
s.check("/status shows session info",
        "model" in t.lower() or "project" in t.lower() or "session" in t.lower()
        or "Project" in t or "gemma" in t.lower())

# /quit
restart()
si("/quit\n")
time.sleep(1)
t = gs()
s.check("/quit handled gracefully", t.strip() != "")


# ═══════════════════════════════════════════════════════════
# SECTION 3: VIEW SWITCHING
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 3: View Switching")
print("="*60)

restart()
# Ctrl+T cycles: chat→agents→tools→files→conversations→chat
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

# Direct slash commands
restart()
si("/agents\n")
t = gs()
s.check("/agents switches view", "agent" in t.lower() or "Agent" in t)

restart()
si("/tools\n")
t = gs()
s.check("/tools switches view", "tool" in t.lower() or "Tool" in t)

restart()
si("/conversations\n")
t = gs()
s.check("/conversations switches view", "onversation" in t or "chat" in t.lower())

restart()
si("/chat\n")
t = gs()
s.check("/chat returns to chat", "Message" in t or "Try:" in t)


# ═══════════════════════════════════════════════════════════
# SECTION 4: PROVIDERS OVERLAY (/model)
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
    s.check("Provider descriptions", "Cloud" in t or "Local" in t or "Aggregator" in t)
    s.check("Provider names", "gemini" in t or "llamacpp" in t or "openrouter" in t)
    s.check("Add provider option", "Add provider" in t or "+ Add" in t)
    s.check("Visual rules (═)", t.count("═") > 10)
    s.check("Navigation hints", "navigate" in t.lower() or "↑↓" in t)

    # Provider detail (Screen B)
    sk("down")  # → llamacpp
    si("\n")
    t = print_screen("Provider Detail")
    s.check("Detail: base URL", "localhost" in t or "http" in t)
    s.check("Detail: model info", "ctx" in t.lower() or "out" in t.lower())
    s.check("Detail: back hint", "back" in t.lower())

    # Model select → return to chat
    si("\n")
    t = gs()
    s.check("Model select → chat",
            ("Message" in t or "Chat" in t) and "Providers" not in t)

    # Catalog picker (Screen C)
    restart()
    si("/model\n")
    t = gs()
    if "Providers" in t:
        si("a")
        t = print_screen("Catalog Picker")
        s.check("Catalog: select prompt", "Select a provider" in t or "select" in t.lower())
        s.check("Catalog: known providers", "openai" in t.lower() or "anthropic" in t.lower())
        s.check("Catalog: custom option", "custom" in t.lower() or "Custom" in t)

        # Config form (Screen D)
        si("\n")
        t = print_screen("Config Form")
        s.check("Config: form fields", "Base URL" in t or "API Key" in t or "Model ID" in t)
        s.check("Config: title", "Configure" in t or "configure" in t.lower())

        # Escape: config → add → list → chat
        sk("escape")  # config → add
        t1 = gs()
        c2a = "Select a provider" in t1 or "custom" in t1.lower()
        sk("escape")  # add → list
        t2 = gs()
        a2l = "Providers" in t2 or "Add provider" in t2
        sk("escape")  # list → chat
        t3 = gs()
        l2c = ("Message" in t3 or "Chat" in t3) and "Providers" not in t3
        s.check("Escape: config→add→list→chat", c2a and a2l and l2c,
                f"c→a:{c2a} a→l:{a2l} l→c:{l2c}")
    else:
        for n in ["Catalog: select prompt", "Catalog: known providers",
                  "Catalog: custom option", "Config: form fields",
                  "Config: title", "Escape: config→add→list→chat"]:
            s.skip(n, "overlay didn't re-open")
else:
    for n in ["Provider descriptions", "Provider names", "Add provider option",
              "Visual rules (═)", "Navigation hints", "Detail: base URL",
              "Detail: model info", "Detail: back hint", "Model select → chat",
              "Catalog: select prompt", "Catalog: known providers",
              "Catalog: custom option", "Config: form fields",
              "Config: title", "Escape: config→add→list→chat"]:
        s.skip(n, "overlay didn't open")


# ═══════════════════════════════════════════════════════════
# SECTION 5: SETTINGS OVERLAY (/settings)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 5: Settings Overlay")
print("="*60)

restart()
si("/settings\n")
t = print_screen("Settings")

settings_open = "Settings" in t or "Theme" in t or "theme" in t.lower()
s.check("/settings opens overlay", settings_open)

if settings_open:
    s.check("Settings: model info", "model" in t.lower() or "Model" in t)
    s.check("Settings: theme option", "theme" in t.lower() or "Theme" in t)
    sk("escape")
    t = gs()
    s.check("Settings closes on Esc",
            ("Message" in t or "Chat" in t) and "Settings" not in t)
else:
    s.skip("Settings: model info")
    s.skip("Settings: theme option")
    s.skip("Settings closes on Esc")


# ═══════════════════════════════════════════════════════════
# SECTION 6: SEARCH OVERLAY (/search)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 6: Search Overlay")
print("="*60)

restart()
si("/search\n")
t = print_screen("Search")

search_open = "Search" in t or "search" in t.lower()
s.check("/search opens overlay", search_open)

if search_open:
    sk("escape")
    t = gs()
    s.check("Search closes on Esc",
            ("Message" in t or "Chat" in t) and "Search" not in t)
else:
    s.skip("Search closes on Esc")


# ═══════════════════════════════════════════════════════════
# SECTION 7: LOG VIEWER (/logs)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 7: Log Viewer")
print("="*60)

restart()
si("/logs\n")
t = print_screen("Logs")

logs_open = "Log" in t or "Diagnostics" in t
s.check("/logs opens overlay", logs_open)

if logs_open:
    sk("escape")
    t = gs()
    s.check("Logs closes on Esc",
            ("Message" in t or "Chat" in t) and "Log" not in t)
else:
    s.skip("Logs closes on Esc")


# ═══════════════════════════════════════════════════════════
# SECTION 8: SESSION SWITCHER (/sessions)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 8: Session Switcher")
print("="*60)

restart()
si("/sessions\n")
t = print_screen("Sessions")

sessions_open = "Session" in t or "Conversation" in t
s.check("/sessions opens overlay", sessions_open)

if sessions_open:
    sk("escape")
    t = gs()
    s.check("Sessions closes on Esc",
            ("Message" in t or "Chat" in t) and "Session" not in t)
else:
    s.skip("Sessions closes on Esc")


# ═══════════════════════════════════════════════════════════
# SECTION 9: FILE PICKER (/files overlay)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 9: File Picker Overlay")
print("="*60)

restart()
si("/files\n")
t = print_screen("File Picker")

files_open = "File" in t or "Pick" in t or "Browse" in t or "path" in t.lower()
s.check("/files opens file picker", files_open)

if files_open:
    sk("escape")
    t = gs()
    s.check("File picker closes on Esc",
            ("Message" in t or "Chat" in t) and "File" not in t)
else:
    s.skip("File picker closes on Esc")


# ═══════════════════════════════════════════════════════════
# SECTION 10: MCP OVERLAY (/mcp)
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 10: MCP Overlay")
print("="*60)

restart()
si("/mcp\n")
t = print_screen("MCP")

mcp_open = "MCP" in t or "Server" in t or "mcp" in t.lower()
s.check("/mcp opens overlay", mcp_open)

if mcp_open:
    sk("escape")
    t = gs()
    s.check("MCP closes on Esc",
            ("Message" in t or "Chat" in t) and "MCP" not in t)
else:
    s.skip("MCP closes on Esc")


# ═══════════════════════════════════════════════════════════
# SECTION 11: INVALID COMMANDS
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 11: Invalid Commands")
print("="*60)

restart()
si("/models\n")
t = gs()
s.check("/models rejected", "Providers" not in t)

restart()
si("/providers\n")
t = gs()
s.check("/providers rejected", "Providers" not in t)

restart()
si("/xyz\n")
t = gs()
s.check("/xyz doesn't crash", t.strip() != "")


# ═══════════════════════════════════════════════════════════
# SECTION 12: OVERLAY PRIORITY
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 12: Overlay Priority")
print("="*60)

restart()
# Use Ctrl+T to switch to agents view (more reliable than /agents)
sk("ctrl+t")  # → Agents
time.sleep(0.5)
t = gs()
agents_ok = "agent" in t.lower() or "Agent" in t or "Subagent" in t
if agents_ok:
    # Go back to chat to type the command (agents view has no composer)
    si("/chat\n")
    time.sleep(0.5)
    # Now open providers overlay
    si("/model\n")
    t = gs()
    s.check("Overlay takes priority over view",
            "Providers" in t and "═" in t)
    sk("escape")
    t = gs()
    # After closing overlay, should be in chat (previous view)
    s.check("Returns to previous view", "Chat" in t or "Message" in t)
else:
    s.skip("Overlay takes priority over view", "couldn't switch to agents")
    s.skip("Returns to previous view")


# ═══════════════════════════════════════════════════════════
# SECTION 13: HISTORY COMMAND
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 13: History Command")
print("="*60)

restart()
si("/history\n")
t = gs()
s.check("/history shows output",
        "conversation" in t.lower() or "conversations" in t.lower()
        or "No " in t or "recent" in t.lower() or "history" in t.lower()
        or "msgs" in t)


# ═══════════════════════════════════════════════════════════
# SECTION 14: STATUS BAR
# ═══════════════════════════════════════════════════════════
print("\n" + "="*60)
print("SECTION 14: Status Bar")
print("="*60)

restart()
t = gs()
s.check("Status bar visible", "Chat" in t or "·" in t)
s.check("Model in status bar", "gemma" in t.lower() or "local" in t.lower()
        or "llamacpp" in t.lower() or "·" in t)


# ═══════════════════════════════════════════════════════════
# FINAL SUMMARY
# ═══════════════════════════════════════════════════════════

ok = s.summary()
sys.exit(0 if ok else 1)
