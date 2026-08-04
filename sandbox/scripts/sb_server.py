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
    sb_server.py [--port PORT] [--stealth] [--use-chromium]

Flags:
    --stealth         Launch own Chrome with fingerprint-masking flags, proxy
                      support, and randomized viewport instead of attaching to
                      the supervisord Chrome. Kills any existing Chrome first.
    --use-chromium    Use unbranded Chromium (auto-downloaded via SeleniumBase)
                      instead of Google Chrome. Implies a stealthier fingerprint
                      on some sites (e.g. Reddit). Only meaningful with --stealth.

Environment:
    SB_SERVER_PORT    Port to listen on (default: 9876)
    SB_SERVER_PROXY   HTTP/S proxy URL for Chrome (e.g. http://user:pass@host:port)
    DISPLAY           X11 display (set by supervisord)

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
    /drag               {"strategy":"auto|html5|physics", from_*/to_*|dx/dy} → drag-and-drop
    /hover              {"index|selector"|"x,y"}         → hover (reveal menus/tooltips)
    /press              {"keys":"Enter|Control+a|ArrowDown", "index|selector"?} → key / hotkey press
    /click_at           {"x,y"|"index|selector", button?, double?, right?} → coordinate / variant click
    /scroll_into_view   {"index|selector"}               → scroll element into the viewport
    /iframe             {"action":"list|enter|exit", selector?} → iframe traversal
    /save_session       {"path":"..."}                   → save cookies + localStorage to file
    /restore_session    {"path":"..."}                   → restore from saved session file
    /reset              {}                               → kill and recreate browser
    /solve_captcha      {}                               → honest best-effort captcha click + VERIFY
    /uc                 {"action":"open|click|type|read|evaluate|cookies|close", ...}
                                                        → SB(uc=True) Cloudflare/Turnstile/hCaptcha bypass
    /accept_cookies     {}                               → GDPR/CCPA banner dismissal (curated selectors)
    /warmup_history     {"urls":[...]?, "dwell":3.0}     → visit benign sites to build fingerprint legitimacy
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
import asyncio
import traceback
import threading
import re
import base64
import random
import socket
import tempfile
from http.server import HTTPServer, ThreadingHTTPServer, BaseHTTPRequestHandler
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


# ── Drag / mouse / keyboard interaction ──────────────────────────────────────
# These IIFEs mirror OCCLUSION_CHECK_JS / CDP_TYPE_JS: raw CDP Runtime.evaluate
# runs them unwrapped, so each is a single arrow-fn expression returning a JSON
# string. Selenium's native drag_and_drop() does NOT emit the HTML5
# dragstart/dragover/drop sequence (W3C WebDriver gap — Selenium issue #3604),
# so SIMULATE_DND_JS synthesizes that chain. Physics drags (custom sliders,
# dnd-kit, canvas) use _trusted_cdp_drag below — a Python helper that drives
# the CDP Input domain directly, NOT an IIFE (it needs isTrusted=true events
# that no in-page JS can produce).

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

# Trusted physics drag via the CDP Input domain. Replaces the old synthetic
# PHYS_DRAG_JS (untrusted dispatchEvent), which could NOT move native widgets:
# a real <input type=range> ignores synthetic MouseEvents because Chrome
# computes its value from the *default action* of trusted pointer input, and
# pointer-based DnD libraries (dnd-kit) ignore untrusted pointer events. CDP
# Input.dispatchMouseEvent moves the real browser cursor → isTrusted=true mouse
# + synthesized pointer events → native sliders move, dnd-kit's PointerSensor
# fires, canvas/map widgets respond. The HTML5 SIMULATE_DND_JS chain above is
# STILL required for native draggable=true (CDP mouse input does not synthesize
# dragstart/dragover/drop — only a real OS drag gesture does, which no API
# produces). mycdp is imported lazily because it exists only in the sandbox
# image, not on the host where the test suite imports this module.
def _trusted_cdp_drag(sb, x1, y1, x2, y2, steps=40, button="left"):
    """Trusted drag via CDP Input.dispatchMouseEvent. Caller MUST scroll the
    start element into view first and pass post-scroll viewport coordinates
    (off-screen coordinates hit nothing).

    Mirrors the CDP primitive SeleniumBase's mouse_click_async uses (the WORKING
    one): mousePressed/Released carry ``buttons`` (the held-button bitmask) and
    ``click_count=1`` so the browser treats them as a real click → default
    actions fire (native sliders move, checkboxes toggle). The drag-move steps
    carry ``buttons=1`` too — that bitmask is how the browser distinguishes a
    held-button DRAG (range follows the cursor, dnd-kit PointerSensor stays
    armed) from a bare hover. Without it, SeleniumBase's own mouse_drag_async
    (and our v1) emit press+move+release that custom-widget JS handlers catch
    but native default-actions ignore."""
    import mycdp as cdp  # vendored by seleniumbase; container-only

    async def _run():
        tab = sb.page
        btn = cdp.input_.MouseButton(button)
        held = 1 if button == "left" else 2  # buttons bitmask: left=1, right=2
        # hover onto the start point first
        await tab.send(cdp.input_.dispatch_mouse_event("mouseMoved", x=x1, y=y1))
        await asyncio.sleep(0.05)
        # press — click_count=1 + buttons=held makes this a real primary press
        await tab.send(cdp.input_.dispatch_mouse_event(
            "mousePressed", x=x1, y=y1, button=btn,
            buttons=held, click_count=1))
        await asyncio.sleep(0.03)
        # interpolated drag moves — buttons=held signals "left still down"
        for i in range(1, steps + 1):
            t = i / steps
            await tab.send(cdp.input_.dispatch_mouse_event(
                "mouseMoved",
                x=x1 + (x2 - x1) * t, y=y1 + (y2 - y1) * t,
                buttons=held))
            await asyncio.sleep(0.005)
        await asyncio.sleep(0.03)
        await tab.send(cdp.input_.dispatch_mouse_event(
            "mouseReleased", x=x2, y=y2, button=btn,
            buttons=held, click_count=1))

    sb.loop.run_until_complete(_run())


def _trusted_cdp_click(sb, x, y, button="left", click_count=1):
    """Trusted click via CDP Input.dispatchMouseEvent — the anti-bot primitive
    Selenium's ``sb.click()`` and ``CLICK_AT_JS`` can't provide (both emit
    untrusted events that Cloudflare Turnstile / behavioral fingerprinting
    silently ignore; only ``isTrusted=true`` input passes). Mirrors
    ``_trusted_cdp_drag``'s CDP shape (proven in this image):
    mouseMoved → mousePressed → mouseReleased at the element's viewport center.
    ``click_count=2`` for a double-click, ``button="right"`` for a context menu.
    Caller MUST scroll the element into view first and pass post-scroll viewport
    coords — ``dispatchMouseEvent`` hits nothing off-screen.

    This is the value OpenComputer ships that pux lacked: every other click path
    here (Selenium, JS ``.click()``, ``CLICK_AT_JS``) is untrusted. Drag already
    used this primitive; click/type now do too, opt-in via ``trusted: true``."""
    import mycdp as cdp  # vendored by seleniumbase; container-only

    async def _run():
        tab = sb.page
        btn = cdp.input_.MouseButton(button)
        held = 1 if button == "left" else 2  # buttons bitmask: left=1, right=2
        await tab.send(cdp.input_.dispatch_mouse_event("mouseMoved", x=x, y=y))
        await asyncio.sleep(0.04)
        await tab.send(cdp.input_.dispatch_mouse_event(
            "mousePressed", x=x, y=y, button=btn, buttons=held, click_count=click_count))
        await asyncio.sleep(0.04)
        await tab.send(cdp.input_.dispatch_mouse_event(
            "mouseReleased", x=x, y=y, button=btn, buttons=held, click_count=click_count))

    sb.loop.run_until_complete(_run())


def _trusted_cdp_type(sb, text):
    """Trusted typing via CDP ``Input.insertText`` — fires a real (isTrusted)
    ``input`` event through the browser's editing pipeline, so React/Vue
    controlled inputs update WITHOUT the native-value-setter workaround in
    ``CDP_TYPE_JS`` (the trusted path is strictly more correct, not a
    fallback). Caller MUST focus (+ optionally clear) the target first —
    ``insertText`` lands at the caret of the focused editable element."""
    import mycdp as cdp  # vendored by seleniumbase; container-only

    async def _run():
        tab = sb.page
        await tab.send(cdp.input_.insert_text(text))

    sb.loop.run_until_complete(_run())


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
    """Persistent SeleniumBase browser with labeler cache and fingerprinting.

    Two modes:
      - Normal: attach to supervisord's Chrome on CDP port 9222 (default).
      - Stealth: kill supervisord Chrome, launch own Chrome with fingerprint-
        masking flags, optional unbranded Chromium, proxy, random viewport.
    """

    def __init__(self, stealth=False, use_chromium=False, cdp_port=9222):
        self.stealth = stealth
        self.use_chromium = use_chromium
        self.cdp_port = cdp_port
        self.sb = None
        self._ctx = None
        self._lock = threading.Lock()
        self._last_element_map = []
        self._download_dir = "/tmp/sb_downloads"
        self._last_fingerprint = ""
        self._chrome_process = None
        self._user_data_dir = None
        # UC-mode session (SeleniumBase SB(uc=True)) — a SEPARATE patched
        # undetected-chromedriver Chrome used to bypass Cloudflare Turnstile /
        # hCaptcha / reCAPTCHA that the persistent sb_cdp browser can't click.
        # Held open across calls so the agent can open→click_captcha→type on
        # the same CF-cleared page. Closed by _close_uc() / reset() / close().
        self.uc_sb = None
        self._uc_stack = None
        os.makedirs(self._download_dir, exist_ok=True)
        self._init_browser()

    def _init_browser(self):
        self._close_browser()
        os.environ.setdefault("DISPLAY", ":99")

        if self.stealth:
            self._init_browser_stealth()
        else:
            self._init_browser_normal()

    # ── Normal mode: attach to supervisord Chrome ───────────────────────────
    def _init_browser_normal(self):
        """Attach to the supervisord Chrome on CDP port 9222 (default).
        No stealth, no proxy, no fingerprint masking — but still uses CDP so
        actions (click/type/drag) go through the trusted Input domain."""

        import subprocess as _sp
        import time as _t
        # Precise binary names only — see _init_browser_stealth for why bare
        # "chromium" is fatal (matches --use-chromium in our own argv).
        for _pat in ("google-chrome-stable", "chromium-browser"):
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
            self.sb = sb_cdp.Chrome("about:blank", host="127.0.0.1", port=9222)
            self._ctx = None
            self._setup_cdp_downloads()
        except Exception as e:
            print(f"[sb_server] browser init failed: {e}", file=sys.stderr)
            import traceback as _tb
            _tb.print_exc(file=sys.stderr)
            self.sb = None
            self._ctx = None

    # ── Stealth mode: launch own Chrome with fingerprint masking ────────────
    def _init_browser_stealth(self):
        """Kill supervisord Chrome, launch a fresh Chrome (or unbranded
        Chromium) with fingerprint-masking flags, optional proxy, and
        randomized viewport. Attach sb_cdp.Chrome to it."""

        import subprocess as _sp
        import time as _t

        # Kill Chrome processes ONLY on OUR CDP port. When cdp_port == 9222
        # (the default/shared instance), we kill ALL Chrome (backward compat —
        # the supervisord Chrome + any strays on 9222). When cdp_port != 9222
        # (ephemeral multi-instance), we ONLY kill whatever holds our specific
        # port via fuser — other instances' Chrome processes are left alone.
        if self.cdp_port == 9222:
            for _pat in ("google-chrome-stable", "chromium-browser"):
                try:
                    _sp.run(["pkill", "-9", "-f", _pat], capture_output=True, timeout=5)
                except Exception:
                    pass
        else:
            # Ephemeral instance: only kill whatever holds our CDP port.
            try:
                _sp.run(["fuser", "-k", f"{self.cdp_port}/tcp"],
                        capture_output=True, timeout=5)
            except Exception:
                pass
        _t.sleep(0.8)

        # 1. Chrome binary — unbranded Chromium or Google Chrome
        chrome_bin = "/usr/bin/google-chrome-stable"
        if self.use_chromium:
            try:
                from seleniumbase import SB
                chrome_bin = SB.get_chromium()
                print(f"[sb_server] using unbranded Chromium: {chrome_bin}", file=sys.stderr)
            except Exception as e:
                print(f"[sb_server] chromium download failed, fallback to google-chrome: {e}", file=sys.stderr)

        # 2. Randomized viewport (base 1280x720 ± small variance)
        base_w, base_h = 1280, 720
        w = base_w + random.randint(-80, 80)
        h = base_h + random.randint(-40, 40)

        # 3. Proxy from environment
        proxy = os.environ.get("SB_SERVER_PROXY", "").strip()
        proxy_args = [f"--proxy-server={proxy}"] if proxy else []

        # 4. Unique user data dir (avoid fingerprint reuse)
        self._user_data_dir = tempfile.mkdtemp(prefix="sb_stealth_")

        # 5. CDP port — from constructor (default 9222 for shared instance,
        #    unique per ephemeral instance for multi-browser isolation)
        cdp_port = self.cdp_port

        # 6. Build Chrome args
        args = [
            chrome_bin,
            f"--window-size={w},{h}",
            "--no-first-run",
            "--no-default-browser-check",
            # ── WebGL / GL backend ───────────────────────────────────────────
            # Docker has no DRI/GPU, so we MUST keep --disable-gpu to avoid
            # GPU-init crashes. But --disable-gpu alone kills WebGL — the GL
            # backend is gone, canvas.getContext('webgl') returns null, and
            # Three.js / GLB viewers parse metadata but can't render. Fix:
            # layer SwiftShader (software WebGL) on top. Chrome 107+ requires
            # --enable-unsafe-swiftshader to opt in (it's "unsafe" because
            # software rasterization can be fingerprinted, but this sandbox
            # already runs fingerprint-masking flags). --ignore-gpu-blocklist
            # prevents Chrome from disabling SwiftShader on its own blocklist.
            "--disable-gpu",
            "--use-gl=swiftshader",
            "--enable-unsafe-swiftshader",
            "--ignore-gpu-blocklist",
            "--no-sandbox",
            "--disable-dev-shm-usage",
            f"--remote-debugging-port={cdp_port}",
            "--remote-allow-origins=*",
            f"--user-data-dir={self._user_data_dir}",
            # ── Fingerprint masking ─────────────────────────────────────────
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
            # ── Visual consistency ─────────────────────────────────────────
            # NOTE: Chrome only honors the LAST --enable-features on the
            # command line, so Vulkan (SwiftShader accel) is merged HERE
            # alongside WebContentsForceDark rather than declared separately
            # near the GL flags above.
            "--enable-features=WebContentsForceDark,Vulkan",
            "about:blank",
        ] + proxy_args

        print(f"[sb_server] starting Chrome (stealth=True, chromium={self.use_chromium}, "
              f"viewport={w}x{h}, proxy={bool(proxy)})", file=sys.stderr)

        self._chrome_process = _sp.Popen(
            args, stdout=_sp.DEVNULL, stderr=_sp.DEVNULL,
        )

        # 7. Wait for CDP port to be ready (poll up to 20s)
        for attempt in range(40):
            try:
                s = socket.socket()
                s.settimeout(1)
                s.connect(("127.0.0.1", cdp_port))
                s.close()
                break
            except Exception:
                _t.sleep(0.5)
        else:
            print(f"[sb_server] CDP port {cdp_port} not ready after 20s", file=sys.stderr)
            self._chrome_process = None
            self.sb = None
            return

        # 8. Attach sb_cdp.Chrome to our Chrome instance
        try:
            from seleniumbase import sb_cdp
            self.sb = sb_cdp.Chrome("about:blank", host="127.0.0.1", port=cdp_port)
            self._ctx = None
            self._setup_cdp_downloads()
            print("[sb_server] stealth browser ready", file=sys.stderr)
        except Exception as e:
            print(f"[sb_server] stealth attach failed: {e}", file=sys.stderr)
            import traceback as _tb
            _tb.print_exc(file=sys.stderr)
            self.sb = None

    # ── Utilities ──────────────────────────────────────────────────────────

    def _setup_cdp_downloads(self):
        try:
            downloads_path = str(Path(self._download_dir).absolute())
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
        if self._chrome_process is not None:
            try: self._chrome_process.kill()
            except Exception: pass
            try: self._chrome_process.wait(3)
            except Exception: pass
            self._chrome_process = None
        if self._user_data_dir and os.path.isdir(self._user_data_dir):
            try:
                import shutil
                shutil.rmtree(self._user_data_dir, ignore_errors=True)
            except Exception: pass
            self._user_data_dir = None

    def reset(self):
        with self._lock:
            self._close_uc()
            self._init_browser()
            self._last_element_map = []
            self._last_fingerprint = ""

    def ensure(self):
        if self.sb is None:
            self._init_browser()
            self._last_element_map = []
        return self.sb is not None

    def close(self):
        self._close_uc()
        self._close_browser()

    # ── UC mode: dedicated SB(uc=True) for captcha-protected pages ──────────
    def open_uc(self):
        """Spin up a dedicated SB(uc=True) session. Separate Chrome instance
        (patched undetected-chromedriver) from the persistent sb_cdp browser.
        Used by /uc to bypass Cloudflare Turnstile / hCaptcha / reCAPTCHA via
        sb.uc_gui_click_captcha() — physical pyautogui clicks the sb_cdp CDP
        path structurally cannot make.

        Verified working in-container (SeleniumBase 4.50.6): sb.open() +
        sb.uc_gui_click_captcha() pass nowsecure.nl cleanly. The
        uc_open_with_reconnect() path is intentionally AVOIDED — its
        disconnect/reconnect cycle loses chromedriver in this container's
        64 MiB /dev/shm + multi-Chrome environment (port 42419 dies after
        the 4s reconnect window). sb.open() on the UC driver is sufficient.

        Held open for follow-up /uc actions until _close_uc()/reset()/close().
        Returns the BaseCase instance (self.uc_sb) or raises on failure. """
        if self.uc_sb is not None:
            return self.uc_sb
        os.environ.setdefault("DISPLAY", ":99")
        import contextlib
        from seleniumbase import SB
        stack = contextlib.ExitStack()
        try:
            sb = stack.enter_context(
                SB(uc=True, test=True, xvfb=False, headed=True, locale_code="en")
            )
        except Exception as e:
            try: stack.close()
            except Exception: pass
            print(f"[sb_server] UC session open failed: {e}", file=sys.stderr)
            raise
        self._uc_stack = stack
        self.uc_sb = sb
        print("[sb_server] UC session ready (SB uc=True)", file=sys.stderr)
        return sb

    def _close_uc(self):
        if self._uc_stack is not None:
            try: self._uc_stack.close()
            except Exception as e:
                print(f"[sb_server] UC close error (non-fatal): {e}", file=sys.stderr)
            self._uc_stack = None
            self.uc_sb = None

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


def cookie_to_dict(c):
    """sb_cdp's get_all_cookies() returns CDP Cookie dataclass instances,
    not JSON-serializable dicts. Project back to the dict shape callers and
    json.dumps expect. Used by /cookies (get), /save_session, /uc handoff."""
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


def dicts_to_cookie_params(dicts):
    """Convert plain cookie dicts to CDP CookieParam instances.
    sb_cdp's set_all_cookies() ultimately calls
    cdp.network.set_cookies([i.to_json() for i in cookies]) — plain dicts
    lack .to_json() and the call deadlocks. expires is intentionally NOT
    forwarded (CDP Network.setCookies silently rejects the ENTIRE batch
    when any cookie carries expires — verified with the twitter payload).
    Used by /cookies (set), /restore_session, /uc handoff."""
    from mycdp import network as _net
    params = []
    for d in dicts:
        if not isinstance(d, dict):
            params.append(d)
            continue
        kw = {"name": d.get("name", ""), "value": d.get("value", "")}
        if d.get("domain") is not None:
            kw["domain"] = d["domain"]
        if d.get("path") is not None:
            kw["path"] = d["path"]
        if d.get("secure") is not None:
            kw["secure"] = bool(d["secure"])
        # Accept both our normalized key (httponly, from cookie_to_dict /
        # saved-session files) AND Selenium's native key (httpOnly, from
        # UC mode get_cookies()) so this is a universal drop-in.
        ho = d.get("httponly")
        if ho is None:
            ho = d.get("httpOnly")
        if ho is not None:
            kw["http_only"] = bool(ho)
        params.append(_net.CookieParam(**kw))
    return params


# ═══════════════════════════════════════════════════════════════════════════════
# HTTP Handler
# ═══════════════════════════════════════════════════════════════════════════════

# Activity tracker for TTL self-reaper (ephemeral instances only)
_last_activity = time.time()
_REAPER_TTL = 300  # 5 minutes of inactivity → self-terminate

def _activity_tick():
    global _last_activity
    _last_activity = time.time()

def _start_reaper_if_ephemeral(cdp_port):
    """Ephemeral instances (cdp_port != 9222) self-terminate after 5min idle."""
    if cdp_port == 9222:
        return  # shared instance — never self-reap
    def _reaper():
        while True:
            time.sleep(30)
            if time.time() - _last_activity > _REAPER_TTL:
                log(f"ephemeral instance (cdp {cdp_port}) idle "
                    f"{_REAPER_TTL}s — self-terminating")
                os._exit(0)
    threading.Thread(target=_reaper, daemon=True).start()

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

    def _capture_with_fingerprint(self, sb, state, *, full=True):
        """Full capture + fingerprint comparison.

        When ``full=False`` (viewport-only actions: scroll, hover, drag,
        scroll_into_view, find_text), skips the expensive ``extract_page_data``
        + ``run_page_stats`` + ``check_page_changed`` — the page text, links,
        and images are identical to the last full capture; only the viewport
        moved. Returns element_map + screenshot + ``page_unchanged: true`` so
        the agent gets fresh SoM labels and a fresh visual without re-paying
        the ~500ms + ~4KB extraction tax on every scroll."""
        element_map = run_labeler(sb, state)
        spath = screenshot_path()
        safe(lambda: sb.save_screenshot(spath), None)

        if not full:
            return {
                "element_map": element_map,
                "screenshot_path": spath,
                "page_unchanged": True,
            }

        stats = run_page_stats(sb)
        page_data = extract_page_data(sb)
        page_changed = state.check_page_changed()

        return {
            "page_data": page_data,
            "element_map": element_map,
            "screenshot_path": spath,
            "page_stats": stats,
            "page_changed": page_changed,
        }

    def _capture_uc(self, ucsb):
        """Capture from a UC-mode BaseCase instance (not the persistent sb_cdp).
        Mirrors _capture_with_fingerprint but without the persistent-state
        fingerprint comparison (the UC session has its own page state)."""
        element_map = safe(lambda: run_labeler(ucsb, self.state), []) or []
        stats = safe(lambda: run_page_stats(ucsb), {}) or {}
        page_data = {}
        try:
            page_data = extract_page_data(ucsb)
        except Exception as e:
            page_data = {"error": str(e), "title": "", "url": "", "text": "", "images": [], "links": []}
        spath = screenshot_path()
        safe(lambda: ucsb.save_screenshot(spath), None)
        # Honest challenge-status read — the UC path's whole point is bypass.
        challenge_state = safe(lambda: ucsb.execute_script("""
            (() => {
                const body = (document.body && document.body.innerText) || '';
                const markers = ['just a moment', 'checking your browser', 'cloudflare',
                                 'verify you are human', 'hcaptcha', 'enable javascript and cookies'];
                return JSON.stringify({markers: markers.filter(m => body.toLowerCase().includes(m))});
            })()
        """), '{"markers":[]}')
        try: challenge_state = json.loads(challenge_state)
        except Exception: challenge_state = {"markers": []}
        return {
            "page_data": page_data,
            "element_map": element_map,
            "screenshot_path": spath,
            "page_stats": stats,
            "challenge_state": challenge_state,
            "cf_cleared": not bool(challenge_state.get("markers")),
        }

    def _uc_handoff_cookies(self, ucsb, url):
        """Copy cookies from the UC session into the persistent sb_cdp browser
        so a subsequent /navigate to the same domain inherits cf_clearance /
        session cookies. Navigate persistent to the domain first (CDP needs a
        scope), then inject each cookie via the /cookies set path.

        Returns a summary dict. Best-effort: cf_clearance is fingerprint-bound
        so handoff isn't guaranteed, but session cookies (auth_token, csrf,
        account cookies) reliably transfer."""
        import urllib.parse as _up
        psb = self.state.sb
        if psb is None: return {"ok": False, "reason": "persistent browser not available"}
        try:
            cookies = ucsb.get_cookies() or []
        except Exception as e:
            return {"ok": False, "reason": f"uc get_cookies failed: {e}"}
        if not cookies: return {"ok": True, "injected": 0, "reason": "no cookies to hand off"}
        domain = ""
        try:
            host = _up.urlparse(url).hostname or ""
            domain = "." + host.split(".", 1)[-1] if host.count(".") >= 1 else host
        except Exception: pass
        # Navigate persistent browser to the cookie domain so CDP can bind them.
        if domain:
            try: psb.get(f"https://{domain.lstrip('.')}")
            except Exception: pass
        # Build CookieParam list (drop expires — CDP silently rejects batches
        # when ANY cookie carries expires; see /cookies set comment).
        try:
            from mycdp import network as _net
            params = []
            for c in cookies:
                if not isinstance(c, dict): continue
                kw = {"name": c.get("name", ""), "value": c.get("value", "")}
                cd = c.get("domain") or domain
                if cd: kw["domain"] = cd
                if c.get("path"): kw["path"] = c["path"]
                if c.get("secure") is not None: kw["secure"] = bool(c["secure"])
                if c.get("httpOnly") is not None: kw["http_only"] = bool(c["httpOnly"])
                params.append(_net.CookieParam(**kw))
            if params:
                # Use the same proven API as /cookies set (sb.set_all_cookies
                # takes CookieParam instances — see _dicts_to_cookie_params).
                psb.set_all_cookies(params)
            return {"ok": True, "injected": len(params),
                    "names": [c.get("name") for c in cookies if isinstance(c, dict)][:12],
                    "domain": domain}
        except Exception as e:
            return {"ok": False, "reason": f"inject failed: {e}", "injected": 0}

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
        _activity_tick()
        if self.path == "/status":
            alive = self.state.sb is not None
            url = safe(lambda: self.state.sb.get_current_url() or "", "") if alive else ""
            self._ok({
                "alive": alive, "url": url,
                "stealth": self.state.stealth,
                "chromium": self.state.use_chromium,
                "tabs": safe(lambda: len(self.state.sb.driver.window_handles), 0) if alive else 0,
            })
        elif self.path == "/warmup":
            # Force browser init NOW (Chrome attach via sb_cdp.Chrome) so the
            # first agent-driven tool call doesn't pay the cold start. Unlike
            # /status — a cheap alive check that NEVER inits — /warmup actually
            # calls ensure(). Used by the pre-run warmup_browser job declared
            # by browser-using orgs (general, dev-bot) to move the SeleniumBase
            # attach latency out of the LLM turn budget.
            warmed = self.state.ensure()
            alive = self.state.sb is not None
            if warmed:
                url = safe(lambda: self.state.sb.get_current_url() or "", "")
                self._ok({"warmed": True, "alive": alive, "url": url,
                          "stealth": self.state.stealth,
                          "chromium": self.state.use_chromium})
            else:
                self._err("warmup failed: browser not available", 503)
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
        _activity_tick()
        try:
            body = self._read_body()
        except Exception as e:
            self._err(f"Invalid JSON: {e}", 400)
            return
        with self.state._lock:
            try:
                self._dispatch(self.path, body)
            except Exception as e:
                # Top-level catch: ANY handler exception returns an error JSON
                # instead of dropping the HTTP connection ("Empty reply from
                # server") which leaves the agent waiting forever.
                import traceback
                tb = traceback.format_exc()[-500:]
                try:
                    self._err(f"handler crashed: {e}\n{tb}")
                except Exception:
                    pass

    def _dispatch(self, path, body):
        sb = self.state.sb

        if path == "/reset":
            self.state.reset()
            self._ok({"message": "browser reset"})

        elif path == "/solve_captcha":
            # HONEST best-effort captcha click on the persistent sb_cdp browser.
            # The CDP path structurally CANNOT click cross-origin CF Turnstile /
            # hCaptcha checkboxes (SOP blocks contentDocument access, and even
            # if reached, .click() lacks the isTrusted=true signal anti-bot
            # scripts require). So we attempt the JS click, then VERIFY whether
            # a challenge is still visible — and report captcha_solved=False
            # honestly when it is, pointing the agent at /uc (the real
            # SB(uc=True) + uc_gui_click_captcha path) instead of falsely
            # claiming success and letting the agent walk into a wall.
            if sb is None: return self._err("browser not available")
            self.state.snapshot_before()
            try:
                # 1. JS click attempt on any captcha-like element (best effort)
                js_result = safe(lambda: sb.execute_script("""
                    (() => {
                        const sels = '.recaptcha-checkbox-border, [role="checkbox"], #checkbox, '
                                   + '.cf-turnstile-checkbox, [data-action="challenge"], '
                                   + '.challenge-button, .hcaptcha-checkbox';
                        const frames = document.querySelectorAll('iframe');
                        for (const f of frames) {
                            try {
                                const doc = f.contentDocument || f.contentWindow?.document;
                                if (!doc) continue;
                                const cb = doc.querySelector(sels);
                                if (cb) { cb.click(); return 'clicked in iframe'; }
                            } catch(e) {}  // cross-origin — expected for CF/hCaptcha
                        }
                        const top = document.querySelector(sels);
                        if (top) { top.click(); return 'clicked top-level'; }
                        return 'no captcha element reachable';
                    })()
                """), "no captcha element reachable")

                sb.sleep(2)
                # 2. VERIFY: is a challenge still on screen? This is the honesty fix.
                challenge_state = safe(lambda: sb.execute_script("""
                    (() => {
                        const body = (document.body && document.body.innerText) || '';
                        const markers = ['just a moment', 'checking your browser',
                                         'cloudflare', 'ray id', 'verify you are human',
                                         'hcaptcha', 'recaptcha', 'enable javascript and cookies'];
                        const found = markers.filter(m => body.toLowerCase().includes(m));
                        const ifr = document.querySelectorAll(
                            'iframe[src*="challenges.cloudflare.com"], '
                            + 'iframe[src*="hcaptcha"], iframe[src*="recaptcha"], '
                            + 'iframe[src*="turnstile"]').length;
                        return JSON.stringify({markers: found, captcha_iframes: ifr});
                    })()
                """), '{"markers":[],"captcha_iframes":0}')
                try: challenge_state = json.loads(challenge_state)
                except Exception: challenge_state = {"markers": [], "captcha_iframes": 0}

                still_blocked = bool(challenge_state.get("markers")) or \
                                bool(challenge_state.get("captcha_iframes"))
                capture = self._capture_with_fingerprint(sb, self.state)
                capture["captcha_solved"] = not still_blocked      # HONEST verdict
                capture["captcha_attempt"] = js_result
                capture["challenge_state"] = challenge_state
                if still_blocked:
                    capture["hint"] = ("Persistent CDP browser cannot click this cross-origin "
                                       "captcha. Call POST /uc {action:'open', url:<same url>, "
                                       "click_captcha:true} to use SB(uc=True) + "
                                       "uc_gui_click_captcha (real pyautogui click).")
                self._ok(capture)
            except Exception as e:
                return self._err(f"solve_captcha failed: {e}")

        elif path == "/uc":
            # The REAL captcha bypass: SB(uc=True) dedicated Chrome +
            # uc_gui_click_captcha (pyautogui physically clicks the checkbox).
            # See BrowserState.open_uc for why uc_open_with_reconnect is avoided.
            # Multi-action: one persistent UC session so the agent can
            # open→click_captcha→type→submit on the SAME CF-cleared page.
            action = body.get("action", "open")
            try:
                if action == "open":
                    url = body.get("url", "")
                    if not url: return self._err("uc open requires url", 400)
                    click_captcha = body.get("click_captcha", True)
                    do_handoff = body.get("handoff", True)
                    ucsb = self.state.open_uc()
                    ucsb.open(url)
                    ucsb.sleep(2)
                    captcha_clicked = False
                    if click_captcha:
                        try:
                            ucsb.uc_gui_click_captcha()
                            captcha_clicked = True
                            ucsb.sleep(3)
                        except Exception as e:
                            # uc_gui_click_captcha raises if no captcha found —
                            # that's fine, means the page has no challenge.
                            pass
                    capture = self._capture_uc(ucsb)
                    capture["captcha_clicked"] = captcha_clicked
                    capture["uc_session_open"] = True
                    # Cookie handoff to the persistent browser so subsequent
                    # /navigate calls (same domain) inherit cf_clearance etc.
                    handoff_result = None
                    if do_handoff:
                        handoff_result = self._uc_handoff_cookies(ucsb, url)
                        capture["cookie_handoff"] = handoff_result
                    self._ok(capture)

                elif action == "click":
                    ucsb = self.state.uc_sb
                    if ucsb is None: return self._err("no open UC session — call action:open first", 400)
                    selector = body.get("selector", "")
                    by = body.get("by", "css")  # css | text | xpath
                    text = body.get("text", "")
                    self.state.snapshot_before()
                    if text:
                        ucsb.click_text(text)
                    elif selector:
                        ucsb.click(selector, by=by)
                    else:
                        return self._err("uc click requires selector or text", 400)
                    ucsb.sleep(1)
                    self._ok(self._capture_uc(ucsb))

                elif action == "type":
                    ucsb = self.state.uc_sb
                    if ucsb is None: return self._err("no open UC session", 400)
                    selector = body.get("selector", "")
                    text = body.get("text", "")
                    submit = body.get("submit", False)
                    clear = body.get("clear", True)
                    if not selector or text is None:
                        return self._err("uc type requires selector + text", 400)
                    if clear:
                        try: ucsb.clear(selector)
                        except Exception: pass
                    ucsb.type(selector, text)
                    if submit:
                        try: ucsb.submit(selector)
                        except Exception: ucsb.press_keys("Enter")
                    ucsb.sleep(1)
                    self._ok(self._capture_uc(ucsb))

                elif action == "read":
                    ucsb = self.state.uc_sb
                    if ucsb is None: return self._err("no open UC session", 400)
                    self._ok(self._capture_uc(ucsb))

                elif action == "evaluate":
                    ucsb = self.state.uc_sb
                    if ucsb is None: return self._err("no open UC session", 400)
                    code = body.get("code", "")
                    if not code: return self._err("uc evaluate requires code", 400)
                    if code.startswith("return "): code = code[7:]
                    res = safe(lambda: ucsb.execute_script(code), None)
                    self._ok({"result": res, "type": type(res).__name__})

                elif action == "cookies":
                    ucsb = self.state.uc_sb
                    if ucsb is None: return self._err("no open UC session", 400)
                    cact = body.get("cookie_action", "get")
                    if cact == "get":
                        self._ok({"cookies": ucsb.get_cookies() or []})
                    elif cact == "inject_persistent":
                        self._ok({"cookie_handoff": self._uc_handoff_cookies(ucsb, body.get("url", ""))})
                    else:
                        return self._err(f"unknown cookie_action: {cact}", 400)

                elif action == "close":
                    self.state._close_uc()
                    self._ok({"uc_session_open": False})

                else:
                    return self._err(f"unknown uc action: {action}. Use open|click|type|read|evaluate|cookies|close", 400)
            except Exception as e:
                import traceback
                traceback.print_exc(file=sys.stderr)
                return self._err(f"uc {action} failed: {e}")

        elif path == "/accept_cookies":
            # GDPR/CCPA cookie-consent banner dismissal. Curated selector list
            # covering the major consent platforms (OneTrust, TrustArc, CookieBot,
            # Quantcast, Didomi, BBC/REMODEGG) + text-based fallback for custom
            # banners. More reliable than SoM-guessing: BBC's banner produces
            # ZERO candidates in the SoM element map (the banner renders inside a
            # trust-elevated container the labeler skips), but this endpoint finds
            # it via CSS id/class patterns the SoM path ignores.
            if sb is None: return self._err("browser not available")
            # Ordered most-specific → generic. The first match wins.
            CONSENT_SELECTORS = [
                "#onetrust-accept-btn-handler",              # OneTrust (LinkedIn, many enterprise)
                "#truste-consent-button", ".truste-button2", # TrustArc
                "#CybotCookiebotDialogBodyLevelButtonAcceptAll",  # CookieBot
                "#CybotCookiebotDialogBodyButtonAccept",
                ".qc-cmp2-summary-buttons button[mode='primary']",  # Quantcast
                "#qcm-accept-all",
                "#didomi-notice-agree-button",              # Didomi
                ".tcfcp", ".tcfcp-btn",                      # misc TCF
                "[data-testid='cookie-policy-dialog-accept-button']",  # React apps
                "button[data-testid='accept-all-cookies']",
                "button#accept-recommended-btn-handler",     # OneTrust variant
                ".cc-accept", ".cookie-accept",              # generic
                "#ccc-notify-accept",                        # BBC legacy
                "button.sp_choice_type_11",                  # SourcePoint
            ]
            ACCEPT_TEXTS = ["accept all", "accept all & close", "accept all cookies",
                            "i agree", "agree to all", "allow all", "got it",
                            "accept & close", "accept the use of cookies",
                            "agree", "ok", "accept", "allow cookies", "consent"]
            try:
                # 1. Curated CSS selectors (fast path)
                clicked = None
                for sel in CONSENT_SELECTORS:
                    visible = safe(lambda s=sel: sb.execute_script(
                        "(() => { const el = document.querySelector(" + js_str(s) + "); "
                        "if (!el) return false; const r = el.getBoundingClientRect(); "
                        "return !!(r.width && r.height); })()"), False)
                    if visible:
                        safe(lambda s=sel: sb.execute_script(
                            "document.querySelector(" + js_str(s) + ").click()"), None)
                        clicked = sel
                        break
                # 2. Text-based fallback (buttons/links whose text matches)
                if not clicked:
                    import json as _json
                    res = safe(lambda: sb.execute_script(
                        "(() => {"
                        "  const textRe = /^(accept all.*|i agree.*|agree to all.*|allow all.*|"
                        "got it.*|accept & close.*|accept.*|agree.*|allow cookies.*|ok|consent.*)$/i;"
                        "  const els = Array.from(document.querySelectorAll("
                        "    'button, a, input[type=button], input[type=submit], div[role=button]'));"
                        "  for (const el of els) {"
                        "    const t = (el.innerText || el.value || '').trim();"
                        "    if (t.length > 0 && t.length < 40 && textRe.test(t)) {"
                        "      const r = el.getBoundingClientRect();"
                        "      if (r.width && r.height) { el.click(); return t; }"
                        "    }"
                        "  }"
                        "  return null;"
                        "})()"), None)
                    if res:
                        clicked = "text:" + res[:40]
                sb.sleep(1.2)
                capture = self._capture_with_fingerprint(sb, self.state)
                capture["cookies_accepted"] = bool(clicked)
                capture["accept_method"] = clicked or "no-banner-found"
                self._ok(capture)
            except Exception as e:
                return self._err(f"accept_cookies failed: {e}")

        elif path == "/warmup_history":
            # Build fingerprint legitimacy: visit benign high-traffic sites with
            # realistic dwell times + scroll, so the browser's history + cookie
            # jar + TLS fingerprint look like a real user before the real task.
            # Combats "fresh automation" heuristics on job sites (Workday,
            # Greenhouse CF rules) and Twitter that flag a Chrome whose only
            # navigation is about:blank → their-domain.
            if sb is None:
                if not self.state.ensure(): return self._err("browser not available")
                sb = self.state.sb
            DEFAULT_SITES = [
                "https://en.wikipedia.org/wiki/Web_browser",
                "https://www.bbc.com/news",
                "https://github.com/",
                "https://stackoverflow.com/",
                "https://news.ycombinator.com/",
                "https://www.google.com/",
            ]
            urls = body.get("urls") or DEFAULT_SITES
            dwell = float(body.get("dwell", 3.0))   # seconds per site
            scroll = body.get("scroll", True)
            visited = []
            import random as _r
            for u in urls:
                t0 = time.time()
                try:
                    sb.get(u)
                    sb.sleep(dwell + _r.uniform(-0.6, 1.4))
                    if scroll:
                        safe(lambda: sb.execute_script(
                            "window.scrollBy(0, window.innerHeight * "
                            "(0.3 + 0.5*Math.random()))"), None)
                        sb.sleep(0.6)
                    title = safe(lambda: sb.get_title() or "", "")
                    url_now = safe(lambda: sb.get_current_url() or "", "")
                    visited.append({"url": u, "final_url": url_now,
                                    "title": title[:80],
                                    "ok": True, "ms": int((time.time() - t0) * 1000)})
                except Exception as e:
                    visited.append({"url": u, "ok": False, "error": str(e)[:120]})
            self._ok({"visited": visited, "count": len(visited),
                      "history_built": len([v for v in visited if v.get("ok")])})

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
            trusted = bool(body.get("trusted"))

            before_handles, _ = self._capture_tabs_info(sb)
            self.state.snapshot_before()

            if trusted:
                # Trusted CDP click (isTrusted=true) for anti-bot sites that
                # ignore Selenium/JS synthetic clicks (Cloudflare Turnstile,
                # behavioral fingerprinting). Scroll into view first —
                # dispatchMouseEvent hits nothing off-screen — then drive the
                # real cursor at the element's viewport center.
                safe(lambda: sb.execute_script(f"{SCROLL_INTO_VIEW_JS}({js_str(selector)})"))
                ctr = safe(lambda: json.loads(
                    sb.execute_script(f"{ELEMENT_CENTER_JS}({js_str(selector)})") or "{}"), {})
                if not ctr.get("ok"):
                    return self._err("trusted click: " + ctr.get("error", "element not found"), 400)
                try:
                    _trusted_cdp_click(sb, ctr["x"], ctr["y"])
                    click_method = "trusted_cdp"
                except Exception as e:
                    return self._err(f"trusted click failed: {e}")
            else:
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
            trusted = bool(body.get("trusted"))

            self.state.snapshot_before()

            if trusted:
                # Trusted typing via CDP Input.insertText — fires a real
                # (isTrusted) input event through the browser editing pipeline,
                # so React/Vue controlled inputs update WITHOUT the
                # native-value-setter workaround in CDP_TYPE_JS. Focus + clear
                # via JS first (insertText lands at the caret of the focused
                # editable). The trusted path is strictly more correct than the
                # setter, not a fallback — opt-in via trusted: true for
                # keystroke-fingerprinting defenses.
                escaped_sel = selector.replace("\\", "\\\\").replace("'", "\\'")
                safe(lambda: sb.execute_script(
                    f'(() => {{ const el = document.querySelector(\'{escaped_sel}\'); '
                    f'if (!el) return; el.scrollIntoView({{block:"center"}}); el.focus(); '
                    f'if ({str(clear).lower()}) {{ el.value = ""; '
                    f'el.dispatchEvent(new Event("input", {{bubbles:true}})); }} }})()'
                ))
                try:
                    _trusted_cdp_type(sb, text)
                except Exception as e:
                    return self._err(f"trusted type failed: {e}")
            else:
                # Single path: the CDP native-value-setter (React/Vue compatible).
                # No silent fallback to sb.type() — that's a no-fallbacks-no-aliases
                # violation AND it masked the dead CDP_TYPE_JS for the whole pre-fix
                # run (the multi-line `return {X_JS}` bug made it throw every time,
                # the handler swallowed it, and sb.type papered over the gap). If the
                # setter fails, surface the error so the caller knows typing failed.
                escaped_sel = selector.replace("\\", "\\\\").replace("'", "\\'")
                escaped_text = text.replace("\\", "\\\\").replace("'", "\\'")
                js_code = f'{CDP_TYPE_JS}(\'{escaped_sel}\', \'{escaped_text}\', {str(clear).lower()})'
                result = safe(lambda: json.loads(sb.execute_script(js_code) or "{}"), {})
                if not isinstance(result, dict) or not result.get("ok"):
                    err_detail = str(result.get("error") or "unknown") if isinstance(result, dict) else str(result)
                    return self._err(f"type failed: {err_detail}")

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
            self._ok(self._capture_with_fingerprint(sb, self.state, full=False))

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
            self._ok(self._capture_with_fingerprint(sb, self.state, full=False))

        elif path == "/evaluate":
            code = body.get("code", "").strip()
            if not code: return self._err("missing code", 400)
            if sb is None: return self._err("browser not available")
            # Normalize a leading `return ` (a common caller convention for
            # marking an expression). SeleniumBase CDP execute_script only
            # strips `return` from the LAST line, so `return <multi-line expr>`
            # leaves a top-level `return` → "Illegal return statement". We strip
            # exactly one leading `return` ourselves and pass the bare
            # expression, which Playwright evaluates directly. Statement blocks
            # (no leading return) pass through untouched.
            if code.startswith("return ") or code.startswith("return\t"):
                code = code[6:].lstrip()
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
            # sb_cdp native: sb.get_tabs() returns Tab objects. The old
            # Selenium-style sb.driver.window_handles path is GONE in sb_cdp
            # (sb.driver is a Browser, not a WebDriver) — using it raised
            # "'Browser' object has no attribute 'window_handles'" on every call.
            if sb is None: return self._err("browser not available")
            try:
                raw_tabs = sb.get_tabs() or []
                active = safe(lambda: sb.get_active_tab(), None)
                tabs = []
                for i, t in enumerate(raw_tabs):
                    tabs.append({
                        "index": i,
                        "url": safe(lambda t=t: getattr(t, "url", "") or "", ""),
                        "title": "",  # title requires switching; skip for cheapness
                        "tab_id": safe(lambda t=t: getattr(t, "tab_id", "") or str(t), "")[:32],
                        "active": t is active,
                    })
                self._ok({"tabs": tabs})
            except Exception as e:
                return self._err(f"tab listing failed: {e}")

        elif path == "/new_tab":
            url = body.get("url", "about:blank")
            if sb is None: return self._err("browser not available")
            target = url if url and url != "about:blank" else "about:blank"
            escaped = target.replace("\\", "\\\\").replace("'", "\\'")
            # Each step individually wrapped — sb_cdp's tab APIs crash
            # unpredictably ("cannot unpack non-iterable NoneType") but the
            # tab IS created by window.open. Return ok regardless so the
            # agent can proceed (it'll /tabs to verify).
            try: sb.execute_script(f"window.open('{escaped}', '_blank');")
            except Exception: pass
            sb.sleep(1)
            try: sb.switch_to_newest_tab()
            except Exception: pass
            sb.sleep(2)
            try:
                capture = self._capture_with_fingerprint(sb, self.state)
            except Exception:
                capture = {"page_data": {"url": url, "text": ""}, "element_map": []}
            self._ok(capture)

        elif path == "/switch_tab":
            index = body.get("index", 0)
            if sb is None: return self._err("browser not available")
            try:
                tabs = sb.get_tabs() or []
                if not (0 <= index < len(tabs)):
                    return self._err(f"tab index {index} out of range (0-{len(tabs)-1})", 400)
                # sb_cdp native: switch_to_tab takes a Tab object (from get_tabs).
                sb.switch_to_tab(tabs[index])
                sb.sleep(1)
                self._ok(self._capture_with_fingerprint(sb, self.state))
            except Exception as e:
                self._err(f"switch tab failed: {e}")

        elif path == "/close_tab":
            if sb is None: return self._err("browser not available")
            try:
                tabs = sb.get_tabs() or []
                if len(tabs) <= 1: return self._err("can't close last tab")
                # sb_cdp native: close_active_tab() closes the current tab and
                # the harness auto-selects another; switch_to_newest_tab() makes
                # the remaining tab canonical for subsequent calls.
                sb.close_active_tab()
                sb.switch_to_newest_tab()
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
                # Thin delegate to the module-level helper (single source of
                # truth — also used by /save_session and the UC handoff).
                return cookie_to_dict(c)

            def _dicts_to_cookie_params(dicts):
                # Thin delegate to the module-level helper (single source of
                # truth — also used by /restore_session and the UC handoff).
                # See dicts_to_cookie_params for the `expires` drop rationale.
                return dicts_to_cookie_params(dicts)

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
                # sb_cdp's get_all_cookies() returns CDP Cookie dataclass instances
                # (not JSON-serializable) — project to dicts via cookie_to_dict.
                raw_cookies = sb.get_all_cookies() or []
                cookies = [cookie_to_dict(c) for c in raw_cookies]
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
                # Restore cookies. The saved file holds plain dicts;
                # sb_cdp's set_all_cookies needs CDP CookieParam instances —
                # dicts_to_cookie_params converts and DROPS expires (CDP
                # Network.setCookies silently rejects the whole batch if any
                # cookie carries expires).
                if payload.get("cookies"):
                    sb.set_all_cookies(dicts_to_cookie_params(payload["cookies"]))
                # Restore localStorage
                for k, v in payload.get("localStorage", {}).items():
                    try: sb.set_local_storage_item(str(k), str(v))
                    except Exception: pass
                self._ok({"restored": True, "path": sess_path,
                          "cookies": len(payload.get("cookies", [])),
                          "storage_items": len(payload.get("localStorage", {}))})
            except Exception as e:
                self._err(f"restore_session failed: {e}")

        # ── Drag-and-drop ──────────────────────────────────────────────────
        elif path == "/drag":
            if sb is None: return self._err("browser not available")
            strategy = body.get("strategy", "auto")
            steps = int(body.get("steps", 40) or 40)
            original_strategy = strategy

            # Resolve SOURCE: optional selector (from_index/from_selector) and/or coords.
            src_sel = ""
            src_draggable = False
            if body.get("from_selector") or body.get("from_index"):
                src_sel, err = self._resolve_selector(
                    {"selector": body.get("from_selector", ""), "index": body.get("from_index", 0)})
                if err: return self._err("source: " + err, 400)
            src_x = body.get("from_x"); src_y = body.get("from_y")
            if src_sel and (src_x is None or src_y is None):
                # Trusted CDP drags need the element IN the viewport — scroll first,
                # then read post-scroll center + draggable flag.
                safe(lambda: sb.execute_script(f"{SCROLL_INTO_VIEW_JS}({js_str(src_sel)})"), None)
                sb.sleep(0.15)
                r = safe(lambda: json.loads(sb.execute_script(
                    f"{ELEMENT_CENTER_JS}({js_str(src_sel)})") or "{}"), {})
                if not r.get("ok"): return self._err("source: " + r.get("error", "not found"))
                src_x, src_y = r["x"], r["y"]
                src_draggable = bool(r.get("draggable"))
            if (src_x is None or src_y is None) and not src_sel:
                return self._err("drag needs a source: from_index/from_selector OR from_x/from_y", 400)

            # Resolve TARGET: offset (dx/dy) > element (to_*) > coords (to_x/to_y).
            tgt_sel = ""
            tgt_x = body.get("to_x"); tgt_y = body.get("to_y")
            offset_drag = body.get("dx") is not None or body.get("dy") is not None
            if offset_drag:
                if src_x is None or src_y is None: return self._err("offset drag needs a resolved source position", 400)
                tgt_x = src_x + (body.get("dx") or 0); tgt_y = src_y + (body.get("dy") or 0)
            elif body.get("to_selector") or body.get("to_index"):
                tgt_sel, err = self._resolve_selector(
                    {"selector": body.get("to_selector", ""), "index": body.get("to_index", 0)})
                if err: return self._err("target: " + err, 400)
                r = safe(lambda: json.loads(sb.execute_script(
                    f"{ELEMENT_CENTER_JS}({js_str(tgt_sel)})") or "{}"), {})
                if not r.get("ok"): return self._err("target: " + r.get("error", "not found"))
                tgt_x, tgt_y = r["x"], r["y"]
            elif tgt_x is not None and tgt_y is not None:
                pass
            else:
                return self._err("drag needs a target: to_index/to_selector, to_x/to_y, or dx/dy", 400)

            # Auto-pick: native HTML5 DnD only when the source is genuinely
            # draggable AND it's an element-to-element drag (HTML5 needs element
            # handles). Otherwise trusted-physics — the path that moves native
            # sliders and fires the pointer events dnd-kit watches for.
            if strategy == "auto":
                strategy = "html5" if (src_draggable and tgt_sel and not offset_drag) else "physics"

            self.state.snapshot_before()

            def _do(m):
                if m == "html5":
                    if not (src_sel and tgt_sel):
                        return "physics-fallback-needed", None
                    res = safe(lambda: json.loads(sb.execute_script(
                        f"{SIMULATE_DND_JS}({js_str(src_sel)}, {js_str(tgt_sel)})") or "{}"), {})
                    return ("ok" if res.get("ok") else "html5 failed: " + res.get("error", "")), res
                # physics = trusted CDP drag
                try:
                    _trusted_cdp_drag(sb, src_x, src_y, tgt_x, tgt_y, steps)
                    return "ok", None
                except Exception as e:
                    return "physics failed: " + str(e), None

            method = strategy
            status, _ = _do(method)
            if status == "physics-fallback-needed":
                method, status, _ = "physics", _do("physics")[0], None
            sb.sleep(0.6)
            capture = self._capture_with_fingerprint(sb, self.state, full=False)

            # Verify-and-retry: if the page didn't react AND we auto-picked, try
            # the other strategy once (HTML5↔physics) — but only when we have
            # both selectors to switch between. Closes the "auto guessed wrong"
            # case (e.g. a non-draggable list that needs pointer events, or a
            # draggable that needs the synthetic DnD chain).
            retried = False
            if (original_strategy == "auto" and not capture.get("page_changed")
                    and src_sel and tgt_sel and status.startswith("ok")):
                other = "physics" if method == "html5" else "html5"
                s2, _ = _do(other)
                if s2.startswith("ok"):
                    method, retried = other, True
                    sb.sleep(0.6)
                    capture = self._capture_with_fingerprint(sb, self.state, full=False)

            if not status.startswith("ok") and not retried:
                return self._err(status)
            capture["drag_method"] = method
            capture["auto_retried"] = retried
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
            self._ok(self._capture_with_fingerprint(sb, self.state, full=False))

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
            trusted = bool(body.get("trusted"))
            x = body.get("x"); y = body.get("y")
            if x is None or y is None:
                if body.get("selector") or body.get("index"):
                    selector, err = self._resolve_selector(body)
                    if err: return self._err(err, 400)
                    if trusted:
                        # Trusted coordinate click needs the element on-screen
                        # (off-screen dispatchMouseEvent hits nothing).
                        safe(lambda: sb.execute_script(f"{SCROLL_INTO_VIEW_JS}({js_str(selector)})"))
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
            if trusted:
                # Trusted CDP click_at (isTrusted=true) — same primitive as
                # /click trusted, honoring button/right/double flags.
                btn_name = "right" if is_right == "true" else "left"
                count = 2 if is_double == "true" else 1
                try:
                    _trusted_cdp_click(sb, x, y, button=btn_name, click_count=count)
                    res = {"ok": True, "method": "trusted_cdp"}
                except Exception as e:
                    res = {"ok": False, "error": str(e)}
            else:
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
            self._ok(self._capture_with_fingerprint(sb, self.state, full=False))

        elif path == "/iframe":
            if sb is None: return self._err("browser not available")
            action = body.get("action", "list")
            if action == "list":
                frames = safe(lambda: json.loads(sb.execute_script(
                    "return JSON.stringify(Array.from(document.querySelectorAll('iframe')).map((f,i)=>({"
                    "index:i, name:f.name||'', id:f.id||'', src:(f.src||'').substring(0,120), "
                    "title:(f.title||f.getAttribute('aria-label')||'').substring(0,80)})))") or "[]"), [])
                self._ok({"frames": frames, "total": len(frames)})
            elif action in ("enter", "exit"):
                # RETIRED (no-legacy-left-behind): CDP has no global frame-context
                # switch like WebDriver's switch_to_frame / switch_to_default_content
                # — those methods don't exist on the SeleniumBase CDP Chrome object,
                # so the old enter/exit actions were silently broken (AttributeError
                # swallowed → "iframe enter failed"). Use action='click'/'evaluate'
                # with inner_selector/code, which bridges through contentDocument.
                return self._err(
                    f"iframe action '{action}' is retired — CDP has no WebDriver-style "
                    "frame switch. Use action='click' (selector=iframe, inner_selector="
                    "in-frame element) or action='evaluate' (selector=iframe, code=JS).", 400)
            elif action == "click":
                selector, err = self._resolve_selector(body)
                if err: return self._err("iframe: " + err, 400)
                inner = body.get("inner_selector")
                if not inner: return self._err("iframe click needs inner_selector (the in-frame element)", 400)
                # Same-origin bridge: reach into the iframe's contentDocument. We
                # also focus+dispatch a real click so listeners fire. contentDocument
                # access throws for cross-origin iframes — surface that honestly.
                esc_ifr = selector.replace("\\", "\\\\").replace("'", "\\'")
                esc_inner = inner.replace("\\", "\\\\").replace("'", "\\'")
                res = safe(lambda: json.loads(sb.execute_script(
                    f"((ifrSel, innerSel) => {{"
                    f"  const ifr = document.querySelector(ifrSel);"
                    f"  if (!ifr) return JSON.stringify({{ok:false, error:'iframe not found'}});"
                    f"  let doc; try {{ doc = ifr.contentDocument || (ifr.contentWindow && ifr.contentWindow.document); }}"
                    f"  catch (e) {{ return JSON.stringify({{ok:false, cross_origin:true, "
                    f"    error:'cross-origin iframe — contentDocument blocked by SOP'}}); }}"
                    f"  if (!doc) return JSON.stringify({{ok:false, error:'iframe has no contentDocument'}});"
                    f"  const el = doc.querySelector(innerSel);"
                    f"  if (!el) return JSON.stringify({{ok:false, error:'in-frame element not found: ' + innerSel}});"
                    f"  el.scrollIntoView({{block:'center'}});"
                    f"  try {{ el.focus(); }} catch (e) {{}}"
                    f"  el.click();"
                    f"  return JSON.stringify({{ok:true, tag: el.tagName.toLowerCase()}});"
                    f"}})('{esc_ifr}', '{esc_inner}')") or "{}"), {})
                if not res.get("ok"):
                    return self._err("iframe click failed: " + res.get("error", ""))
                sb.sleep(0.6)
                self._ok(self._capture_with_fingerprint(sb, self.state))
            elif action == "evaluate":
                selector, err = self._resolve_selector(body)
                if err: return self._err("iframe: " + err, 400)
                code = (body.get("code") or "").strip()
                if not code: return self._err("iframe evaluate needs code", 400)
                if code.startswith("return ") or code.startswith("return\t"):
                    code = code[6:].lstrip()
                esc_ifr = selector.replace("\\", "\\\\").replace("'", "\\'")
                esc_code = code.replace("\\", "\\\\").replace("'", "\\'")
                res = safe(lambda: json.loads(sb.execute_script(
                    f"((ifrSel, expr) => {{"
                    f"  const ifr = document.querySelector(ifrSel);"
                    f"  if (!ifr) return JSON.stringify({{ok:false, error:'iframe not found'}});"
                    f"  let win; try {{ win = ifr.contentWindow; }}"
                    f"  catch (e) {{ return JSON.stringify({{ok:false, cross_origin:true, "
                    f"    error:'cross-origin iframe — contentWindow blocked by SOP'}}); }}"
                    f"  if (!win) return JSON.stringify({{ok:false, error:'iframe has no contentWindow'}});"
                    f"  let result; try {{ result = win.eval(expr); }}"
                    f"  catch (e) {{ return JSON.stringify({{ok:false, error:'eval failed: ' + e.message}}); }}"
                    f"  return JSON.stringify({{ok:true, result: String(result)}});"
                    f"}})('{esc_ifr}', '{esc_code}')") or "{}"), {})
                if not res.get("ok"):
                    return self._err("iframe evaluate failed: " + res.get("error", ""))
                self._ok({"result": res.get("result"), "type": "str"})
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
    use_chromium = "--use-chromium" in sys.argv
    # CDP port: default 9222 (shared instance). When set via --cdp-port or
    # SB_CDP_PORT env, each sb_server instance gets its own Chrome on a
    # unique CDP port — enabling multiple isolated browsers in one container.
    cdp_port = int(os.environ.get("SB_CDP_PORT", "9222"))
    for i, a in enumerate(sys.argv):
        if a == "--cdp-port" and i + 1 < len(sys.argv):
            cdp_port = int(sys.argv[i + 1])
    state = BrowserState(stealth=stealth, use_chromium=use_chromium, cdp_port=cdp_port)
    Handler.state = state
    _start_reaper_if_ephemeral(cdp_port)
    # ThreadingHTTPServer (not single-threaded HTTPServer): a stuck sb.get() in
    # one POST handler must NOT block /status or concurrent requests. With the
    # single-threaded server, a page that hangs on load ("Timeout loading …")
    # held the only accept-loop slot and made the whole org unresponsive —
    # /status returned empty, the agent couldn't recover. ThreadingHTTPServer
    # handles each request in its own daemon thread; BrowserState._lock still
    # serializes browser access so concurrent POSTs are safe. daemon_threads=True
    # (the class default since 3.7) prevents threads from blocking shutdown.
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    details = []
    if stealth: details.append("stealth")
    if use_chromium: details.append("chromium")
    extra = f" ({', '.join(details)})" if details else ""
    log(f"listening on :{port}{extra} download_dir={state._download_dir}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        state.close()
        server.server_close()

if __name__ == "__main__":
    main()
