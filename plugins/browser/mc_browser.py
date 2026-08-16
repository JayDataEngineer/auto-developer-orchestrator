#!/usr/bin/env python3
"""MCP browser server — full SeleniumBase CDP Mode + UC stack as MCP tools.

Architecture: Pattern 2 (one MCP server per agent).
  - Each agent that wants browser spawns `docker exec -i sandbox mc_browser.py`
  - The MCP client connects over stdio (JSON-RPC)
  - This server owns ONE Chrome instance for its lifetime
  - When the agent disconnects (stdin EOF), the server exits, Chrome dies with it

Chrome lifecycle: this script spawns Chrome via ``subprocess.Popen`` as a
child of THIS Python process, with full fingerprint-masking flags (ported
from sb_server.py). When this process exits, Chrome dies with it — proper
PID ownership, no orphan helpers, no fuser/ss kill-stale, no PID cgroup trips.

Stealth: Pure CDP Mode (``sb_cdp.Chrome``) — NO chromedriver process ever
runs. Stealthier than ``SB(uc=True)`` (UC Mode) which starts chromedriver
(modified to avoid detection) then disconnects it; the chromedriver process
+ port remain observable to advanced bot detection.

Tool surface (all sb_server.py endpoints ported + run escape hatch).
"""
from __future__ import annotations

import contextlib
import glob
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
import traceback
from typing import Any


# ═══════════════════════════════════════════════════════════════════════════════
# Logging — stdout is RESERVED for MCP JSON-RPC. All logs go to stderr.
# ═══════════════════════════════════════════════════════════════════════════════

def log(*args, **kwargs):
    kwargs.setdefault("file", sys.stderr)
    kwargs.setdefault("flush", True)
    print(*args, **kwargs)


try:
    from fastmcp import FastMCP
except ImportError as e:
    log(f"FATAL: fastmcp not installed: {e}")
    sys.exit(1)

# Mock pyvirtualdisplay BEFORE importing SeleniumBase — prevents SeleniumBase
# from spawning a hidden Xvfb that shadows the VNC-visible DISPLAY=:99.
import types
class _NoopDisplay:
    def __init__(self, *a, **kw): pass
    def start(self): return self
    def stop(self): pass
for _mod_name in ("pyvirtualdisplay", "sbvirtualdisplay"):
    if _mod_name not in sys.modules:
        _m = types.ModuleType(_mod_name)
        _m.Display = _NoopDisplay
        sys.modules[_mod_name] = _m

if "DISPLAY" not in os.environ or not os.environ["DISPLAY"]:
    os.environ["DISPLAY"] = ":99"

try:
    import seleniumbase  # noqa: F401 — proves the dependency is installed
except ImportError as e:
    log(f"FATAL: seleniumbase not installed: {e}")
    sys.exit(1)

# Pure CDP Mode is imported lazily inside BrowserSession.start() — sb_cdp.Chrome
# prints a banner to stdout on attach that would corrupt MCP stdio; we redirect
# stdout→stderr ONLY during the attach call there.


# ═══════════════════════════════════════════════════════════════════════════════
# JS bridges — IIFE bodies WITHOUT the trailing invocation parens. The
# _call_js helper appends `({json-args})` after the body.
# ═══════════════════════════════════════════════════════════════════════════════

MAX_TEXT = 4000
MAX_IMAGES = 50
MAX_LINKS = 30
MAX_ELEMENTS = 50

SOM_LABELER_JS = r"""
(() => {
    const existing = document.getElementById('__sb_label_overlay__');
    if (existing) existing.remove();
    const overlay = document.createElement('div');
    overlay.id = '__sb_label_overlay__';
    overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;pointer-events:none;z-index:999999;';
    const elements = [];
    const INTERACTIVE = 'a[href], button, input:not([type="hidden"]), select, textarea, ' +
        '[role="button"], [role="link"], [role="textbox"], [role="combobox"], ' +
        '[role="checkbox"], [role="radio"], [role="tab"], [role="menuitem"], ' +
        '[role="option"], [role="searchbox"], [role="switch"], ' +
        '[onclick], [contenteditable="true"], summary, details, ' +
        '[type="submit"], [type="button"], [type="reset"]';
    let id = 1;
    const seen = new Set();
    const vw = window.innerWidth, vh = window.innerHeight;
    function isVisible(el) {
        try {
            if (el.checkVisibility) return el.checkVisibility({checkOpacity:true, checkVisibilityCSS:true});
            const style = getComputedStyle(el);
            if (style.display === 'none') return false;
            if (style.visibility === 'hidden' || style.visibility === 'collapse') return false;
            if (parseFloat(style.opacity) === 0) return false;
            return true;
        } catch(e) { return false; }
    }
    function buildSelector(el) {
        if (el.id) return '#' + CSS.escape(el.id);
        const parts = [];
        let current = el;
        while (current && current !== document.body && current !== document.documentElement && parts.length < 3) {
            const tag = current.tagName.toLowerCase();
            if (current.id) { parts.unshift('#' + CSS.escape(current.id)); break; }
            const parent = current.parentElement;
            if (!parent) break;
            const siblings = Array.from(parent.children).filter(c => c.tagName === current.tagName);
            if (siblings.length === 1) parts.unshift(tag);
            else { const idx = siblings.indexOf(current) + 1; parts.unshift(tag + ':nth-of-type(' + idx + ')'); }
            current = parent;
        }
        return parts.join(' > ') || el.tagName.toLowerCase();
    }
    function processElement(el) {
        if (id > 50) return false;
        if (!isVisible(el)) return true;
        const rect = el.getBoundingClientRect();
        if (rect.width < 4 || rect.height < 4) return true;
        if (rect.bottom < -200 || rect.top > vh + 200) return true;
        const sel = buildSelector(el);
        if (seen.has(sel)) return true;
        seen.add(sel);
        const tag = el.tagName.toLowerCase();
        let text = '';
        if (tag === 'input' || tag === 'textarea') text = el.placeholder || el.value || el.name || el.getAttribute('aria-label') || el.type || '';
        else if (tag === 'select') text = el.name || el.getAttribute('aria-label') || '';
        else text = (el.textContent || '').trim();
        text = text.substring(0, 80).replace(/\s+/g, ' ');
        if (!text && tag !== 'input' && tag !== 'select' && tag !== 'textarea' && tag !== 'button') return true;
        const label = document.createElement('div');
        label.style.cssText = 'position:fixed;pointer-events:none;background:rgba(220,38,38,0.88);color:white;font-size:11px;font-family:monospace;padding:1px 5px;border-radius:3px;z-index:1000000;line-height:1.3;white-space:nowrap;font-weight:bold;text-shadow:0 1px 2px rgba(0,0,0,0.5);';
        label.textContent = '' + id;
        label.style.left = Math.max(0, rect.left - 1) + 'px';
        label.style.top = Math.max(0, rect.top - 1) + 'px';
        overlay.appendChild(label);
        const box = document.createElement('div');
        box.style.cssText = 'position:fixed;pointer-events:none;border:2px solid rgba(220,38,38,0.5);z-index:999999;border-radius:2px;';
        box.style.left = rect.left + 'px'; box.style.top = rect.top + 'px';
        box.style.width = rect.width + 'px'; box.style.height = rect.height + 'px';
        overlay.appendChild(box);
        elements.push({index:id, tag:tag, text:text, selector:sel,
            x:Math.round(rect.left), y:Math.round(rect.top),
            w:Math.round(rect.width), h:Math.round(rect.height)});
        id++;
        return true;
    }
    try { const nodes = document.querySelectorAll(INTERACTIVE); for (let i=0;i<nodes.length;i++) { if (!processElement(nodes[i])) break; } } catch(e) {}
    document.body.appendChild(overlay);
    return JSON.stringify(elements);
})
"""

PAGE_STATS_JS = r"""
(() => {
    return JSON.stringify({
        viewport_w: window.innerWidth, viewport_h: window.innerHeight,
        page_w: document.documentElement.scrollWidth, page_h: document.documentElement.scrollHeight,
        scroll_x: window.scrollX, scroll_y: window.scrollY,
        max_scroll_y: document.documentElement.scrollHeight - window.innerHeight,
        pixels_above: window.scrollY,
        pixels_below: Math.max(0, document.documentElement.scrollHeight - window.innerHeight - window.scrollY),
        links: document.querySelectorAll('a[href]').length,
        images: document.querySelectorAll('img[src]').length,
        inputs: document.querySelectorAll('input, select, textarea').length,
        buttons: document.querySelectorAll('button, input[type="submit"], input[type="button"], [role="button"]').length,
        total_elements: document.querySelectorAll('*').length
    });
})
"""

OCCLUSION_CHECK_JS = r"""
((sel) => {
    const el = document.querySelector(sel);
    if (!el) return JSON.stringify({exists:false});
    const rect = el.getBoundingClientRect();
    const cx = rect.left + rect.width / 2;
    const cy = rect.top + rect.height / 2;
    const topEl = document.elementFromPoint(cx, cy);
    const occluded = topEl !== el && !el.contains(topEl);
    return JSON.stringify({exists:true, occluded:occluded, blocker: topEl ? topEl.tagName : null});
})
"""

CDP_TYPE_JS = r"""
((sel, text, clear) => {
    const el = document.querySelector(sel);
    if (!el) return JSON.stringify({ok:false, error:'element not found'});
    el.focus();
    if (clear) {
        el.value = '';
        el.dispatchEvent(new Event('input', {bubbles:true}));
        el.dispatchEvent(new Event('change', {bubbles:true}));
    }
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype, 'value'
    )?.set || Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype, 'value'
    )?.set;
    if (nativeInputValueSetter) nativeInputValueSetter.call(el, text);
    else el.value = text;
    el.dispatchEvent(new Event('input', {bubbles:true}));
    el.dispatchEvent(new Event('change', {bubbles:true}));
    return JSON.stringify({ok:true, value:el.value});
})
"""

DROPDOWN_OPTIONS_JS = r"""
((sel) => {
    const s = document.querySelector(sel);
    if (!s) return JSON.stringify({ok:false, error:'element not found'});
    if (s.tagName !== 'SELECT') return JSON.stringify({ok:false, error:'not a select element'});
    const opts = [];
    for (const opt of s.options) {
        opts.push({value:opt.value, text:opt.textContent.trim(), selected:opt.selected, index:opt.index});
    }
    return JSON.stringify({ok:true, options:opts, multiple:s.multiple, selected_count:s.selectedOptions.length});
})
"""

SELECT_DROPDOWN_JS = r"""
((sel, value, text) => {
    const s = document.querySelector(sel);
    if (!s) return JSON.stringify({ok:false, error:'element not found'});
    let found = false;
    for (const opt of s.options) {
        if (value !== undefined && opt.value === value) { opt.selected = true; found = true; break; }
        if (text !== undefined && opt.textContent.trim() === text) { opt.selected = true; found = true; break; }
    }
    if (found) {
        s.dispatchEvent(new Event('change', {bubbles:true}));
        s.dispatchEvent(new Event('input', {bubbles:true}));
    }
    return JSON.stringify({ok:found, value:s.value});
})
"""

ELEMENT_CENTER_JS = r"""
((sel) => {
    const el = document.querySelector(sel);
    if (!el) return JSON.stringify({ok:false, error:"element not found: " + sel});
    const r = el.getBoundingClientRect();
    return JSON.stringify({
        ok:true,
        x: r.left + r.width / 2,
        y: r.top + r.height / 2,
        left: r.left, top: r.top, width: r.width, height: r.height,
        draggable: el.draggable === true || el.getAttribute("draggable") === "true",
        tag: el.tagName.toLowerCase()
    });
})
"""

SIMULATE_DND_JS = r"""
((srcSel, dstSel) => {
    const src = document.querySelector(srcSel);
    const dst = document.querySelector(dstSel);
    if (!src) return JSON.stringify({ok:false, error:"source not found: " + srcSel});
    if (!dst) return JSON.stringify({ok:false, error:"target not found: " + dstSel});
    let dt = null;
    try { dt = new DataTransfer(); } catch (e) {
        try { dt = { data: {}, setData(k,v){this.data[k]=v;}, getData(k){return this.data[k]||"";} }; } catch (e2) {}
    }
    const sr = src.getBoundingClientRect();
    const dr = dst.getBoundingClientRect();
    const sx = sr.left + sr.width / 2, sy = sr.top + sr.height / 2;
    const dx = dr.left + dr.width / 2, dy = dr.top + dr.height / 2;
    const fired = [];
    const fire = (el, type, x, y) => {
        let e;
        try { e = new DragEvent(type, {bubbles:true, cancelable:true, dataTransfer:dt, clientX:x, clientY:y}); }
        catch (err) {
            e = document.createEvent("DragEvent");
            try { e.initDragEvent(type, true, true, window, 0, x, y, x, y, false, false, false, false, 0, dt); }
            catch (e3) { return; }
        }
        el.dispatchEvent(e);
        fired.push(type);
    };
    fire(src, "dragstart", sx, sy);
    fire(dst, "dragenter", dx, dy);
    fire(dst, "dragover", dx, dy);
    fire(dst, "drop", dx, dy);
    fire(src, "dragend", dx, dy);
    return JSON.stringify({ok:true, fired:fired, source:srcSel, target:dstSel});
})
"""

HOVER_JS = r"""
((sel, x, y) => {
    let el = null;
    if (sel) {
        el = document.querySelector(sel);
        if (!el) return JSON.stringify({ok:false, error:"element not found: " + sel});
        const r = el.getBoundingClientRect();
        x = r.left + r.width / 2; y = r.top + r.height / 2;
    } else {
        el = document.elementFromPoint(x, y) || document.body;
    }
    const mk = (type) => new MouseEvent(type, {bubbles:true, cancelable:true, view:window, clientX:x, clientY:y});
    el.dispatchEvent(mk("mouseover"));
    el.dispatchEvent(mk("mousemove"));
    el.dispatchEvent(mk("mouseenter"));
    el.dispatchEvent(mk("mousemove"));
    return JSON.stringify({ok:true, hovered:true, x:Math.round(x), y:Math.round(y), tag:el.tagName.toLowerCase()});
})
"""

PRESS_JS = r"""
((keys, sel) => {
    const MOD = {ctrl:"Control", control:"Control", alt:"Alt", shift:"Shift", cmd:"Meta", meta:"Meta", command:"Meta", win:"Meta"};
    const parts = String(keys).split("+").map(s => s.trim()).filter(Boolean);
    if (!parts.length) return JSON.stringify({ok:false, error:"no keys given"});
    const mods = {ctrlKey:false, shiftKey:false, altKey:false, metaKey:false};
    const FLAG = {Control:"ctrlKey", Alt:"altKey", Shift:"shiftKey", Meta:"metaKey"};
    let main = null;
    for (const p of parts) {
        const lk = p.toLowerCase();
        if (MOD[lk]) mods[FLAG[MOD[lk]]] = true;
        else main = p;
    }
    if (!main) main = parts[parts.length - 1];
    let target = sel ? document.querySelector(sel) : (document.activeElement || document.body);
    if (!target) return JSON.stringify({ok:false, error:"target not found: " + sel});
    try { target.focus(); } catch (e) {}
    const codeFor = (k) => {
        const codes = {Enter:["Enter","Enter",13], Escape:["Escape","Escape",27], Tab:["Tab","Tab",9],
            Backspace:["Backspace","Backspace",8], Delete:["Delete","Delete",46], " ":[" ","Space",32],
            ArrowUp:["ArrowUp","ArrowUp",38], ArrowDown:["ArrowDown","ArrowDown",40],
            ArrowLeft:["ArrowLeft","ArrowLeft",37], ArrowRight:["ArrowRight","ArrowRight",39],
            Home:["Home","Home",36], End:["End","End",35], PageUp:["PageUp","PageUp",33], PageDown:["PageDown","PageDown",34]};
        if (codes[k]) return {key:codes[k][0], code:codes[k][1], keyCode:codes[k][2]};
        if (k.length === 1) return {key:k, code:"Key" + k.toUpperCase(), keyCode:k.toUpperCase().charCodeAt(0)};
        return {key:k, code:k, keyCode:0};
    };
    const info = codeFor(main);
    const fired = [];
    for (const type of ["keydown", "keypress", "keyup"]) {
        target.dispatchEvent(new KeyboardEvent(type, {
            bubbles:true, cancelable:true, view:window,
            key:info.key, code:info.code, keyCode:info.keyCode, which:info.keyCode,
            ctrlKey:mods.ctrlKey, shiftKey:mods.shiftKey, altKey:mods.altKey, metaKey:mods.metaKey
        }));
        fired.push(type + ":" + info.key);
    }
    return JSON.stringify({ok:true, fired:fired, keys:keys});
})
"""

CLICK_AT_JS = r"""
((x, y, button, isDouble, isRight) => {
    x = Number(x) || 0; y = Number(y) || 0;
    const btn = isRight ? 2 : ((typeof button === "number") ? button : 0);
    const el = document.elementFromPoint(x, y) || document.body;
    const mk = (type, b, buttons) => el.dispatchEvent(new MouseEvent(type, {
        bubbles:true, cancelable:true, view:window, clientX:x, clientY:y,
        button:b, buttons:buttons, relatedTarget:null
    }));
    mk("mouseover", btn, 0); mk("mousemove", btn, 0); mk("mousedown", btn, btn + 1); mk("mouseup", btn, 0);
    if (isRight) {
        el.dispatchEvent(new MouseEvent("contextmenu", {bubbles:true, cancelable:true, view:window, clientX:x, clientY:y, button:2}));
        return JSON.stringify({ok:true, action:"contextmenu", x:Math.round(x), y:Math.round(y), tag:el.tagName.toLowerCase()});
    }
    mk("click", btn, 0);
    if (isDouble) { mk("mousedown", btn, btn+1); mk("mouseup", btn, 0); mk("dblclick", btn, 0); }
    return JSON.stringify({ok:true, action: isDouble ? "dblclick" : "click", x:Math.round(x), y:Math.round(y), tag:el.tagName.toLowerCase()});
})
"""

SCROLL_INTO_VIEW_JS = r"""
((sel) => {
    const el = document.querySelector(sel);
    if (!el) return JSON.stringify({ok:false, error:"element not found: " + sel});
    el.scrollIntoView({block:"center", inline:"center"});
    const r = el.getBoundingClientRect();
    return JSON.stringify({ok:true, x:Math.round(r.left + r.width/2), y:Math.round(r.top + r.height/2),
        in_view: r.top >= 0 && r.left >= 0 && r.bottom <= window.innerHeight && r.right <= window.innerWidth});
})
"""

EXTRACT_TEXT_JS = r"""
(() => {
    const body = document.body;
    if (!body) return JSON.stringify({text: ''});
    const block = document.createElement('div');
    block.innerHTML = body.innerHTML;
    block.querySelectorAll('script,style,noscript,svg').forEach(n => n.remove());
    const text = (block.textContent || '').replace(/\s+/g, ' ').trim().substring(0, 8000);
    const links = Array.from(document.querySelectorAll('a[href]')).slice(0, 30).map(a => ({
        text: (a.textContent || '').trim().substring(0, 80),
        url: a.href
    }));
    const images = Array.from(document.querySelectorAll('img[src]')).slice(0, 50).map(img => ({
        src: img.src, alt: img.alt || ''
    }));
    return JSON.stringify({text, links, images, title: document.title, url: location.href});
})
"""

A11Y_JS = r"""
(() => {
    const out = [];
    const tree = (el, depth) => {
        if (out.length > 100) return;
        if (depth > 8) return;
        const role = el.getAttribute && el.getAttribute('role');
        const name = (el.getAttribute && (el.getAttribute('aria-label') || el.getAttribute('alt'))) || (el.textContent || '').trim().substring(0, 80);
        if (role || name) {
            out.push({tag: el.tagName.toLowerCase(), role, name: name.substring(0, 80), depth});
        }
        if (el.children) {
            for (const c of el.children) tree(c, depth + 1);
        }
    };
    tree(document.body, 0);
    return JSON.stringify({nodes: out, count: out.length});
})
"""


# ═══════════════════════════════════════════════════════════════════════════════
# CDP Python helpers — copied verbatim from sb_server.py
# ═══════════════════════════════════════════════════════════════════════════════

def _trusted_cdp_drag(sb, x1, y1, x2, y2, steps=40, button="left"):
    """Trusted drag via CDP Input.dispatchMouseEvent."""
    import asyncio
    import mycdp as cdp
    async def _run():
        tab = sb.page
        btn = cdp.input_.MouseButton(button)
        held = 1 if button == "left" else 2
        await tab.send(cdp.input_.dispatch_mouse_event("mouseMoved", x=x1, y=y1))
        await asyncio.sleep(0.05)
        await tab.send(cdp.input_.dispatch_mouse_event(
            "mousePressed", x=x1, y=y1, button=btn, buttons=held, click_count=1))
        await asyncio.sleep(0.03)
        for i in range(1, steps + 1):
            t = i / steps
            await tab.send(cdp.input_.dispatch_mouse_event(
                "mouseMoved", x=x1 + (x2 - x1) * t, y=y1 + (y2 - y1) * t, buttons=held))
            await asyncio.sleep(0.005)
        await asyncio.sleep(0.03)
        await tab.send(cdp.input_.dispatch_mouse_event(
            "mouseReleased", x=x2, y=y2, button=btn, buttons=held, click_count=1))
    sb.loop.run_until_complete(_run())


def _trusted_cdp_click(sb, x, y, button="left", click_count=1):
    """Trusted click via CDP Input.dispatchMouseEvent."""
    import asyncio
    import mycdp as cdp
    async def _run():
        tab = sb.page
        btn = cdp.input_.MouseButton(button)
        held = 1 if button == "left" else 2
        await tab.send(cdp.input_.dispatch_mouse_event("mouseMoved", x=x, y=y))
        await asyncio.sleep(0.04)
        await tab.send(cdp.input_.dispatch_mouse_event(
            "mousePressed", x=x, y=y, button=btn, buttons=held, click_count=click_count))
        await asyncio.sleep(0.04)
        await tab.send(cdp.input_.dispatch_mouse_event(
            "mouseReleased", x=x, y=y, button=btn, buttons=held, click_count=click_count))
    sb.loop.run_until_complete(_run())


def _trusted_cdp_type(sb, text):
    """Trusted typing via CDP Input.insertText."""
    import asyncio
    import mycdp as cdp
    async def _run():
        tab = sb.page
        await tab.send(cdp.input_.insert_text(text))
    sb.loop.run_until_complete(_run())


# ═══════════════════════════════════════════════════════════════════════════════
# Browser session — owns ONE Chrome for the lifetime of this MCP server
# ═══════════════════════════════════════════════════════════════════════════════

def _pick_cdp_port() -> int:
    """Bind to port 0, get the assigned port, close. Race-tolerant."""
    import socket
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def _reap_orphan_stealth_dirs():
    """Reap /tmp/sb_stealth_* and /tmp/mcp_browser_profile_* dirs that have
    NO live Chrome process bound to them. Safe across concurrent mc_browser
    instances — only touches dirs whose owner Chrome has already died.

    SeleniumBase creates /tmp/sb_stealth_<rand>/ as its Chrome user-data-dir.
    On ungraceful exit (SIGKILL, OOM, host crash), these dirs leak. We reap
    them at startup and on exit to keep /tmp clean."""
    reaped = 0
    for pattern in ("/tmp/sb_stealth_*", "/tmp/mcp_browser_profile_*"):
        for d in glob.glob(pattern):
            if not os.path.isdir(d):
                continue
            # Check if any live chrome process references this dir
            try:
                result = subprocess.run(
                    ["pgrep", "-f", d],
                    capture_output=True, text=True, timeout=2.0,
                )
                if result.stdout.strip():
                    # Live process — skip
                    continue
            except (subprocess.TimeoutExpired, FileNotFoundError):
                # pgrep missing or timed out — skip to be safe
                continue
            try:
                shutil.rmtree(d, ignore_errors=True)
                reaped += 1
            except Exception:
                pass
    if reaped:
        log(f"[mc_browser] reaped {reaped} orphan profile dirs")


class BrowserSession:
    """Owns ONE Chrome via Pure CDP Mode (stealthiest available).

    Architecture (ported from sb_server.py for parity with the in-container
    HTTP path):

    1. Spawn Chrome ourselves via ``subprocess.Popen`` with full fingerprint-
       masking flags (``--disable-blink-features=AutomationControlled``,
       ``--disable-features=...`` for ~12 leaky Chrome features, random viewport,
       unique user-data-dir). Chrome is a CHILD of this Python process → dies
       with us, no orphan helpers, no kill-stale.

    2. Wait for the CDP port to accept connections (poll up to 20s).

    3. Attach ``sb_cdp.Chrome("about:blank", host="127.0.0.1", port=cdp_port)``
       to the already-running Chrome. Pure CDP Mode = no chromedriver process
       ever runs = stealthier than ``SB(uc=True)`` (which starts chromedriver
       and disconnects it — the chromedriver process + its port are still
       observable to advanced bot detection).

    Comparison (per SeleniumBase docs):
    - ``SB(uc=True)`` → UC Mode → modified chromedriver, disconnects/reconnects
      strategically. Chromedriver PROCESS runs at startup.
    - ``sb_cdp.Chrome()`` → Pure CDP Mode → NO chromedriver, ever. All browser
      actions via CDP. The stealthy side of the "CDP Mode vs WebDriver" docs
      framing.
    """

    def __init__(self):
        self.sb: Any = None
        self._chrome_process: subprocess.Popen | None = None
        self._cdp_port: int = 0
        self._user_data_dir: str = ""
        self._started = False
        self._lock = threading.Lock()

    def _build_chrome_args(self) -> list[str]:
        """Chrome launch args — fingerprint-masking flags, random viewport,
        unique user-data-dir. Ported verbatim from sb_server.py."""
        import random
        # Chrome binary: env override wins, then the usual suspects. The
        # original container shipped google-chrome-stable; plugin deployments
        # point MC_BROWSER_CHROME at whatever the host has.
        chrome_bin = (
            os.environ.get("MC_BROWSER_CHROME")
            or shutil.which("google-chrome-stable")
            or shutil.which("google-chrome")
            or shutil.which("chromium-browser")
            or shutil.which("chromium")
            or "/usr/bin/google-chrome-stable"
        )
        base_w, base_h = 1280, 720
        w = base_w + random.randint(-80, 80)
        h = base_h + random.randint(-40, 40)
        args = [
            chrome_bin,
            f"--window-size={w},{h}",
            "--no-first-run",
            "--no-default-browser-check",
            "--disable-gpu",
            "--no-sandbox",
            "--disable-dev-shm-usage",
            f"--remote-debugging-port={self._cdp_port}",
            f"--user-data-dir={self._user_data_dir}",
            # ── Fingerprint masking (ported from sb_server.py) ───────────
            "--disable-blink-features=AutomationControlled",
            "--disable-features=ChromeWhatsNewUI,ChromeLabs,TranslateUI,"
            "InterestFeedContentSuggestions,ChromeWhatsNewHATS,"
            "EnableExtensionsExtensionsCheckup,OptimizationHints,"
            "MediaRouter,PasswordGeneration,PasswordsAccountStorage,"
            "AutofillServerCommunication",
            "--disable-sync",
            "--disable-default-apps",
            "--disable-background-networking",
            "--disable-component-update",
            "--disable-background-timer-throttling",
            "--disable-renderer-backgrounding",
            "--disable-field-trial-config",
            "--disable-ipc-flooding-protection",
            "--disable-search-engine-choice-screen",
            "--disable-dinosaur-easter-egg",
            # ── Visual consistency ───────────────────────────────────────
            "--enable-features=WebContentsForceDark",
        ]
        # Headless opt-in for display-less hosts (Xvfb :99 absent). UC/CDP
        # stealth holds under --headless=new.
        if os.environ.get("MC_BROWSER_HEADLESS") == "1":
            args.append("--headless=new")
        args.append("about:blank")
        return args

    def _wait_for_cdp_port(self, timeout_s: float = 20.0) -> bool:
        """Poll until the CDP port accepts connections."""
        import socket
        deadline = time.time() + timeout_s
        while time.time() < deadline:
            try:
                s = socket.socket()
                s.settimeout(1.0)
                s.connect(("127.0.0.1", self._cdp_port))
                s.close()
                return True
            except Exception:
                time.sleep(0.5)
        return False

    def _kill_chrome_process_tree(self):
        """Kill Chrome and all its helpers. Chrome ``setsid``s helpers out of
        the process group, so we walk by ppid (same approach as the
        ``_KILL_STALE_TEMPLATE`` in pux_harness/sandbox/tools/browser.py)."""
        if not self._chrome_process:
            return
        try:
            pid = self._chrome_process.pid
            # Recursive kill by ppid — kills Chrome and ALL its descendants
            # regardless of setsid. Uses ps + awk (busybox-safe; no fuser).
            kill_script = (
                "_kt() { "
                "  for c in $(ps -eo pid,ppid --noheaders | "
                "             awk -v P=$1 '$2==P{print $1}'); do "
                "    _kt $c; "
                "  done; "
                "  kill -9 $1 2>/dev/null; "
                "}; "
                f"_kt {pid}"
            )
            subprocess.run(
                ["bash", "-c", kill_script],
                capture_output=True, timeout=5,
            )
        except Exception as e:
            log(f"[mc_browser] process-tree kill error (Chrome may leak): {e}")
        # Reap the Popen handle
        try:
            self._chrome_process.wait(timeout=2)
        except Exception:
            pass
        self._chrome_process = None

    def start(self):
        with self._lock:
            if self._started:
                return
            # Reap any orphan stealth dirs from prior ungraceful exits BEFORE
            # we add our own. Keeps /tmp clean across crashes.
            _reap_orphan_stealth_dirs()
            # Pick a free port + unique user-data-dir
            self._cdp_port = _pick_cdp_port()
            self._user_data_dir = tempfile.mkdtemp(prefix="sb_stealth_")
            log(f"[mc_browser] starting Chrome (Pure CDP Mode) on CDP port "
                f"{self._cdp_port}, profile {self._user_data_dir}")
            # 1. Spawn Chrome with stealth flags
            args = self._build_chrome_args()
            self._chrome_process = subprocess.Popen(
                args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            )
            # 2. Wait for CDP port
            if not self._wait_for_cdp_port():
                log("[mc_browser] Chrome failed to start — CDP port not ready")
                self._kill_chrome_process_tree()
                shutil.rmtree(self._user_data_dir, ignore_errors=True)
                raise RuntimeError("Chrome failed to start (CDP port not ready)")
            # 3. Attach sb_cdp.Chrome (Pure CDP Mode — no chromedriver)
            try:
                from seleniumbase import sb_cdp
                with contextlib.redirect_stdout(sys.stderr):
                    self.sb = sb_cdp.Chrome(
                        "about:blank", host="127.0.0.1", port=self._cdp_port,
                    )
            except Exception as e:
                log(f"[mc_browser] sb_cdp.Chrome attach failed: {e}")
                self._kill_chrome_process_tree()
                shutil.rmtree(self._user_data_dir, ignore_errors=True)
                raise
            self._started = True
            log(f"[mc_browser] Chrome ready (Pure CDP Mode), "
                f"sb={type(self.sb).__name__}")

    def stop(self):
        with self._lock:
            if not self._started:
                return
            log("[mc_browser] stopping Chrome")
            # 1. Disconnect sb_cdp
            try:
                if self.sb:
                    with contextlib.redirect_stdout(sys.stderr):
                        self.sb.quit()
            except Exception as e:
                log(f"[mc_browser] sb.quit() error: {e}")
            # 2. Kill the Chrome process tree
            self._kill_chrome_process_tree()
            self._started = False
            self.sb = None
            # 3. Reap the user-data-dir
            if self._user_data_dir:
                try:
                    shutil.rmtree(self._user_data_dir, ignore_errors=True)
                    log(f"[mc_browser] reaped profile {self._user_data_dir}")
                except Exception as e:
                    log(f"[mc_browser] profile reap failed: {e}")
                self._user_data_dir = ""
            # 4. Reap orphan dirs from prior crashes
            _reap_orphan_stealth_dirs()

    def reset(self):
        """Kill and recreate the browser."""
        self.stop()
        time.sleep(0.5)
        self.start()


# ═══════════════════════════════════════════════════════════════════════════════
# MCP server + tools
# ═══════════════════════════════════════════════════════════════════════════════

mcp = FastMCP("mc-browser")
session = BrowserSession()


def _ensure():
    """Lazy-start Chrome on first tool call."""
    if not session._started:
        session.start()


def _eval_js(js: str) -> Any:
    """Run JS in page, return parsed JSON result if string."""
    raw = session.sb.execute_script(js)
    if isinstance(raw, str):
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return {"raw": raw}
    return raw


def _call_js(body: str, *args) -> Any:
    """Invoke a JS IIFE with JSON-encoded args. `body` ends with `)` (closing
    the arrow function) but NOT the invocation parens — we append those."""
    parts = []
    for a in args:
        parts.append("null" if a is None else json.dumps(a))
    invocation = ",".join(parts)
    return _eval_js(f"{body}({invocation})")


def _capture_page_state() -> dict:
    """Snapshot: title, url, text, links, images, stats, elements."""
    sb = session.sb
    try:
        elements = _call_js(SOM_LABELER_JS) or []
    except Exception:
        elements = []
    try:
        stats = _call_js(PAGE_STATS_JS) or {}
    except Exception:
        stats = {}
    try:
        extract = _call_js(EXTRACT_TEXT_JS) or {}
    except Exception:
        extract = {}
    return {
        "title": extract.get("title") or sb.get_title(),
        "url": extract.get("url") or sb.get_current_url(),
        "text": (extract.get("text") or "")[:MAX_TEXT],
        "links": (extract.get("links") or [])[:MAX_LINKS],
        "images": (extract.get("images") or [])[:MAX_IMAGES],
        "stats": stats,
        "element_map": elements[:MAX_ELEMENTS],
    }


# ── Lifecycle ─────────────────────────────────────────────────────────────────

@mcp.tool()
def status() -> dict:
    """Check if the browser is alive."""
    try:
        _ensure()
        return {"ok": True, "alive": True,
                "url": session.sb.get_current_url(),
                "cdp_port": session._cdp_port}
    except Exception as e:
        return {"ok": False, "alive": False, "error": str(e)}


@mcp.tool()
def reset() -> dict:
    """Kill and recreate the browser (clears all state, cookies, tabs)."""
    session.reset()
    return {"ok": True, "url": session.sb.get_current_url()}


@mcp.tool()
def quit_browser() -> dict:
    """Quit the browser. (Usually stdin EOF does this automatically.)"""
    session.stop()
    return {"ok": True}


# ── Navigation ────────────────────────────────────────────────────────────────

@mcp.tool()
def navigate(url: str) -> dict:
    """Navigate to URL. Returns page snapshot."""
    _ensure()
    session.sb.goto(url)
    return {"ok": True, **_capture_page_state()}


@mcp.tool()
def read() -> dict:
    """Snapshot the current page WITHOUT navigating."""
    _ensure()
    return {"ok": True, **_capture_page_state()}


@mcp.tool()
def search(query: str) -> dict:
    """DuckDuckGo search."""
    _ensure()
    session.sb.goto(f"https://duckduckgo.com/?q={query}")
    return {"ok": True, **_capture_page_state()}


@mcp.tool()
def go_back() -> dict:
    """Go back in browser history."""
    _ensure()
    session.sb.go_back()
    return {"ok": True, **_capture_page_state()}


@mcp.tool()
def refresh() -> dict:
    """Refresh the current page."""
    _ensure()
    session.sb.refresh()
    return {"ok": True, **_capture_page_state()}


@mcp.tool()
def wait(seconds: float = 2.0) -> dict:
    """Explicit wait (max 30s)."""
    seconds = max(0, min(30, float(seconds)))
    time.sleep(seconds)
    return {"ok": True, "waited": seconds}


# ── Interaction ───────────────────────────────────────────────────────────────

@mcp.tool()
def click(selector: str, index: int = 0, trusted: bool = False) -> dict:
    """Click element by CSS selector. Occlusion-aware. trusted=True uses CDP
    isTrusted events (anti-bot-resilient)."""
    _ensure()
    sb = session.sb
    if index:
        elements = _call_js(SOM_LABELER_JS) or []
        if index > len(elements):
            return {"ok": False, "error": f"index {index} out of range (have {len(elements)})"}
        selector = elements[index - 1]["selector"]
    if trusted:
        center = _call_js(ELEMENT_CENTER_JS, selector) or {}
        if center.get("ok"):
            _trusted_cdp_click(sb, center["x"], center["y"])
        else:
            return {"ok": False, "error": f"element not found: {selector}"}
    else:
        try:
            sb.click(selector)
        except Exception:
            sb_result = _call_js(SCROLL_INTO_VIEW_JS, selector)
            _eval_js(f"const el = document.querySelector({json.dumps(selector)}); if (el) el.click();")
    return {"ok": True, "clicked": selector, **_capture_page_state()}


@mcp.tool()
def type_text(selector: str, text: str, clear: bool = True, trusted: bool = False) -> dict:
    """Type text into element. trusted=True uses CDP insertText (React-safe)."""
    _ensure()
    sb = session.sb
    if trusted:
        _call_js(CDP_TYPE_JS, selector, text, clear)
        # Also do a trusted CDP type for trusted-keypress sites
        _eval_js(f"const el = document.querySelector({json.dumps(selector)}); if (el) {{ el.focus(); if ({'true' if clear else 'false'}) el.value = ''; }}")
        _trusted_cdp_type(sb, text)
    else:
        sb.type(selector, text)
    return {"ok": True, "typed": text, **_capture_page_state()}


@mcp.tool()
def scroll(direction: str = "down", amount: int = 25) -> dict:
    """Scroll the page. direction=down|up, amount=steps."""
    _ensure()
    if direction == "up":
        session.sb.scroll_up(amount)
    else:
        session.sb.scroll_down(amount)
    return {"ok": True, **_capture_page_state()}


@mcp.tool()
def scroll_into_view(selector: str) -> dict:
    """Scroll element into the viewport."""
    _ensure()
    result = _call_js(SCROLL_INTO_VIEW_JS, selector)
    return {"ok": True, "result": result}


@mcp.tool()
def hover(selector: str | None = None, x: int = 0, y: int = 0) -> dict:
    """Hover over element (by selector) or coordinate (x, y)."""
    _ensure()
    result = _call_js(HOVER_JS, selector, x, y)
    return {"ok": True, "result": result}


@mcp.tool()
def press(keys: str, selector: str | None = None) -> dict:
    """Press keys/hotkey. E.g. 'Enter', 'Control+a', 'ArrowDown'."""
    _ensure()
    result = _call_js(PRESS_JS, keys, selector)
    return {"ok": True, "result": result}


@mcp.tool()
def click_at(x: int = 0, y: int = 0, button: int = 0, double: bool = False, right: bool = False) -> dict:
    """Click at coordinate. button=0 left, 2 right. double/right are variants."""
    _ensure()
    result = _call_js(CLICK_AT_JS, x, y, button, double, right)
    return {"ok": True, "result": result}


@mcp.tool()
def drag(strategy: str = "auto", source: str | None = None, target: str | None = None,
         x1: int = 0, y1: int = 0, x2: int = 0, y2: int = 0) -> dict:
    """Drag and drop. strategy=auto|html5|physics. source/target=CSS selectors
    OR use x1,y1,x2,y2 coordinates for physics drag."""
    _ensure()
    sb = session.sb
    if strategy in ("auto", "html5") and source and target:
        result = _call_js(SIMULATE_DND_JS, source, target)
        if (result or {}).get("ok") or strategy == "html5":
            return {"ok": True, "strategy": "html5", "result": result, **_capture_page_state()}
    if strategy in ("auto", "physics"):
        if source:
            center = _call_js(ELEMENT_CENTER_JS, source) or {}
            if not center.get("ok"):
                return {"ok": False, "error": f"source not found: {source}"}
            x1, y1 = int(center["x"]), int(center["y"])
        if target:
            center = _call_js(ELEMENT_CENTER_JS, target) or {}
            if not center.get("ok"):
                return {"ok": False, "error": f"target not found: {target}"}
            x2, y2 = int(center["x"]), int(center["y"])
        _trusted_cdp_drag(sb, x1, y1, x2, y2)
        return {"ok": True, "strategy": "physics", **_capture_page_state()}
    return {"ok": False, "error": f"unknown strategy: {strategy}"}


@mcp.tool()
def find_text(text: str) -> dict:
    """Scroll until text is visible on the page."""
    _ensure()
    sb = session.sb
    # XPath text search via JS injection
    _eval_js(
        "const it = document.evaluate("
        f"'//text()[contains(., {json.dumps(text)})]',"
        "document, null, XPathResult.ANY_TYPE, null);"
        "const node = it.iterateNext();"
        "if (node && node.parentElement) node.parentElement.scrollIntoView({block:'center'});"
    )
    return {"ok": True, **_capture_page_state()}


# ── Extraction ────────────────────────────────────────────────────────────────

@mcp.tool()
def get_title() -> dict:
    """Get current page title."""
    _ensure()
    return {"ok": True, "title": session.sb.get_title()}


@mcp.tool()
def get_url() -> dict:
    """Get current page URL."""
    _ensure()
    return {"ok": True, "url": session.sb.get_current_url()}


@mcp.tool()
def get_text(selector: str = "body") -> dict:
    """Get text content of element by CSS selector."""
    _ensure()
    return {"ok": True, "text": session.sb.get_text(selector)}


@mcp.tool()
def evaluate(code: str) -> dict:
    """Execute JavaScript in the page. Return result as string."""
    _ensure()
    result = session.sb.execute_script(code)
    return {"ok": True, "result": str(result)}


@mcp.tool()
def label() -> dict:
    """Number + box all interactive elements on the page (SoM labeling)."""
    _ensure()
    elements = _call_js(SOM_LABELER_JS) or []
    return {"ok": True, "elements": elements, "count": len(elements)}


@mcp.tool()
def interact() -> dict:
    """List interactive elements without drawing labels (faster than label)."""
    _ensure()
    elements = _call_js(SOM_LABELER_JS) or []
    # Strip the overlay after
    _eval_js("const ex = document.getElementById('__sb_label_overlay__'); if (ex) ex.remove();")
    return {"ok": True, "elements": elements, "count": len(elements)}


@mcp.tool()
def extract_images() -> dict:
    """List all images on the page."""
    _ensure()
    images = _eval_js(
        "const imgs = Array.from(document.querySelectorAll('img[src]')).slice(0, 50);"
        "return JSON.stringify(imgs.map(i => ({src: i.src, alt: i.alt || '', w: i.naturalWidth, h: i.naturalHeight})));"
    )
    if isinstance(images, dict) and "raw" in images:
        images = []
    return {"ok": True, "images": images}


@mcp.tool()
def a11y() -> dict:
    """Get the accessibility tree (role + name per node)."""
    _ensure()
    result = _call_js(A11Y_JS) or {}
    return {"ok": True, **result}


@mcp.tool()
def screenshot(name: str = "screenshot") -> dict:
    """Take a screenshot. Returns path + base64 PNG."""
    import base64
    _ensure()
    path = session.sb.save_screenshot(name)
    try:
        with open(path, "rb") as f:
            b64 = base64.b64encode(f.read()).decode("ascii")
        return {"ok": True, "path": path, "base64_png": b64, "size_bytes": len(b64) * 3 // 4}
    except Exception as e:
        return {"ok": True, "path": path, "error": f"could not read back: {e}"}


# ── Tabs ──────────────────────────────────────────────────────────────────────

@mcp.tool()
def tabs() -> dict:
    """List open tabs."""
    _ensure()
    try:
        return {"ok": True, "tabs": session.sb.get_tabs()}
    except Exception as e:
        return {"ok": True, "tabs": [], "error": str(e)}


@mcp.tool()
def new_tab(url: str | None = None) -> dict:
    """Open a new tab."""
    _ensure()
    session.sb.open_new_tab(url=url, switch_to=True)
    return {"ok": True, **_capture_page_state()}


@mcp.tool()
def switch_tab(index: int) -> dict:
    """Switch to tab by index."""
    _ensure()
    session.sb.switch_to_tab(index)
    return {"ok": True, **_capture_page_state()}


@mcp.tool()
def close_tab() -> dict:
    """Close the current tab."""
    _ensure()
    session.sb.close_active_tab()
    return {"ok": True, **_capture_page_state()}


# ── Forms ─────────────────────────────────────────────────────────────────────

@mcp.tool()
def dropdown_options(selector: str) -> dict:
    """List options in a <select> dropdown."""
    _ensure()
    result = _call_js(DROPDOWN_OPTIONS_JS, selector)
    return {"ok": True, "result": result}


@mcp.tool()
def select_dropdown(selector: str, value: str | None = None, text: str | None = None) -> dict:
    """Select dropdown option by value or visible text."""
    _ensure()
    result = _call_js(SELECT_DROPDOWN_JS, selector, value, text)
    return {"ok": True, "result": result}


@mcp.tool()
def upload(selector: str, file_path: str) -> dict:
    """Upload a file to an <input type='file'> element."""
    _ensure()
    session.sb.upload_file(file_path)
    return {"ok": True, "uploaded": file_path}


# ── State ─────────────────────────────────────────────────────────────────────

@mcp.tool()
def cookies(action: str = "get", name: str | None = None, value: str | None = None,
            domain: str | None = None, path: str = "/") -> dict:
    """Get/set/clear cookies. action=get|set|clear."""
    _ensure()
    sb = session.sb
    if action == "get":
        return {"ok": True, "cookies": sb.get_cookies()}
    if action == "set":
        sb.set_all_cookies([{"name": name, "value": value, "domain": domain, "path": path}])
        return {"ok": True, "set": name}
    if action == "clear":
        sb.clear_cookies()
        return {"ok": True, "cleared": True}
    return {"ok": False, "error": f"unknown action: {action}"}


@mcp.tool()
def storage(action: str = "get", key: str | None = None, value: str | None = None) -> dict:
    """Get/set localStorage. action=get|set|clear."""
    _ensure()
    if action == "get":
        if key:
            return {"ok": True, "value": session.sb.get_local_storage_item(key)}
        result = _eval_js("return JSON.stringify(Object.entries(localStorage));")
        return {"ok": True, "storage": result if isinstance(result, list) else []}
    if action == "set" and key:
        session.sb.set_local_storage_item(key, value)
        return {"ok": True, "set": key}
    if action == "clear":
        _eval_js("localStorage.clear();")
        return {"ok": True, "cleared": True}
    return {"ok": False, "error": f"unknown action: {action}"}


@mcp.tool()
def save_session(path: str) -> dict:
    """Save cookies + localStorage to a file."""
    _ensure()
    sb = session.sb
    data = {"cookies": sb.get_cookies(), "url": sb.get_current_url()}
    ls = _eval_js("return JSON.stringify(Object.entries(localStorage));")
    data["localStorage"] = ls if isinstance(ls, list) else []
    with open(path, "w") as f:
        json.dump(data, f, indent=2)
    return {"ok": True, "path": path, "cookies_count": len(data["cookies"])}


@mcp.tool()
def restore_session(path: str) -> dict:
    """Restore cookies + localStorage from a file."""
    _ensure()
    sb = session.sb
    with open(path) as f:
        data = json.load(f)
    sb.set_all_cookies(data.get("cookies", []))
    for k, v in data.get("localStorage", []):
        sb.set_local_storage_item(k, v)
    return {"ok": True, "restored_cookies": len(data.get("cookies", []))}


# ── Stealth / anti-bot ────────────────────────────────────────────────────────

@mcp.tool()
def solve_captcha() -> dict:
    """Click any visible CAPTCHA (Cloudflare Turnstile, hCaptcha, reCAPTCHA).

    In Pure CDP Mode (our default), ``sb.solve_captcha()`` uses CDP
    Input.dispatchMouseEvent to click the CAPTCHA — a ``isTrusted=true``
    event. If that doesn't work, falls back to ``sb.gui_click_captcha()``
    (PyAutoGUI physical mouse click via Xvfb) — the most robust path for
    advanced detections that watch for CDP-originated events."""
    _ensure()
    sb = session.sb
    tried = []
    # 1. Pure CDP solve (preferred — no PyAutoGUI dependency)
    try:
        sb.solve_captcha()
        tried.append("cdp")
        return {"ok": True, "solved": True, "method": "cdp", **_capture_page_state()}
    except Exception as cdp_err:
        tried.append(f"cdp_failed:{type(cdp_err).__name__}")
    # 2. PyAutoGUI physical click fallback (needs DISPLAY=:99 + pyautogui)
    try:
        sb.gui_click_captcha()
        tried.append("gui")
        return {"ok": True, "solved": True, "method": "gui", "tried": tried,
                **_capture_page_state()}
    except Exception as e:
        return {"ok": False, "error": str(e), "tried": tried, **_capture_page_state()}


@mcp.tool()
def accept_cookies() -> dict:
    """Dismiss GDPR/CCPA cookie banners via curated selectors."""
    _ensure()
    sb = session.sb
    selectors = [
        "button#onetrust-accept-btn-handler",
        "button[data-testid='cookie-policy-dialog-accept-button']",
        "button.sp_choice_type_11",
        "button.js-accept-cookies",
    ]
    for sel in selectors:
        try:
            sb.click_if_visible(sel, timeout=0.5)
            return {"ok": True, "clicked": sel}
        except Exception:
            continue
    return {"ok": False, "error": "no cookie banner found"}


@mcp.tool()
def warmup_history(urls: list[str] | None = None, dwell: float = 3.0) -> dict:
    """Visit benign sites to build fingerprint legitimacy."""
    _ensure()
    sb = session.sb
    urls = urls or ["https://google.com", "https://wikipedia.org", "https://github.com"]
    visited = []
    for u in urls:
        try:
            sb.goto(u)
            time.sleep(dwell)
            visited.append(u)
        except Exception:
            pass
    return {"ok": True, "visited": visited}


# ── Escape hatch ──────────────────────────────────────────────────────────────

@mcp.tool()
def run(code: str) -> dict:
    """Run arbitrary Python with the live sb object in scope. THE ESCAPE HATCH —
    use this for anything not covered by structured tools. Code runs in a
    namespace with: sb, json, os, time, session. Set `result = {...}` to return
    a custom dict.

    Example:
        run(code="sb.goto('https://example.com'); result = {'title': sb.get_title()}")
    """
    _ensure()
    namespace = {"sb": session.sb, "json": json, "os": os, "time": time, "session": session}
    try:
        exec(code, namespace)
    except Exception as e:
        return {"ok": False, "error": f"{type(e).__name__}: {e}",
                "traceback": traceback.format_exc()}
    if "result" in namespace and isinstance(namespace["result"], dict):
        return {"ok": True, **namespace["result"]}
    return {"ok": True, **_capture_page_state()}


# ═══════════════════════════════════════════════════════════════════════════════
# Lifecycle hooks — ensure Chrome dies with the server, no matter what
# ═══════════════════════════════════════════════════════════════════════════════

import atexit
atexit.register(session.stop)


def _signal_handler(signum, frame):
    log(f"[mc_browser] got signal {signum} — stopping Chrome")
    session.stop()
    sys.exit(0)


for _sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
    try:
        signal.signal(_sig, _signal_handler)
    except (ValueError, AttributeError, OSError):
        pass


if __name__ == "__main__":
    log(f"[mc_browser] pid={os.getpid()}, Pure CDP Mode, "
        f"profile will be assigned on start()")
    try:
        mcp.run(transport="stdio")
    except (KeyboardInterrupt, SystemExit):
        pass
    finally:
        log("[mc_browser] mcp.run() returned — shutting down")
        session.stop()
