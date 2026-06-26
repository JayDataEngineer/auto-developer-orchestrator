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
    /solve_captcha      {}                               → auto-detect + solve CF Turnstile / reCAPTCHA / hCaptcha / DataDome / FriendlyCaptcha
    /a11y               {}                               → accessibility tree (role + name per node)
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
import random
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


# ═══════════════════════════════════════════════════════════════════════════════
# Browser State
# ═══════════════════════════════════════════════════════════════════════════════

class BrowserState:
    """Persistent SeleniumBase browser with labeler cache and fingerprinting.

    Dual-Chrome waterfall:
      • Mode A (default) — attach to the supervisord-managed Chrome via
        sb_cdp.Chrome(host, port=9222). Fast, VNC-visible, persistent state.
        Works for the open web. Cloudflare detects the attach and serves the
        "Just a moment..." interstitial to it.
      • Mode Stealth (fallback) — spawn a fresh SB(uc=True, test=True,
        locale="en") Chrome, navigate via activate_cdp_mode(url). UC mode
        launches Chrome without the debugging port, navigates first, then
        switches to CDP — the documented delayed-attach pattern that
        Cloudflare's challenge platform does not flag. Spawned lazily, only
        when Mode A returns a CF challenge page.

    Routing: `self.sb` is a property that returns whichever Chrome `self._mode`
    points at. Endpoint code reads `state.sb` and stays mode-agnostic.
    """

    def __init__(self, stealth=False):
        # Vestigial flag — kept for backward compat with `--stealth` CLI arg.
        # Both modes use sb_cdp internally; stealth is now the dual-Chrome
        # waterfall, not a CLI toggle.
        self.stealth = stealth
        # Mode A: supervisord Chrome attach
        self.sb_attach = None
        self._attach_ctx = None
        # Mode Stealth: lazy SB UC Chrome
        self.sb_stealth = None
        self._stealth_ctx = None
        # Routing: "attach" (default) or "stealth"
        self._mode = "attach"
        # Domains that have triggered CF on Mode A — skip straight to Stealth
        # on subsequent navigations to avoid the round-trip penalty.
        self._cf_domains = set()
        self._lock = threading.Lock()
        self._last_element_map = []
        self._download_dir = "/tmp/sb_downloads"
        self._last_fingerprint = ""
        os.makedirs(self._download_dir, exist_ok=True)
        self._init_attach_browser()

    @property
    def sb(self):
        """Return whichever browser the current mode routes to."""
        if self._mode == "stealth" and self.sb_stealth is not None:
            return self.sb_stealth
        return self.sb_attach

    @property
    def mode(self):
        return self._mode

    def _init_attach_browser(self):
        self._close_attach_browser()
        # pyvirtualdisplay is mocked at module level (noop), so SeleniumBase
        # cannot create a hidden Xvfb. Chrome uses DISPLAY from environment
        # (:99 by default), making browser automation visible via VNC.
        #
        # ── Mode A: attach to supervisord Chrome ───────────────────────────
        # The sandbox container runs ONE Chrome via supervisord (PID 43, CDP
        # port 9222). sb_server ATTACHES to that Chrome instead of launching a
        # separate one. This keeps VNC showing the browser being automated.
        # Stray Chrome processes from prior sb_server versions are killed here.
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
            self.sb_attach = sb_cdp.Chrome("about:blank", host="127.0.0.1", port=9222)
            self._attach_ctx = None
            self._setup_cdp_downloads()
        except Exception as e:
            print(f"[sb_server] attach browser init failed: {e}", file=sys.stderr)
            import traceback as _tb
            _tb.print_exc(file=sys.stderr)
            self.sb_attach = None
            self._attach_ctx = None

    def _init_stealth_browser(self, url):
        """Spawn a fresh SB UC Chrome and navigate to URL with CF bypass.

        Uses the canonical SeleniumBase CF-bypass recipe:
          SB(uc=True, test=True, locale="en") + activate_cdp_mode(url).

        UC mode launches Chrome without --remote-debugging-port at process
        start (the fingerprint Cloudflare detects). activate_cdp_mode then
        switches mid-session to CDP for automation. The session persists
        across subsequent /click, /type, /evaluate calls until the agent
        navigates to a non-CF site (which falls back to Mode A).
        """
        self._close_stealth_browser()
        os.environ.setdefault("DISPLAY", ":99")
        try:
            from seleniumbase import SB
            log(f"spawning Mode Stealth for {url}")
            ctx = SB(uc=True, test=True, locale="en", xvfb=False)
            sb = ctx.__enter__()
            sb.activate_cdp_mode(url)
            self.sb_stealth = sb
            self._stealth_ctx = ctx
            self._mode = "stealth"
            log("Mode Stealth ready")
        except Exception as e:
            print(f"[sb_server] stealth browser init failed: {e}", file=sys.stderr)
            import traceback as _tb
            _tb.print_exc(file=sys.stderr)
            # Cleanup partial state
            try:
                if self._stealth_ctx is not None:
                    self._stealth_ctx.__exit__(None, None, None)
            except Exception:
                pass
            self.sb_stealth = None
            self._stealth_ctx = None
            # Stay in attach mode on failure
            self._mode = "attach"
            raise

    def _close_attach_browser(self):
        if self._attach_ctx is not None:
            try: self._attach_ctx.__exit__(None, None, None)
            except Exception: pass
            self._attach_ctx = None
        if self.sb_attach is not None:
            try: self.sb_attach.driver.stop()
            except Exception: pass
            self.sb_attach = None

    def _close_stealth_browser(self):
        if self._stealth_ctx is not None:
            try: self._stealth_ctx.__exit__(None, None, None)
            except Exception: pass
            self._stealth_ctx = None
        self.sb_stealth = None
        if self._mode == "stealth":
            self._mode = "attach"

    def switch_to_attach(self):
        """Switch routing back to Mode A. Does NOT tear down Stealth Chrome
        (kept warm in case the agent returns to a CF-protected domain)."""
        self._mode = "attach"

    def _setup_cdp_downloads(self):
        try:
            downloads_path = str(Path(self._download_dir).absolute())
            self._download_dir = downloads_path
        except Exception as e:
            print(f"[sb_server] download dir setup failed (non-fatal): {e}", file=sys.stderr)

    def looks_like_cf_challenge(self, sb):
        """Detect Cloudflare's 'Just a moment...' interstitial.

        CF challenge pages have tell-tale markers in title + body. We check
        both so a slow-loading page that hasn't yet replaced its title is
        still caught by its body text.
        """
        if sb is None:
            return False
        try:
            title = safe(lambda: sb.get_title() or "", "")
            body = safe(lambda: sb.execute_script(
                "return (document.body && document.body.innerText || '').slice(0,800)"
            ) or "", "")
        except Exception:
            return False
        cf_title_markers = ("Just a moment", "Attention Required!", "Access denied")
        cf_body_markers = (
            "Performing security verification",
            "Checking your browser before accessing",
            "Verify you are a human",
            "needs to review the security of your connection",
            "This website uses a security service to protect against malicious bots",
            "cdn-cgi/challenge-platform",
        )
        if any(m in title for m in cf_title_markers):
            return True
        if any(m in body for m in cf_body_markers):
            return True
        return False

    def remember_cf_domain(self, url):
        """Cache a domain as CF-protected so future navigations skip Mode A."""
        try:
            from urllib.parse import urlparse
            netloc = urlparse(url).netloc
            if netloc:
                self._cf_domains.add(netloc)
                # Also strip leading www. so www.example.com and example.com
                # hit the same cache entry.
                if netloc.startswith("www."):
                    self._cf_domains.add(netloc[4:])
        except Exception:
            pass

    def is_cf_domain(self, url):
        try:
            from urllib.parse import urlparse
            netloc = urlparse(url).netloc
            if not netloc:
                return False
            if netloc in self._cf_domains:
                return True
            if netloc.startswith("www.") and netloc[4:] in self._cf_domains:
                return True
            return False
        except Exception:
            return False

    def reset(self):
        with self._lock:
            self._close_stealth_browser()
            self._init_attach_browser()
            self._last_element_map = []
            self._last_fingerprint = ""
            self._cf_domains.clear()

    def ensure(self):
        if self.sb is None:
            self._init_attach_browser()
            self._last_element_map = []
        return self.sb is not None

    def close(self):
        self._close_stealth_browser()
        self._close_attach_browser()

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

# Behavioral-realism pre-roll. Real users move the cursor before clicking;
# sites that fingerprint input (DataDome, PerimeterX, Cloudflare Turnstile
# widget warmup) flag zero-movement clicks as suspicious. We synthesize a
# Bezier-curve path of mouseMoved events from the viewport center (or last
# known position) to the element's centroid, then let /click do its normal
# mousePressed+mouseReleased. Total cost: ~250ms, ~15-25 CDP round-trips.
_HUMANLIKE_STEPS = (12, 22)   # randomized step count per move
_HUMANLIKE_STEP_MS = (8, 25)  # randomized pause between steps
_HUMANLIKE_JITTER = 3         # max pixel offset from Bezier path

def _bezier_points(p0, p1, p2, p3, steps):
    """Cubic Bezier; returns list of (x,y) points excluding p0, including p3."""
    pts = []
    for i in range(1, steps + 1):
        t = i / steps
        u = 1 - t
        x = u*u*u*p0[0] + 3*u*u*t*p1[0] + 3*u*t*t*p2[0] + t*t*t*p3[0]
        y = u*u*u*p0[1] + 3*u*u*t*p1[1] + 3*u*t*t*p2[1] + t*t*t*p3[1]
        # add jitter so two clicks on the same element don't trace the same path
        x += random.uniform(-_HUMANLIKE_JITTER, _HUMANLIKE_JITTER)
        y += random.uniform(-_HUMANLIKE_JITTER, _HUMANLIKE_JITTER)
        pts.append((x, y))
    return pts

def _humanlike_move_to(sb, selector):
    """Move the cursor to the element center via Bezier curve.
    Returns a metadata dict {from, to, steps, duration_ms} or {error: str}.
    Never raises — worst case the click still goes through without pre-movement."""
    import random as _r
    try:
        # Resolve element bounds via JS (viewport coords)
        rect_json = sb.execute_script(
            f'return (function(){{var el=document.querySelector("{selector.replace(chr(34), chr(92)+chr(34))}");'
            'if(!el){return null;}var r=el.getBoundingClientRect();'
            'return {x:r.left+r.width/2, y:r.top+r.height/2, w:r.width, h:r.height};})()'
        )
        if not rect_json:
            return {"error": "element not found for pre-move"}
        rect = json.loads(rect_json) if isinstance(rect_json, str) else rect_json
        target_x, target_y = rect["x"], rect["y"]

        # Start from viewport center (good enough; we don't track last position)
        vw = sb.execute_script("return window.innerWidth") or 1280
        vh = sb.execute_script("return window.innerHeight") or 720
        start_x, start_y = vw / 2 + _r.uniform(-100, 100), vh / 2 + _r.uniform(-100, 100)

        # Two random control points between start and target for cubic Bezier
        cp_offset = 80
        c1 = (start_x + (target_x - start_x) * 0.3 + _r.uniform(-cp_offset, cp_offset),
              start_y + (target_y - start_y) * 0.3 + _r.uniform(-cp_offset, cp_offset))
        c2 = (start_x + (target_x - start_x) * 0.7 + _r.uniform(-cp_offset, cp_offset),
              start_y + (target_y - start_y) * 0.7 + _r.uniform(-cp_offset, cp_offset))

        steps = _r.randint(*_HUMANLIKE_STEPS)
        pts = _bezier_points((start_x, start_y), c1, c2, (target_x, target_y), steps)

        # Dispatch each step via cdp.input_.dispatch_mouse_event("mouseMoved")
        # We reach through sb.page (nodriver Tab) → connection → send.
        # mycdp is the runtime-registered CDP types package used by
        # seleniumbase.undetected.cdp_driver.element — same one whose
        # mousePressed/mouseReleased calls already produce trusted clicks.
        try:
            import mycdp as _cdp
            import mycdp.input_  # noqa: F401 — ensures dispatch_mouse_event is bound
        except Exception:
            _cdp = None

        tab = sb.page
        total_ms = 0
        for (px, py) in pts:
            step_ms = _r.randint(*_HUMANLIKE_STEP_MS)
            total_ms += step_ms
            try:
                if _cdp is not None:
                    sb.loop.run_until_complete(
                        tab.send(_cdp.input_.dispatch_mouse_event(
                            "mouseMoved", x=px, y=py
                        ))
                    )
                else:
                    # Fallback: raw CDP via evaluate_script
                    sb.execute_script(
                        f'(function(){{var e=new MouseEvent("mousemove",{{clientX:{px},clientY:{py},bubbles:true}});'
                        f'document.dispatchEvent(e);}})()'
                    )
            except Exception:
                pass
            time.sleep(step_ms / 1000.0)

        return {"from": [round(start_x, 1), round(start_y, 1)],
                "to": [round(target_x, 1), round(target_y, 1)],
                "steps": steps,
                "duration_ms": total_ms,
                "fallback_js": _cdp is None}
    except Exception as e:
        return {"error": f"pre-move failed: {e}"}

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

            waterfall = "attach"   # default — went through Mode A
            mode_used = "attach"

            # ── Cached CF domain? Skip Mode A, go straight to Stealth ──────
            # Avoids the ~5s penalty of trying Mode A and waiting for CF to
            # serve its interstitial on every navigation to a known-protected
            # site.
            if self.state.is_cf_domain(url):
                log(f"cache hit: {url} is a known CF domain → going straight to Stealth")
                try:
                    if self.state.sb_stealth is None:
                        self.state._init_stealth_browser(url)
                    else:
                        # Reuse warm Stealth Chrome — just navigate
                        self.state._mode = "stealth"
                        self.state.sb_stealth.activate_cdp_mode(url)
                    sb = self.state.sb_stealth
                    mode_used = "stealth"
                    waterfall = "stealth_cached"
                    sb.sleep(3)
                except Exception as e:
                    return self._err(f"Mode Stealth (cached) failed: {e}")
            else:
                # ── Waterfall step 1: try Mode A ──────────────────────────
                self.state.switch_to_attach()
                sb = self.state.sb_attach
                try:
                    sb.get(url)
                    sb.sleep(3)
                except Exception as e:
                    return self._err(f"Mode A navigate failed: {e}")

                # ── Waterfall step 2: detect CF challenge, fall back ───────
                if self.state.looks_like_cf_challenge(sb):
                    log(f"CF challenge detected on Mode A → spawning Stealth for {url}")
                    self.state.remember_cf_domain(url)
                    try:
                        self.state._init_stealth_browser(url)
                        sb = self.state.sb_stealth
                        mode_used = "stealth"
                        waterfall = "attach_then_stealth"
                        sb.sleep(3)
                        # If Stealth *also* hits CF, report honestly
                        if self.state.looks_like_cf_challenge(sb):
                            capture = self._capture_with_fingerprint(sb, self.state)
                            capture["mode"] = mode_used
                            capture["waterfall"] = "stealth_blocked"
                            capture["cf_still_present"] = True
                            self._ok(capture)
                            return
                    except Exception as e:
                        return self._err(f"Mode Stealth spawn failed: {e}")

            self.state.snapshot_before()
            capture = self._capture_with_fingerprint(sb, self.state)
            capture["mode"] = mode_used
            capture["waterfall"] = waterfall
            self._ok(capture)

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
            humanlike = bool(body.get("humanlike", False))

            before_handles, _ = self._capture_tabs_info(sb)
            self.state.snapshot_before()

            # Behavioral-realism pre-roll: move the cursor along a Bezier
            # curve to the element center before clicking. Default off —
            # costs ~250ms per click. Turn on for sites that fingerprint
            # input patterns (DataDome, PerimeterX, Turnstile widget warmup).
            humanlike_meta = None
            if humanlike:
                humanlike_meta = _humanlike_move_to(sb, selector)

            # Occlusion-aware click: try Selenium click, fall back to JS click
            click_method = "selenium"
            try:
                # Check if element is occluded
                occ_result = safe(lambda: json.loads(sb.execute_script(f'return {OCCLUSION_CHECK_JS}("{selector.replace(chr(34), chr(92)+chr(34))}")') or "{}"), {})
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
            if humanlike_meta is not None:
                capture["humanlike"] = humanlike_meta
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
                js_code = f'return {CDP_TYPE_JS}(\'{escaped_sel}\', \'{escaped_text}\', {str(clear).lower()})'
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
                result = json.loads(sb.execute_script(f'return {DROPDOWN_OPTIONS_JS}(\'{escaped}\')'))
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
                    f'return {SELECT_DROPDOWN_JS}(\'{escaped}\', {val_arg}, {text_arg})'))
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
            # POST (action=set): add a cookie
            # POST (action=clear): delete all cookies
            if sb is None: return self._err("browser not available")
            action = body.get("action", "get")
            try:
                if action == "get":
                    cookies = sb.get_all_cookies() or []
                    self._ok({"cookies": cookies})
                elif action == "set":
                    cookie = body.get("cookie", {})
                    # SeleniumBase set_all_cookies takes a list of cookie dicts
                    sb.set_all_cookies([cookie])
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
                # sb_cdp.Chrome doesn't expose upload_file publicly — reach
                # into the private name-mangled CDP method which takes the
                # nodriver Element + a list of file paths.
                element = sb.select(selector, timeout=5)
                sb._CDPMethods__send_file(element, file_path)
                self._ok({"uploaded": True, "selector": selector, "file": file_path})
            except Exception as e:
                self._err(f"upload failed: {e}")

        # ── CAPTCHA solver (Cloudflare Turnstile, reCAPTCHA, hCaptcha, DataDome, FriendlyCaptcha)
        # Calls sb.solve_captcha() which auto-detects the captcha type from
        # page source and dispatches the appropriate handler. For CF Turnstile
        # it tries ~20 selector strategies to find the widget and clicks via
        # CDP click_with_offset (isTrusted: true). For DataDome slider it
        # uses PyAutoGUI. Returns whether a captcha was detected + clicked.
        elif path == "/solve_captcha":
            if sb is None: return self._err("browser not available")
            try:
                # Read page source once so detection is stable across the call
                source = safe(lambda: sb.get_page_source() or "", "")
                on_cf = sb._on_a_cf_turnstile_page(source)
                on_recaptcha = sb._on_a_g_recaptcha_page(source) if hasattr(sb, "_on_a_g_recaptcha_page") else False
                on_datadome = sb._on_a_datadome_slider_page() if hasattr(sb, "_on_a_datadome_slider_page") else False
                on_incapsula = sb._on_an_incapsula_hcaptcha_page() if hasattr(sb, "_on_an_incapsula_hcaptcha_page") else False
                on_friendly = sb._on_a_friendly_captcha_page() if hasattr(sb, "_on_a_friendly_captcha_page") else False
                detected = on_cf or on_recaptcha or on_datadome or on_incapsula or on_friendly
                if not detected:
                    self._ok({
                        "solved": False,
                        "detected": False,
                        "reason": "no captcha detected on page",
                    })
                    return
                # sb.solve_captcha() == sb.click_captcha() == __click_captcha(use_cdp=True)
                # Returns True if it found + clicked the widget, False otherwise.
                clicked = safe(lambda: sb.solve_captcha(), False)
                # Give the widget a moment to validate the click
                sb.sleep(2.0)
                # Re-read source to see if challenge is gone
                after_source = safe(lambda: sb.get_page_source() or "", "")
                still_on_cf = sb._on_a_cf_turnstile_page(after_source)
                # Capture page state for the caller
                capture = self._capture_with_fingerprint(sb, self.state)
                self._ok({
                    "solved": bool(clicked),
                    "detected": True,
                    "clicked": bool(clicked),
                    "initial": {
                        "cf_turnstile": on_cf,
                        "recaptcha": on_recaptcha,
                        "datadome": on_datadome,
                        "incapsula_hcaptcha": on_incapsula,
                        "friendly": on_friendly,
                    },
                    "still_on_cf_turnstile_page": still_on_cf,
                    "capture": capture,
                })
            except Exception as e:
                import traceback
                self._err(f"solve_captcha failed: {e}\n{traceback.format_exc()}")

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
