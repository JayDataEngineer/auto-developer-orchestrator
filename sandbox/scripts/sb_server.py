#!/usr/bin/env python3
"""Persistent SeleniumBase browser server with SoM labeling and vision.

Runs as a supervised HTTP service inside the sandbox.
The agent sends commands via curl, browser state persists across calls.

Features:
    - SoM (Set-of-Marks) visual labeling: numbered boxes on interactive elements
    - Auto-screenshot after every page change → vision-in-the-loop
    - Page fingerprinting: detects when page didn't change after action
    - Occlusion-aware clicking with JS fallback
    - CDP-based character-by-character typing (React-safe)
    - Tab auto-detection after clicks
    - Dropdown/select support
    - Explicit wait action
    - Structured data extraction

Usage:
    sb_server.py [--port PORT] [--stealth]

Environment:
    SB_SERVER_PORT  Port to listen on (default: 9876)
    DISPLAY         X11 display (set by supervisord)

API — POST unless noted:
    /navigate           {"url":"..."}                    → page + SoM + screenshot
    /read               {}                               → page data (no re-navigation)
    /search             {"query":"..."}                  → DuckDuckGo search results
    /go_back            {}                               → back in history
    /refresh            {}                               → reload page
    /click              {"selector":"...","index":n}     → occlusion-aware click
    /type               {"selector":"...","text":"...","index":n,"submit":false,"clear":true} → CDP typing
    /scroll             {"direction":"down|up","amount":0}
    /label              {}                               → SoM labels on current page
    /interact           {}                               → interactive elements list
    /extract_images     {}                               → image URLs on current page
    /screenshot         {"path":"/tmp/shot.png"}         → save screenshot
    /download           {"url":"...","path":"..."}       → direct URL download
    /find_text          {"text":"..."}                   → scroll to text on page
    /evaluate           {"code":"..."}                   → execute JS, return result
    /run                {"code":"..."}                   → execute Python with `sb` loaded
    /tabs               {}                               → list open tabs
    /new_tab            {"url":"..."}                    → open new tab
    /switch_tab         {"index":n}                      → switch to tab
    /close_tab          {}                               → close current tab
    /dropdown_options   {"selector":"...","index":n}     → list <select> options
    /select_dropdown    {"selector":"...","index":n,"value":"...","text":"..."} → select option
    /wait               {"seconds":2}                    → explicit wait (max 30s)
    /extract            {"query":"..."}                  → extract structured data from page
    /cookies            {"action":"get|set|clear",...}   → manage cookies (HttpOnly-safe via CDP)
    /storage            {"action":"get|set|clear",...}   → manage localStorage
    /upload             {"selector":"...","file_path":"..."} → upload file to <input type="file">
    /a11y               {}                               → accessibility tree (role + name per node)
    /drag               {"strategy":"auto|html5|physics", from_*/to_*|dx/dy} → drag-and-drop (Phase 19)
    /hover              {"index|selector"|"x,y"}         → hover (reveal menus/tooltips)
    /press              {"keys":"Enter|Control+a|ArrowDown", "index|selector"?} → key / hotkey press
    /click_at           {"x,y"|"index|selector", button?, double?, right?} → coordinate / variant click
    /scroll_into_view   {"index|selector"}               → scroll element into the viewport
    /iframe             {"action":"list|enter|exit", selector?} → iframe traversal
    /save_session       {"path":"..."}                   → save cookies + localStorage to file
    /restore_session    {"path":"..."}                   → restore from saved session file
    /reset              {}                               → kill and recreate browser
    GET /status         {}                               → browser alive check
    GET /file/<path>    {}                               → serve local file as base64 data URI
"""

import sys
import types
import json
import os
import io
import hashlib
import time
import traceback
import threading
import re
import base64
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path

os.environ["SB_NO_BORING_RC"] = "1"

# ── Prevent SeleniumBase from creating a hidden Xvfb ──
# Mock sbvirtualdisplay (SeleniumBase's fork of pyvirtualdisplay) so that
# Chrome uses the existing DISPLAY (:99) which is VNC-visible.
# Combined with headed=True, xvfb=False in the SB/sb_cdp constructors.
class _NoopDisplay:
    def __init__(self, *a, **kw): pass
    def start(self): return self
    def stop(self): pass

for _mod_name in ("pyvirtualdisplay", "sbvirtualdisplay"):
    _m = types.ModuleType(_mod_name)
    _m.Display = _NoopDisplay
    sys.modules[_mod_name] = _m

if "DISPLAY" not in os.environ or not os.environ["DISPLAY"]:
    os.environ["DISPLAY"] = ":99"

MAX_TEXT = 4000
MAX_IMAGES = 50
MAX_LINKS = 30
MAX_ELEMENTS = 50
DEFAULT_PORT = 9876
SCREENSHOT_DIR = "/tmp"

# ═══════════════════════════════════════════════════════════════════════════════
# JavaScript Snippets
# ═══════════════════════════════════════════════════════════════════════════════

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
    const vw = window.innerWidth;
    const vh = window.innerHeight;

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
        const selector = buildSelector(el);
        if (seen.has(selector)) return true;
        seen.add(selector);
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

        elements.push({index:id, tag:tag, text:text, selector:selector,
            x:Math.round(rect.left), y:Math.round(rect.top),
            w:Math.round(rect.width), h:Math.round(rect.height)});
        id++;
        return true;
    }

    try { const nodes = document.querySelectorAll(INTERACTIVE); for (let i=0;i<nodes.length;i++) { if (!processElement(nodes[i])) break; } } catch(e) {}
    document.body.appendChild(overlay);
    return JSON.stringify(elements);
})()
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
})()
"""

# Occlusion check — returns true if element at selector is blocked by another element
OCCLUSION_CHECK_JS = r"""
((selector) => {
    const el = document.querySelector(selector);
    if (!el) return JSON.stringify({exists:false});
    const rect = el.getBoundingClientRect();
    const cx = rect.left + rect.width / 2;
    const cy = rect.top + rect.height / 2;
    const topEl = document.elementFromPoint(cx, cy);
    const occluded = topEl !== el && !el.contains(topEl);
    return JSON.stringify({exists:true, occluded:occluded, blocker: topEl ? topEl.tagName : null});
})
"""

# CDP-based character-by-character typing — dispatches proper keyboard events
CDP_TYPE_JS = r"""
((selector, text, clear) => {
    const el = document.querySelector(selector);
    if (!el) return JSON.stringify({ok:false, error:'element not found'});
    el.focus();
    if (clear) {
        el.value = '';
        el.dispatchEvent(new Event('input', {bubbles:true}));
        el.dispatchEvent(new Event('change', {bubbles:true}));
    }
    // Use native input setter to trigger React's change detection
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

# Dropdown options extraction
DROPDOWN_OPTIONS_JS = r"""
((selector) => {
    const sel = document.querySelector(selector);
    if (!sel) return JSON.stringify({ok:false, error:'element not found'});
    if (sel.tagName !== 'SELECT') return JSON.stringify({ok:false, error:'not a select element'});
    const opts = [];
    for (const opt of sel.options) {
        opts.push({value:opt.value, text:opt.textContent.trim(), selected:opt.selected, index:opt.index});
    }
    return JSON.stringify({ok:true, options:opts, multiple:sel.multiple, selected_count:sel.selectedOptions.length});
})
"""

# Select dropdown option by value or text
SELECT_DROPDOWN_JS = r"""
((selector, value, text) => {
    const sel = document.querySelector(selector);
    if (!sel) return JSON.stringify({ok:false, error:'element not found'});
    let found = false;
    for (const opt of sel.options) {
        if (value !== undefined && opt.value === value) { opt.selected = true; found = true; break; }
        if (text !== undefined && opt.textContent.trim() === text) { opt.selected = true; found = true; break; }
    }
    if (found) {
        sel.dispatchEvent(new Event('change', {bubbles:true}));
        sel.dispatchEvent(new Event('input', {bubbles:true}));
    }
    return JSON.stringify({ok:found, value:sel.value});
})
"""


# ── Drag / mouse / keyboard interaction (Phase 19 — SOTA browser agent) ──────
# These IIFEs mirror OCCLUSION_CHECK_JS / CDP_TYPE_JS: raw CDP Runtime.evaluate
# runs them unwrapped, so each is a single arrow-fn expression returning a JSON
# string. Selenium's native drag_and_drop() does NOT emit the HTML5
# dragstart/dragover/drop sequence (W3C WebDriver gap — Selenium issue #3604),
# so SIMULATE_DND_JS synthesizes that chain; PHYS_DRAG_JS covers custom
# draggables/sliders that listen to mousedown/mousemove/mouseup.

# Element center + draggable flag from a CSS selector.
ELEMENT_CENTER_JS = r"""
((selector) => {
    const el = document.querySelector(selector);
    if (!el) return JSON.stringify({ok:false, error:"element not found: " + selector});
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

# HTML5 drag-and-drop via synthetic DragEvent dispatch (works for SortableJS,
# react-dnd, dnd-kit, native draggable). Libraries track drag state internally
# and react to the event sequence rather than DataTransfer contents, so the
# chain fires even though the events are untrusted.
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
        try {
            e = new DragEvent(type, {bubbles:true, cancelable:true, dataTransfer:dt, clientX:x, clientY:y});
        } catch (err) {
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

# Mouse-physics drag for custom draggables / sliders / canvas that listen to
# mousedown→mousemove(N)→mouseup. Steps interpolates a straight path; callers
# wanting human-like curves can pre-compute waypoints. Untrusted MouseEvents —
# most custom widgets respond; anti-bot-hardened sites may not.
PHYS_DRAG_JS = r"""
((x1, y1, x2, y2, steps, button) => {
    button = (typeof button === "number") ? button : 0;
    steps = Math.max(2, (steps | 0) || 25);
    x1 = Number(x1) || 0; y1 = Number(y1) || 0; x2 = Number(x2) || 0; y2 = Number(y2) || 0;
    const fired = [];
    const fire = (type, x, y) => {
        const el = document.elementFromPoint(x, y) || document.body;
        const e = new MouseEvent(type, {
            bubbles:true, cancelable:true, view:window,
            clientX:x, clientY:y, button:button,
            buttons: type === "mouseup" ? 0 : (button === 2 ? 2 : 1),
            relatedTarget:null
        });
        el.dispatchEvent(e);
        fired.push(type + "(" + Math.round(x) + "," + Math.round(y) + ")");
    };
    fire("mouseover", x1, y1);
    fire("mousemove", x1, y1);
    fire("mousedown", x1, y1);
    for (let i = 1; i <= steps; i++) {
        const t = i / steps;
        fire("mousemove", x1 + (x2 - x1) * t, y1 + (y2 - y1) * t);
    }
    fire("mouseup", x2, y2);
    return JSON.stringify({ok:true, fired:fired, from:[x1,y1], to:[x2,y2], steps:steps});
})
"""

# Hover (mouseover/mousemove/mouseenter) over a selector OR raw x,y.
HOVER_JS = r"""
((selector, x, y) => {
    let el = null;
    if (selector) {
        el = document.querySelector(selector);
        if (!el) return JSON.stringify({ok:false, error:"element not found: " + selector});
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

# Key / hotkey press via synthetic KeyboardEvent. keys is "+"-joined, e.g.
# "Enter", "Escape", "Control+a", "ArrowDown", "Shift+ArrowDown". Dispatches
# keydown/keypress/keyup with modifier flags on the target (or activeElement).
PRESS_JS = r"""
((keys, selector) => {
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
    let target = selector ? document.querySelector(selector) : (document.activeElement || document.body);
    if (!target) return JSON.stringify({ok:false, error:"target not found: " + selector});
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

# Coordinate click with right-click / double-click variants.
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

# scrollIntoView a selector; reports the post-scroll center + in_view flag.
SCROLL_INTO_VIEW_JS = r"""
((selector) => {
    const el = document.querySelector(selector);
    if (!el) return JSON.stringify({ok:false, error:"element not found: " + selector});
    el.scrollIntoView({block:"center", inline:"center"});
    const r = el.getBoundingClientRect();
    return JSON.stringify({ok:true, x:Math.round(r.left + r.width/2), y:Math.round(r.top + r.height/2),
        in_view: r.top >= 0 && r.left >= 0 && r.bottom <= window.innerHeight && r.right <= window.innerWidth});
})
"""


# ═══════════════════════════════════════════════════════════════════════════════
# Browser State
# ═══════════════════════════════════════════════════════════════════════════════

class BrowserState:
    """Persistent SeleniumBase browser with labeler cache and fingerprinting."""

    def __init__(self, stealth=False):
        self.stealth = stealth
        self.sb = None
        self._ctx = None
        self._lock = threading.Lock()
        self._last_element_map = []
        self._download_dir = "/tmp/sb_downloads"
        self._last_fingerprint = ""
        os.makedirs(self._download_dir, exist_ok=True)
        self._init_browser()

    def _init_browser(self):
        self._close_browser()
        # pyvirtualdisplay is mocked at module level (noop), so SeleniumBase
        # cannot create a hidden Xvfb. Chrome uses DISPLAY from environment
        # (:99 by default), making browser automation visible via VNC.
        #
        # We use sb_cdp.Chrome (SeleniumBase Pure CDP Mode) for both stealth
        # and non-stealth modes. This is already CDP-based (no webdriver flag,
        # no detectable automation footprint).
        #
        # ── Single Chrome instance ──────────────────────────────────────────
        # The sandbox container runs ONE Chrome via supervisord (PID 43, CDP
        # port 9222). sb_server ATTACHES to that Chrome instead of launching a
        # separate one (sb_cdp.Chrome supports this via host+port parameters).
        # This ensures:
        #   1. VNC shows the browser the agent is using (same X display :99)
        #   2. No orphan Chrome processes on restart (sb_server disconnects,
        #      Chrome keeps running, sb_server reconnects next time)
        #   3. EnableBrowserMode (port 9222 check) keeps working
        #
        # Stray Chrome processes from previous sb_server versions (which use
        # user data dir /tmp/uc_*) are killed here so only ONE browser window
        # appears on the VNC screen.
        os.environ.setdefault("DISPLAY", ":99")
        import subprocess as _sp
        import time as _t
        for _pat in ("google-chrome", "chromium-browser", "chromium"):
            try:
                _sp.run(
                    ["pkill", "-9", "-f", "%s.*--user-data-dir=/tmp/uc_" % _pat],
                    capture_output=True, timeout=5,
                )
            except Exception:
                pass
        _t.sleep(0.5)

        try:
            from seleniumbase import sb_cdp
            # Attach to the supervisord Chrome (port 9222) instead of launching
            # a new instance. This way VNC shows the browser being automated.
            self.sb = sb_cdp.Chrome("about:blank", host="127.0.0.1", port=9222)
            self._ctx = None
            self._setup_cdp_downloads()
        except Exception as e:
            print(f"[sb_server] browser init failed: {e}", file=sys.stderr)
            import traceback as _tb
            _tb.print_exc(file=sys.stderr)
            self.sb = None
            self._ctx = None

    def _setup_cdp_downloads(self):
        try:
            downloads_path = str(Path(self._download_dir).absolute())
            # sb_cdp.Chrome uses a different driver; set download behavior via JS hook
            # that creates an <a> link with download attribute when downloads are initiated.
            # For SeleniumBase UC mode this would be sb.execute_cdp_cmd(...) but sb_cdp.Chrome
            # doesn't expose that. The /download endpoint handles explicit downloads via curl.
            self._download_dir = downloads_path
        except Exception as e:
            print(f"[sb_server] download dir setup failed (non-fatal): {e}", file=sys.stderr)

    def _close_browser(self):
        if self._ctx is not None:
            try: self._ctx.__exit__(None, None, None)
            except Exception: pass
            self._ctx = None
        if self.sb is not None:
            try: self.sb.driver.stop()
            except Exception: pass
            self.sb = None

    def reset(self):
        with self._lock:
            self._init_browser()
            self._last_element_map = []
            self._last_fingerprint = ""

    def ensure(self):
        if self.sb is None:
            self._init_browser()
            self._last_element_map = []
        return self.sb is not None

    def close(self):
        self._close_browser()

    @property
    def has_browser(self):
        return self.sb is not None

    def snapshot_before(self):
        """Capture current URL + element count as a fingerprint before an action."""
        if self.sb is None:
            return ""
        url = safe(lambda: self.sb.get_current_url() or "", "")
        try:
            count = self.sb.execute_script("return document.querySelectorAll('*').length")
            text_hash = hashlib.md5(safe(lambda: self.sb.get_text("body") or "", "").encode()).hexdigest()[:8]
            self._last_fingerprint = f"{url}|{count}|{text_hash}"
        except Exception:
            self._last_fingerprint = url
        return self._last_fingerprint

    def check_page_changed(self):
        """Compare current page to last snapshot. Returns page_changed boolean."""
        if not self._last_fingerprint:
            return True
        old = self._last_fingerprint
        new = self.snapshot_before()
        return old != new


# ═══════════════════════════════════════════════════════════════════════════════
# Helpers
# ═══════════════════════════════════════════════════════════════════════════════

def safe(fn, default=None):
    try: return fn()
    except Exception: return default

def ts(): return str(int(time.time()))
def now(): return time.strftime("%H:%M:%S")
def log(msg): print(f"[sb_server {now()}] {msg}", file=sys.stderr)
def screenshot_path(): return f"{SCREENSHOT_DIR}/sb_screenshot_{ts()}.png"

def run_labeler(sb, state):
    try:
        result = sb.execute_script(SOM_LABELER_JS)
        element_map = json.loads(result) if isinstance(result, str) else result
        state._last_element_map = element_map
        return element_map
    except Exception as e:
        log(f"labeler failed: {e}")
        return state._last_element_map or []

def run_page_stats(sb):
    try:
        result = sb.execute_script(PAGE_STATS_JS)
        return json.loads(result) if isinstance(result, str) else result
    except Exception as e:
        return {"error": str(e)}

def extract_page_data(sb):
    title = safe(lambda: sb.get_title() or "")
    url = safe(lambda: sb.get_current_url() or "")
    text = safe(lambda: sb.get_text("body") or "")
    if text and len(text) > MAX_TEXT:
        text = text[:MAX_TEXT] + "...[truncated]"

    images = []
    try:
        for img in sb.select_all("img[src]", timeout=3):
            src = img.get_attribute("src") or ""
            if src and not src.startswith("data:") and not src.startswith("blob:") and len(src) < 2000:
                alt = img.get_attribute("alt") or ""
                images.append({"src": src, "alt": alt[:100]})
                if len(images) >= MAX_IMAGES: break
    except Exception: pass

    links = []
    try:
        for a in sb.select_all("a[href]", timeout=3):
            href = a.get_attribute("href") or ""
            if href and not href.startswith("#") and not href.startswith("javascript:"):
                links.append({"text": (a.text or "").strip()[:100], "url": href})
                if len(links) >= MAX_LINKS: break
    except Exception: pass

    return {"title": title, "url": url, "text": text, "images": images, "links": links}

def find_element_by_index(state, index):
    for el in state._last_element_map:
        if el.get("index") == index:
            return el.get("selector")
    return None


def js_str(s):
    """Embed ``s`` as a single-quoted JS string literal for execute_script.
    Escapes backslash then single-quote — the only two chars that can break
    out of a single-quoted JS string."""
    return "'" + str(s).replace("\\", "\\\\").replace("'", "\\'") + "'"


# ═══════════════════════════════════════════════════════════════════════════════
# HTTP Handler
# ═══════════════════════════════════════════════════════════════════════════════

class Handler(BaseHTTPRequestHandler):
    state = None

    def log_message(self, fmt, *args):
        log(f"{self.command} {self.path}")

    def _read_body(self):
        length = int(self.headers.get("Content-Length", 0))
        if length == 0: return {}
        return json.loads(self.rfile.read(length))

    def _json_response(self, code, data):
        body = json.dumps(data, ensure_ascii=False).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)

    def _ok(self, data=None, **extra):
        resp = {"ok": True}
        if data: resp.update(data)
        resp.update(extra)
        self._json_response(200, resp)

    def _err(self, msg, code=500):
        self._json_response(code, {"ok": False, "error": str(msg)})

    def _resolve_selector(self, body):
        """Resolve selector from index or direct selector. Returns (selector, error_msg)."""
        selector = body.get("selector", "")
        index = body.get("index", 0)
        if index:
            selector = find_element_by_index(self.state, index)
            if not selector:
                return "", f"element index {index} not found in label map — call /read first"
        if not selector:
            return "", "missing selector or index"
        return selector, ""

    def _capture_with_fingerprint(self, sb, state):
        """Full capture + fingerprint comparison."""
        element_map = run_labeler(sb, state)
        stats = run_page_stats(sb)
        page_data = extract_page_data(sb)

        spath = screenshot_path()
        safe(lambda: sb.save_screenshot(spath), None)

        page_changed = state.check_page_changed()

        return {
            "page_data": page_data,
            "element_map": element_map,
            "screenshot_path": spath,
            "page_stats": stats,
            "page_changed": page_changed,
        }

    def _capture_tabs_info(self, sb):
        """Get current tab count and handle info."""
        try:
            return sb.driver.window_handles, sb.driver.current_window_handle
        except Exception:
            return [], None

    def _detect_and_handle_new_tab(self, sb, before_handles):
        """Check for new tabs after click. If new tab opened, switch to it. Returns info dict."""
        try:
            after_handles = sb.driver.window_handles
            new_handles = [h for h in after_handles if h not in before_handles]
            if new_handles:
                # Auto-switch to the new tab (browser-use behavior)
                sb.driver.switch_to.window(new_handles[-1])
                sb.sleep(1)
                return {"new_tab_opened": True, "total_tabs": len(after_handles)}
            return {"new_tab_opened": False, "total_tabs": len(after_handles)}
        except Exception:
            return {"new_tab_opened": False, "total_tabs": 0}

    # ── GET ─────────────────────────────────────────────────────────────────

    def do_GET(self):
        if self.path == "/status":
            alive = self.state.sb is not None
            url = safe(lambda: self.state.sb.get_current_url() or "", "") if alive else ""
            self._ok({
                "alive": alive, "url": url,
                "stealth": self.state.stealth,
                "tabs": safe(lambda: len(self.state.sb.driver.window_handles), 0) if alive else 0,
            })
        elif self.path.startswith("/file/"):
            file_path = self.path[6:]
            if not file_path.startswith("/"): file_path = "/" + file_path
            if not os.path.isfile(file_path):
                return self._err(f"file not found: {file_path}", 404)
            try:
                data = open(file_path, "rb").read()
                b64 = base64.b64encode(data).decode()
                mime = "application/octet-stream"
                for ext, m in [(".png","image/png"),(".jpg","image/jpeg"),(".jpeg","image/jpeg"),
                               (".webp","image/webp"),(".gif","image/gif"),(".pdf","application/pdf")]:
                    if file_path.lower().endswith(ext): mime = m; break
                self._ok({"path": file_path, "size": len(data), "mime": mime,
                          "data_uri": f"data:{mime};base64,{b64}"})
            except Exception as e:
                self._err(f"read failed: {e}")
        else:
            self._err(f"Unknown GET endpoint: {self.path}", 404)

    # ── POST ────────────────────────────────────────────────────────────────

    def do_POST(self):
        try:
            body = self._read_body()
        except Exception as e:
            self._err(f"Invalid JSON: {e}", 400)
            return
        with self.state._lock:
            self._dispatch(self.path, body)

    def _dispatch(self, path, body):
        sb = self.state.sb

        if path == "/reset":
            self.state.reset()
            self._ok({"message": "browser reset"})

        elif path == "/navigate":
            url = body.get("url", "")
            if not url: return self._err("missing url", 400)
            if not self.state.ensure(): return self._err("browser not available")
            sb = self.state.sb
            sb.get(url)
            sb.sleep(2)
            self.state.snapshot_before()
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/read":
            if sb is None: return self._err("browser not available")
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/search":
            query = body.get("query", "")
            if not query: return self._err("missing query", 400)
            if not self.state.ensure(): return self._err("browser not available")
            sb = self.state.sb
            sb.get(f"https://duckduckgo.com/?q={query.replace(' ', '+')}&iax=images&ia=images")
            sb.sleep(2)
            self.state.snapshot_before()
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/go_back":
            if sb is None: return self._err("browser not available")
            before = self.state.snapshot_before()
            sb.go_back()
            sb.sleep(1)
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/refresh":
            if sb is None: return self._err("browser not available")
            sb.refresh()
            sb.sleep(2)
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/click":
            if sb is None: return self._err("browser not available")
            selector, err = self._resolve_selector(body)
            if err: return self._err(err, 400)

            before_handles, _ = self._capture_tabs_info(sb)
            self.state.snapshot_before()

            # Occlusion-aware click: try Selenium click, fall back to JS click
            click_method = "selenium"
            try:
                # Check if element is occluded
                occ_result = safe(lambda: json.loads(sb.execute_script(f'{OCCLUSION_CHECK_JS}("{selector.replace(chr(34), chr(92)+chr(34))}")') or "{}"), {})
                if occ_result.get("occluded"):
                    log(f"element occluded by {occ_result.get('blocker')}, using JS click")
                    sb.execute_script(f'document.querySelector("{selector.replace(chr(34), chr(92)+chr(34))}").click()')
                    click_method = "js_fallback"
                else:
                    sb.click(selector)
            except Exception:
                # If Selenium click fails, try JS click as fallback
                try:
                    escaped = selector.replace('"', '\\"')
                    sb.execute_script(f'document.querySelector("{escaped}").click()')
                    click_method = "js_fallback"
                except Exception as e:
                    return self._err(f"click failed (both Selenium and JS): {e}")

            sb.sleep(1)

            # Detect new tabs
            tab_info = self._detect_and_handle_new_tab(sb, before_handles)

            capture = self._capture_with_fingerprint(sb, self.state)
            capture["click_method"] = click_method
            capture.update(tab_info)
            self._ok(capture)

        elif path == "/type":
            if sb is None: return self._err("browser not available")
            selector, err = self._resolve_selector(body)
            if err: return self._err(err, 400)
            text = body.get("text", "")
            if not text: return self._err("missing text", 400)
            submit = body.get("submit", False)
            clear = body.get("clear", True)

            self.state.snapshot_before()

            # CDP-based typing: uses native input setter for React compatibility
            try:
                escaped_sel = selector.replace("'", "\\'").replace("\\", "\\\\")
                escaped_text = text.replace("'", "\\'").replace("\\", "\\\\")
                js_code = f'{CDP_TYPE_JS}(\'{escaped_sel}\', \'{escaped_text}\', {str(clear).lower()})'
                result = safe(lambda: json.loads(sb.execute_script(js_code) or "{}"), {})
                if not result.get("ok"):
                    # Fall back to Selenium type
                    if clear:
                        try: sb.clear(selector)
                        except Exception: pass
                    sb.type(selector, text)
            except Exception:
                # Final fallback
                try:
                    if clear:
                        try: sb.clear(selector)
                        except Exception: pass
                    sb.type(selector, text)
                except Exception as e:
                    return self._err(f"type failed: {e}")

            if submit:
                try:
                    escaped_sel = selector.replace('"', '\\"')
                    # Find the closest form and submit it
                    sb.execute_script(f'''
                        var el = document.querySelector("{escaped_sel}");
                        var form = el ? el.closest('form') : null;
                        if (form) form.submit();
                        else {{ el.dispatchEvent(new KeyboardEvent('keydown', {{key:'Enter',code:'Enter',keyCode:13}}));
                                el.dispatchEvent(new KeyboardEvent('keypress', {{key:'Enter',code:'Enter',keyCode:13}}));
                                el.dispatchEvent(new KeyboardEvent('keyup', {{key:'Enter',code:'Enter',keyCode:13}})); }}
                    ''')
                except Exception:
                    try: sb.send_keys(selector, "\n")
                    except Exception: pass

            sb.sleep(1)
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/scroll":
            if sb is None: return self._err("browser not available")
            direction = body.get("direction", "down")
            amount = body.get("amount", 0)
            self.state.snapshot_before()
            if amount > 0:
                sb.execute_script(f"window.scrollBy(0, {amount if direction != 'up' else -amount})")
            elif direction == "down": sb.scroll_down()
            elif direction == "up": sb.scroll_up()
            else: sb.scroll_to(direction)
            sb.sleep(0.5)
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/label":
            if sb is None: return self._err("browser not available")
            element_map = run_labeler(sb, self.state)
            stats = run_page_stats(sb)
            spath = screenshot_path()
            safe(lambda: sb.save_screenshot(spath), None)
            self._ok({"element_map": element_map, "screenshot_path": spath,
                       "page_stats": stats, "url": safe(lambda: sb.get_current_url() or "", "")})

        elif path == "/interact":
            if sb is None: return self._err("browser not available")
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/extract_images":
            if sb is None: return self._err("browser not available")
            images = []
            try:
                for img in sb.select_all("img[src]", timeout=5):
                    src = img.get_attribute("src") or ""
                    if src and not src.startswith("data:") and not src.startswith("blob:") and len(src) < 2000:
                        images.append({"src": src, "alt": (img.get_attribute("alt") or "")[:100]})
                        if len(images) >= MAX_IMAGES: break
            except Exception: pass
            self._ok({"images": images, "url": safe(lambda: sb.get_current_url() or "", "")})

        elif path == "/screenshot":
            path_out = body.get("path", screenshot_path())
            if sb is None: return self._err("browser not available")
            sb.save_screenshot(path_out)
            self._ok({"screenshot_path": path_out, "url": safe(lambda: sb.get_current_url() or "", "")})

        elif path == "/download":
            url = body.get("url", "")
            out_path = body.get("path", "")
            if not url or not out_path: return self._err("missing url or path", 400)
            import urllib.request
            try:
                urllib.request.urlretrieve(url, out_path)
                self._ok({"url": url, "path": out_path, "size": os.path.getsize(out_path)})
            except Exception as e:
                self._err(f"download failed: {e}")

        elif path == "/find_text":
            text_query = body.get("text", "")
            if not text_query: return self._err("missing text", 400)
            if sb is None: return self._err("browser not available")
            self.state.snapshot_before()
            try:
                escaped = text_query.replace('"', '\\"')
                sb.execute_script(f'window.find("{escaped}")')
                sb.sleep(0.5)
            except Exception as e:
                return self._err(f"find_text failed: {e}")
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/evaluate":
            code = body.get("code", "")
            if not code: return self._err("missing code", 400)
            if sb is None: return self._err("browser not available")
            try:
                result = sb.execute_script(code)
                self._ok({"result": str(result)[:5000] if result is not None else None,
                          "type": type(result).__name__})
            except Exception as e:
                self._err(f"evaluate failed: {e}")

        elif path == "/run":
            code = body.get("code", "")
            if not code: return self._err("missing code", 400)
            if sb is None: return self._err("browser not available")
            namespace = {"sb": sb, "json": json, "os": os, "time": time, "state": self.state}
            try:
                exec(code, namespace)
            except Exception as e:
                return self._err(f"exec error: {e}")
            if "result" in namespace and isinstance(namespace["result"], dict):
                self._ok(namespace["result"])
            else:
                self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/tabs":
            if sb is None: return self._err("browser not available")
            tabs = []
            try:
                handles = sb.driver.window_handles
                current = sb.driver.current_window_handle
                for i, h in enumerate(handles):
                    sb.driver.switch_to.window(h)
                    tabs.append({"index": i,
                                 "url": safe(lambda: sb.get_current_url() or "", ""),
                                 "title": safe(lambda: sb.get_title() or "", ""),
                                 "active": h == current})
                sb.driver.switch_to.window(current)
            except Exception as e:
                return self._err(f"tab listing failed: {e}")
            self._ok({"tabs": tabs})

        elif path == "/new_tab":
            url = body.get("url", "about:blank")
            if sb is None: return self._err("browser not available")
            try:
                escaped = url.replace("'", "\\'")
                sb.driver.execute_script(f"window.open('{escaped}', '_blank');")
                handles = sb.driver.window_handles
                sb.driver.switch_to.window(handles[-1])
                sb.sleep(2)
                self._ok(self._capture_with_fingerprint(sb, self.state))
            except Exception as e:
                self._err(f"new tab failed: {e}")

        elif path == "/switch_tab":
            index = body.get("index", 0)
            if sb is None: return self._err("browser not available")
            try:
                handles = sb.driver.window_handles
                if 0 <= index < len(handles):
                    sb.driver.switch_to.window(handles[index])
                    sb.sleep(1)
                    self._ok(self._capture_with_fingerprint(sb, self.state))
                else:
                    self._err(f"tab index {index} out of range (0-{len(handles)-1})")
            except Exception as e:
                self._err(f"switch tab failed: {e}")

        elif path == "/close_tab":
            if sb is None: return self._err("browser not available")
            try:
                handles = sb.driver.window_handles
                if len(handles) <= 1: return self._err("can't close last tab")
                sb.driver.close()
                handles = sb.driver.window_handles
                sb.driver.switch_to.window(handles[-1])
                sb.sleep(1)
                self._ok(self._capture_with_fingerprint(sb, self.state))
            except Exception as e:
                self._err(f"close tab failed: {e}")

        elif path == "/dropdown_options":
            if sb is None: return self._err("browser not available")
            selector, err = self._resolve_selector(body)
            if err: return self._err(err, 400)
            try:
                escaped = selector.replace("'", "\\'").replace("\\", "\\\\")
                result = json.loads(sb.execute_script(f'{DROPDOWN_OPTIONS_JS}(\'{escaped}\')'))
                if not result.get("ok"):
                    return self._err(result.get("error", "not a select element"), 400)
                self._ok({"selector": selector, "options": result["options"],
                          "multiple": result["multiple"], "selected_count": result["selected_count"]})
            except Exception as e:
                self._err(f"dropdown_options failed: {e}")

        elif path == "/select_dropdown":
            if sb is None: return self._err("browser not available")
            selector, err = self._resolve_selector(body)
            if err: return self._err(err, 400)
            value = body.get("value")
            text_val = body.get("text")
            if value is None and text_val is None:
                return self._err("missing value or text", 400)
            self.state.snapshot_before()
            try:
                escaped = selector.replace("'", "\\'").replace("\\", "\\\\")
                val_arg = json.dumps(value) if value is not None else "undefined"
                text_arg = json.dumps(text_val) if text_val is not None else "undefined"
                result = json.loads(sb.execute_script(
                    f'{SELECT_DROPDOWN_JS}(\'{escaped}\', {val_arg}, {text_arg})'))
                if not result.get("ok"):
                    return self._err("option not found in dropdown", 400)
                sb.sleep(0.5)
                self._ok(self._capture_with_fingerprint(sb, self.state))
            except Exception as e:
                self._err(f"select_dropdown failed: {e}")

        elif path == "/wait":
            seconds = min(body.get("seconds", 2), 30)
            if sb is None: return self._err("browser not available")
            time.sleep(seconds)
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/extract":
            """Extract structured data from current page using a query."""
            query = body.get("query", "extract all text content")
            if sb is None: return self._err("browser not available")
            try:
                # Extract structured data from the page based on common patterns
                result = sb.execute_script(f'''
                    return JSON.stringify((function() {{
                        var data = {{
                            title: document.title,
                            url: window.location.href,
                            headings: [],
                            paragraphs: [],
                            lists: [],
                            tables: [],
                            forms: []
                        }};
                        document.querySelectorAll('h1,h2,h3').forEach(function(h) {{
                            data.headings.push({{level: parseInt(h.tagName.charAt(1)), text: h.textContent.trim()}});
                        }});
                        document.querySelectorAll('p').forEach(function(p) {{
                            var t = p.textContent.trim();
                            if (t.length > 10 && t.length < 500) data.paragraphs.push(t);
                        }});
                        document.querySelectorAll('ul,ol').forEach(function(l) {{
                            var items = [];
                            l.querySelectorAll('li').forEach(function(li) {{ items.push(li.textContent.trim()); }});
                            if (items.length > 0) data.lists.push(items);
                        }});
                        document.querySelectorAll('table').forEach(function(t) {{
                            var rows = [];
                            t.querySelectorAll('tr').forEach(function(tr) {{
                                var cells = [];
                                tr.querySelectorAll('th,td').forEach(function(c) {{ cells.push(c.textContent.trim()); }});
                                if (cells.length > 0) rows.push(cells);
                            }});
                            if (rows.length > 0) data.tables.push(rows);
                        }});
                        document.querySelectorAll('form').forEach(function(f) {{
                            var fields = [];
                            f.querySelectorAll('input,select,textarea').forEach(function(el) {{
                                fields.push({{name:el.name, type:el.type, value:el.value}});
                            }});
                            data.forms.push({{action:f.action, method:f.method, fields:fields}});
                        }});
                        return data;
                    }})())
                ''')
                extracted = json.loads(result) if isinstance(result, str) else result
                self._ok({"extracted": extracted, "query": query})
            except Exception as e:
                self._err(f"extract failed: {e}")

        elif path == "/check_downloads":
            files = []
            try:
                ddir = self.state._download_dir
                for f in sorted(Path(ddir).iterdir(), key=lambda p: p.stat().st_mtime, reverse=True):
                    if f.is_file() and not f.name.endswith(".crdownload"):
                        files.append({"filename": f.name, "path": str(f),
                                      "size": f.stat().st_size, "modified": f.stat().st_mtime})
                self._ok({"downloads": files[:20], "download_dir": ddir})
            except Exception as e:
                self._err(f"check_downloads failed: {e}")

        # ── Cookies ────────────────────────────────────────────────────────
        elif path == "/cookies":
            # GET (no action): list cookies for current domain
            # POST (action=set): add a cookie (single via body.cookie or bulk
            #   via body.cookies — bulk is what seed-cookies.sh uses at boot
            #   to inject a Twitter/Telegram session before the agent runs)
            # POST (action=clear): delete all cookies
            if sb is None: return self._err("browser not available")
            action = body.get("action", "get")

            def _cookie_to_dict(c):
                # sb_cdp's get_all_cookies() returns CDP Cookie dataclass
                # instances, which aren't JSON-serializable. Project back to
                # the dict shape callers expect.
                if isinstance(c, dict):
                    return c
                return {
                    "name": getattr(c, "name", ""),
                    "value": getattr(c, "value", ""),
                    "domain": getattr(c, "domain", ""),
                    "path": getattr(c, "path", ""),
                    "secure": bool(getattr(c, "secure", False)),
                    "expires": getattr(c, "expires", None),
                    "httponly": bool(getattr(c, "http_only", False)),
                }

            def _dicts_to_cookie_params(dicts):
                # sb_cdp's set_all_cookies() ultimately calls
                # cdp.network.set_cookies([i.to_json() for i in cookies]).
                # That requires CDP CookieParam instances — plain dicts lack
                # .to_json() and the call deadlocks inside the asyncio loop.
                # Convert each dict to a CookieParam with the fields we have.
                #
                # IMPORTANT: `expires` is intentionally NOT forwarded. CDP's
                # bulk Network.setCookies silently rejects the ENTIRE batch
                # when any cookie carries an expires field — verified by
                # bisecting the twitter cookie payload (1 cookie with
                # expires → whole batch dropped, even cookies that would
                # otherwise succeed on their own). Session-scoped cookies
                # are correct for our use case: seed-cookies.sh runs at
                # sandbox boot, the agent uses them in-session, sandbox
                # tears down at exit. No persistence needed past that.
                from mycdp import network as _net
                params = []
                for d in dicts:
                    if not isinstance(d, dict):
                        params.append(d)
                        continue
                    kwargs = {"name": d.get("name", ""), "value": d.get("value", "")}
                    if d.get("domain") is not None:
                        kwargs["domain"] = d["domain"]
                    if d.get("path") is not None:
                        kwargs["path"] = d["path"]
                    if d.get("secure") is not None:
                        kwargs["secure"] = bool(d["secure"])
                    if d.get("httponly") is not None:
                        kwargs["http_only"] = bool(d["httponly"])
                    params.append(_net.CookieParam(**kwargs))
                return params

            def _navigate_to_cookie_domain(cookies_list):
                # CDP Network.setCookies silently drops cookies when the
                # browser is on about:blank — the cookie store has no scope
                # to bind them to, even with explicit domain/url on each
                # cookie. Navigating to the cookie's domain first gives the
                # browser the context it needs. Picked from the first cookie.
                if not cookies_list:
                    return
                first = cookies_list[0]
                if isinstance(first, dict):
                    domain = (first.get("domain") or "").lstrip(".")
                else:
                    domain = (getattr(first, "domain", "") or "").lstrip(".")
                if not domain:
                    return
                try:
                    cur = sb.get_current_url() or ""
                except Exception:
                    cur = ""
                if domain in cur:
                    return
                try:
                    sb.open(f"https://{domain}")
                except Exception:
                    pass

            try:
                if action == "get":
                    cookies = sb.get_all_cookies() or []
                    self._ok({"cookies": [_cookie_to_dict(c) for c in cookies]})
                elif action == "set":
                    cookies = body.get("cookies")
                    if cookies is not None:
                        if not isinstance(cookies, list):
                            return self._err("cookies must be a list", 400)
                        _navigate_to_cookie_domain(cookies)
                        sb.set_all_cookies(_dicts_to_cookie_params(cookies))
                        self._ok({"set": True, "count": len(cookies)})
                    else:
                        cookie = body.get("cookie", {})
                        _navigate_to_cookie_domain([cookie])
                        sb.set_all_cookies(_dicts_to_cookie_params([cookie]))
                        self._ok({"set": True, "name": cookie.get("name", "")})
                elif action == "clear":
                    sb.clear_cookies()
                    self._ok({"cleared": True})
                else:
                    self._err(f"unknown cookies action: {action}", 400)
            except Exception as e:
                self._err(f"cookies op failed: {e}")

        # ── LocalStorage ───────────────────────────────────────────────────
        elif path == "/storage":
            if sb is None: return self._err("browser not available")
            action = body.get("action", "get")
            try:
                if action == "get":
                    # If key given, return that value; else dump all localStorage
                    key = body.get("key")
                    if key:
                        val = sb.get_local_storage_item(key)
                        self._ok({"key": key, "value": val})
                    else:
                        items = sb.execute_script(
                            "return Object.assign({}, localStorage)"
                        ) or {}
                        self._ok({"items": items})
                elif action == "set":
                    key = body.get("key", "")
                    value = body.get("value", "")
                    if not key: return self._err("missing key", 400)
                    sb.set_local_storage_item(key, str(value))
                    self._ok({"set": True, "key": key})
                elif action == "clear":
                    sb.execute_script("localStorage.clear()")
                    self._ok({"cleared": True})
                else:
                    self._err(f"unknown storage action: {action}", 400)
            except Exception as e:
                self._err(f"storage op failed: {e}")

        # ── File upload via SeleniumBase ───────────────────────────────────
        elif path == "/upload":
            if sb is None: return self._err("browser not available")
            selector = body.get("selector", "")
            file_path = body.get("file_path", "")
            if not selector or not file_path:
                return self._err("selector and file_path required", 400)
            if not os.path.isfile(file_path):
                return self._err(f"file not found: {file_path}", 400)
            try:
                sb.upload_file(file_path, selector)
                self._ok({"uploaded": True, "selector": selector, "file": file_path})
            except Exception as e:
                self._err(f"upload failed: {e}")

        # ── Accessibility tree via JS ──────────────────────────────────────
        elif path == "/a11y":
            if sb is None: return self._err("browser not available")
            try:
                # Walk the DOM collecting aria role + name + selector.
                # More reliable than CDP Accessibility domain (which requires enabling).
                # Wrap in an IIFE so `const`/`function` declarations don't leak
                # into the persistent CDP compilation context. Without this,
                # calling /a11y twice in the same page throws
                # "Identifier 'out' has already been declared". No top-level
                # `return` — CDP Runtime.evaluate runs raw, not in a function.
                result = sb.execute_script(r'''
                    (function() {
                    const out = [];
                    const nodes = document.querySelectorAll(
                      'a[href], button, input:not([type="hidden"]), select, textarea, ' +
                      '[role="button"], [role="link"], [role="textbox"], [role="combobox"], ' +
                      '[role="checkbox"], [role="radio"], [role="tab"], [role="menuitem"], ' +
                      '[role="option"], [role="searchbox"], [role="switch"], [onclick], ' +
                      '[contenteditable="true"], summary, details'
                    );
                    function buildSelector(el) {
                      if (el.id) return '#' + CSS.escape(el.id);
                      const parts = [];
                      let current = el;
                      while (current && current !== document.body && parts.length < 3) {
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
                    for (const el of nodes) {
                      const role = el.getAttribute('role') || el.tagName.toLowerCase();
                      const name = (el.getAttribute('aria-label') || el.textContent || el.value || el.placeholder || '').trim().substring(0, 80);
                      out.push({role: role, name: name, selector: buildSelector(el)});
                      if (out.length >= 200) break;
                    }
                    return JSON.stringify(out);
                    })()
                ''')
                items = json.loads(result) if isinstance(result, str) else (result or [])
                self._ok({"items": items, "total": len(items)})
            except Exception as e:
                self._err(f"a11y failed: {e}")

        # ── Session save/restore (cookies + localStorage) ──────────────────
        elif path == "/save_session":
            if sb is None: return self._err("browser not available")
            sess_path = body.get("path", "/tmp/browser-session.json")
            try:
                cookies = sb.get_all_cookies() or []
                storage = sb.execute_script("return Object.assign({}, localStorage)") or {}
                payload = {"cookies": cookies, "localStorage": storage,
                           "url": safe(lambda: sb.get_current_url() or "", "")}
                Path(sess_path).write_text(json.dumps(payload))
                self._ok({"saved": True, "path": sess_path,
                          "cookies": len(cookies), "storage_items": len(storage)})
            except Exception as e:
                self._err(f"save_session failed: {e}")

        elif path == "/restore_session":
            if sb is None: return self._err("browser not available")
            sess_path = body.get("path", "/tmp/browser-session.json")
            try:
                if not os.path.isfile(sess_path):
                    return self._err(f"session file not found: {sess_path}", 404)
                payload = json.loads(Path(sess_path).read_text())
                # Restore cookies
                if payload.get("cookies"):
                    sb.set_all_cookies(payload["cookies"])
                # Restore localStorage
                for k, v in payload.get("localStorage", {}).items():
                    try: sb.set_local_storage_item(str(k), str(v))
                    except Exception: pass
                self._ok({"restored": True, "path": sess_path,
                          "cookies": len(payload.get("cookies", [])),
                          "storage_items": len(payload.get("localStorage", {}))})
            except Exception as e:
                self._err(f"restore_session failed: {e}")

        # ── Drag-and-drop (Phase 19) ───────────────────────────────────────
        elif path == "/drag":
            if sb is None: return self._err("browser not available")
            strategy = body.get("strategy", "auto")
            steps = int(body.get("steps", 25) or 25)

            # Resolve SOURCE: optional selector (from_index/from_selector) and/or coords.
            src_sel = ""
            if body.get("from_selector") or body.get("from_index"):
                src_sel, err = self._resolve_selector(
                    {"selector": body.get("from_selector", ""), "index": body.get("from_index", 0)})
                if err: return self._err("source: " + err, 400)
            src_x = body.get("from_x"); src_y = body.get("from_y")
            if src_sel and (src_x is None or src_y is None):
                r = safe(lambda: json.loads(sb.execute_script(
                    f"{ELEMENT_CENTER_JS}({js_str(src_sel)})") or "{}"), {})
                if not r.get("ok"): return self._err("source: " + r.get("error", "not found"))
                src_x, src_y = r["x"], r["y"]
                # Auto-pick HTML5 when source is genuinely draggable and not an offset drag.
                if strategy == "auto" and r.get("draggable") and body.get("dx") is None and body.get("dy") is None:
                    strategy = "html5"
            if (src_x is None or src_y is None) and not src_sel:
                return self._err("drag needs a source: from_index/from_selector OR from_x/from_y", 400)

            # Resolve TARGET: offset (dx/dy) > element (to_*) > coords (to_x/to_y).
            tgt_sel = ""
            tgt_x = body.get("to_x"); tgt_y = body.get("to_y")
            if body.get("dx") is not None or body.get("dy") is not None:
                if src_x is None or src_y is None: return self._err("offset drag needs a resolved source position", 400)
                tgt_x = src_x + (body.get("dx") or 0); tgt_y = src_y + (body.get("dy") or 0)
                if strategy == "auto": strategy = "physics"
            elif body.get("to_selector") or body.get("to_index"):
                tgt_sel, err = self._resolve_selector(
                    {"selector": body.get("to_selector", ""), "index": body.get("to_index", 0)})
                if err: return self._err("target: " + err, 400)
                r = safe(lambda: json.loads(sb.execute_script(
                    f"{ELEMENT_CENTER_JS}({js_str(tgt_sel)})") or "{}"), {})
                if not r.get("ok"): return self._err("target: " + r.get("error", "not found"))
                tgt_x, tgt_y = r["x"], r["y"]
                if strategy == "auto": strategy = "html5"
            elif tgt_x is not None and tgt_y is not None:
                if strategy == "auto": strategy = "physics"
            else:
                return self._err("drag needs a target: to_index/to_selector, to_x/to_y, or dx/dy", 400)

            self.state.snapshot_before()
            method = strategy
            if method == "html5":
                if not (src_sel and tgt_sel):
                    method = "physics"  # coords-only: HTML5 needs element handles
                else:
                    res = safe(lambda: json.loads(sb.execute_script(
                        f"{SIMULATE_DND_JS}({js_str(src_sel)}, {js_str(tgt_sel)})") or "{}"), {})
                    if not res.get("ok"): return self._err("html5 drag failed: " + res.get("error", ""))
            if method == "physics":
                res = safe(lambda: json.loads(sb.execute_script(
                    f"{PHYS_DRAG_JS}({src_x}, {src_y}, {tgt_x}, {tgt_y}, {steps}, 0)") or "{}"), {})
                if not res.get("ok"): return self._err("physics drag failed: " + res.get("error", ""))
            sb.sleep(0.8)
            capture = self._capture_with_fingerprint(sb, self.state)
            capture["drag_method"] = method
            capture["from"] = [src_x, src_y]
            capture["to"] = [tgt_x, tgt_y]
            self._ok(capture)

        elif path == "/hover":
            if sb is None: return self._err("browser not available")
            selector = ""
            if body.get("selector") or body.get("index"):
                selector, err = self._resolve_selector(body)
                if err: return self._err(err, 400)
            x = body.get("x"); y = body.get("y")
            if not selector and (x is None or y is None):
                return self._err("hover needs index/selector OR x,y", 400)
            self.state.snapshot_before()
            sel_arg = js_str(selector) if selector else "null"
            xx = x if x is not None else 0
            yy = y if y is not None else 0
            res = safe(lambda: json.loads(sb.execute_script(
                f"{HOVER_JS}({sel_arg}, {xx}, {yy})") or "{}"), {})
            if not res.get("ok"): return self._err("hover failed: " + res.get("error", ""))
            sb.sleep(0.5)
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/press":
            if sb is None: return self._err("browser not available")
            keys = body.get("keys", "")
            if not keys: return self._err("missing keys", 400)
            selector = ""
            if body.get("selector") or body.get("index"):
                selector, err = self._resolve_selector(body)
                if err: return self._err(err, 400)
            self.state.snapshot_before()
            sel_arg = js_str(selector) if selector else "null"
            res = safe(lambda: json.loads(sb.execute_script(
                f"{PRESS_JS}({js_str(keys)}, {sel_arg})") or "{}"), {})
            if not res.get("ok"): return self._err("press failed: " + res.get("error", ""))
            sb.sleep(0.5)
            capture = self._capture_with_fingerprint(sb, self.state)
            capture["pressed"] = keys
            self._ok(capture)

        elif path == "/click_at":
            if sb is None: return self._err("browser not available")
            x = body.get("x"); y = body.get("y")
            if x is None or y is None:
                if body.get("selector") or body.get("index"):
                    selector, err = self._resolve_selector(body)
                    if err: return self._err(err, 400)
                    r = safe(lambda: json.loads(sb.execute_script(
                        f"{ELEMENT_CENTER_JS}({js_str(selector)})") or "{}"), {})
                    if not r.get("ok"): return self._err("click_at: " + r.get("error", "not found"))
                    x, y = r["x"], r["y"]
                else:
                    return self._err("click_at needs x,y OR index/selector", 400)
            button = int(body.get("button", 0) or 0)
            is_double = "true" if body.get("double") else "false"
            is_right = "true" if body.get("right") else "false"
            before_handles, _ = self._capture_tabs_info(sb)
            self.state.snapshot_before()
            res = safe(lambda: json.loads(sb.execute_script(
                f"{CLICK_AT_JS}({x}, {y}, {button}, {is_double}, {is_right})") or "{}"), {})
            if not res.get("ok"): return self._err("click_at failed: " + res.get("error", ""))
            sb.sleep(0.8)
            tab_info = self._detect_and_handle_new_tab(sb, before_handles)
            capture = self._capture_with_fingerprint(sb, self.state)
            capture.update(tab_info)
            self._ok(capture)

        elif path == "/scroll_into_view":
            if sb is None: return self._err("browser not available")
            selector, err = self._resolve_selector(body)
            if err: return self._err(err, 400)
            self.state.snapshot_before()
            res = safe(lambda: json.loads(sb.execute_script(
                f"{SCROLL_INTO_VIEW_JS}({js_str(selector)})") or "{}"), {})
            if not res.get("ok"): return self._err("scroll_into_view failed: " + res.get("error", ""))
            sb.sleep(0.4)
            self._ok(self._capture_with_fingerprint(sb, self.state))

        elif path == "/iframe":
            if sb is None: return self._err("browser not available")
            action = body.get("action", "list")
            if action == "list":
                frames = safe(lambda: json.loads(sb.execute_script(
                    "return JSON.stringify(Array.from(document.querySelectorAll('iframe')).map((f,i)=>({"
                    "index:i, name:f.name||'', id:f.id||'', src:(f.src||'').substring(0,120), "
                    "title:(f.title||f.getAttribute('aria-label')||'').substring(0,80)})))") or "[]"), [])
                self._ok({"frames": frames, "total": len(frames)})
            elif action == "enter":
                selector, err = self._resolve_selector(body)
                if err: return self._err(err, 400)
                try:
                    sb.switch_to_frame(selector)
                    self._ok(self._capture_with_fingerprint(sb, self.state))
                except Exception as e:
                    return self._err(f"iframe enter failed: {e}")
            elif action == "exit":
                try:
                    sb.switch_to_default_content()
                    self._ok(self._capture_with_fingerprint(sb, self.state))
                except Exception as e:
                    return self._err(f"iframe exit failed: {e}")
            else:
                return self._err(f"unknown iframe action: {action}", 400)

        else:
            self._err(f"Unknown endpoint: {path}", 404)


# ═══════════════════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════════════════

def main():
    port = int(os.environ.get("SB_SERVER_PORT", DEFAULT_PORT))
    stealth = "--stealth" in sys.argv
    state = BrowserState(stealth=stealth)
    Handler.state = state
    server = HTTPServer(("127.0.0.1", port), Handler)
    log(f"listening on :{port} stealth={stealth} download_dir={state._download_dir}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        state.close()
        server.server_close()

if __name__ == "__main__":
    main()
