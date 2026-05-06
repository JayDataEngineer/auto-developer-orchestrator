#!/usr/bin/env python3
"""Persistent SeleniumBase browser server with SoM labeling and vision.

Runs as a supervised HTTP service inside the sandbox.
The agent sends commands via curl, browser state persists across calls.

Features:
    - SoM (Set-of-Marks) visual labeling: numbered boxes on interactive elements
    - Auto-screenshot after every page change → vision-in-the-loop via analyze_image
    - Page stats: viewport size, scroll position, element counts
    - Index-based interaction: click/type by SoM label index (no CSS selector needed)
    - CDP-based download tracking (handles click-triggered downloads)
    - Tab management, search, image extraction, JavaScript evaluation

Usage:
    sb_server.py [--port PORT] [--stealth]

Environment:
    SB_SERVER_PORT  Port to listen on (default: 9876)
    DISPLAY         X11 display (set by supervisord)

API — POST unless noted:
    /navigate       {"url":"..."}                    → page + SoM labels + screenshot
    /read           {}                               → page data (no re-navigation)
    /search         {"query":"..."}                  → Google search results
    /go_back        {}                               → back in history
    /refresh        {}                               → reload page
    /click          {"selector":"...","index":n}     → click element (index preferred)
    /type           {"selector":"...","text":"...","index":n,"submit":false} → type
    /scroll         {"direction":"down|up"}          → scroll
    /label          {}                               → SoM labels on current page
    /interact       {}                               → interactive elements list
    /extract_images {}                               → image URLs on current page
    /screenshot     {"path":"/tmp/shot.png"}         → save screenshot
    /download       {"url":"...","path":"..."}       → direct URL download
    /find_text      {"text":"..."}                   → scroll to text on page
    /evaluate       {"code":"..."}                   → execute JS, return result
    /run            {"code":"..."}                   → execute Python with `sb` loaded
    /tabs           {}                               → list open tabs
    /new_tab        {"url":"..."}                    → open new tab
    /switch_tab     {"index":n}                      → switch to tab
    /close_tab      {}                               → close current tab
    /reset          {}                               → kill and recreate browser
    GET /status     {}                               → browser alive check
"""

import sys
import json
import os
import io
import time
import traceback
import threading
import re
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path

os.environ["SB_NO_BORING_RC"] = "1"

MAX_TEXT = 4000
MAX_IMAGES = 50
MAX_LINKS = 30
MAX_ELEMENTS = 50
DEFAULT_PORT = 9876
SCREENSHOT_DIR = "/tmp"

# ═══════════════════════════════════════════════════════════════════════════════
# SoM (Set-of-Marks) Labeler JavaScript
# ═══════════════════════════════════════════════════════════════════════════════

SOM_LABELER_JS = r"""
(() => {
    // Remove previous overlay
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

    function safe(fn, fallback) {
        try { return fn(); } catch(e) { return fallback; }
    }

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
            if (current.id) {
                parts.unshift('#' + CSS.escape(current.id));
                break;
            }
            const parent = current.parentElement;
            if (!parent) break;
            const siblings = Array.from(parent.children).filter(c => c.tagName === current.tagName);
            if (siblings.length === 1) {
                parts.unshift(tag);
            } else {
                const idx = siblings.indexOf(current) + 1;
                parts.unshift(tag + ':nth-of-type(' + idx + ')');
            }
            current = parent;
        }
        return parts.join(' > ') || el.tagName.toLowerCase();
    }

    function processElement(el) {
        if (id > 50) return false;
        if (!isVisible(el)) return true;

        const rect = el.getBoundingClientRect();
        if (rect.width < 4 || rect.height < 4) return true;
        // Allow elements slightly off-screen top/bottom (scrollable pages)
        if (rect.bottom < -200 || rect.top > vh + 200) return true;

        const selector = buildSelector(el);
        if (seen.has(selector)) return true;
        seen.add(selector);

        // Extract text
        const tag = el.tagName.toLowerCase();
        let text = '';
        if (tag === 'input' || tag === 'textarea') {
            text = el.placeholder || el.value || el.name || el.getAttribute('aria-label') || el.type || '';
        } else if (tag === 'select') {
            text = el.name || el.getAttribute('aria-label') || '';
        } else {
            text = (el.textContent || '').trim();
        }
        text = text.substring(0, 80).replace(/\s+/g, ' ');

        // Skip empty non-interactive-looking elements
        if (!text && tag !== 'input' && tag !== 'select' && tag !== 'textarea' && tag !== 'button') return true;

        // Draw numbered label
        const label = document.createElement('div');
        label.style.cssText = 'position:fixed;pointer-events:none;background:rgba(220,38,38,0.88);color:white;font-size:11px;font-family:monospace;padding:1px 5px;border-radius:3px;z-index:1000000;line-height:1.3;white-space:nowrap;font-weight:bold;text-shadow:0 1px 2px rgba(0,0,0,0.5);';
        label.textContent = '' + id;
        label.style.left = Math.max(0, rect.left - 1) + 'px';
        label.style.top = Math.max(0, rect.top - 1) + 'px';
        overlay.appendChild(label);

        // Draw bounding box
        const box = document.createElement('div');
        box.style.cssText = 'position:fixed;pointer-events:none;border:2px solid rgba(220,38,38,0.5);z-index:999999;border-radius:2px;';
        box.style.left = rect.left + 'px';
        box.style.top = rect.top + 'px';
        box.style.width = rect.width + 'px';
        box.style.height = rect.height + 'px';
        overlay.appendChild(box);

        elements.push({
            index: id,
            tag: tag,
            text: text,
            selector: selector,
            x: Math.round(rect.left),
            y: Math.round(rect.top),
            w: Math.round(rect.width),
            h: Math.round(rect.height)
        });
        id++;
        return true;
    }

    try {
        const nodes = document.querySelectorAll(INTERACTIVE);
        for (let i = 0; i < nodes.length; i++) {
            if (!processElement(nodes[i])) break;
        }
    } catch(e) {}

    document.body.appendChild(overlay);
    return JSON.stringify(elements);
})()
"""

# ═══════════════════════════════════════════════════════════════════════════════
# Page Statistics JavaScript
# ═══════════════════════════════════════════════════════════════════════════════

PAGE_STATS_JS = r"""
(() => {
    return JSON.stringify({
        viewport_w: window.innerWidth,
        viewport_h: window.innerHeight,
        page_w: document.documentElement.scrollWidth,
        page_h: document.documentElement.scrollHeight,
        scroll_x: window.scrollX,
        scroll_y: window.scrollY,
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


# ═══════════════════════════════════════════════════════════════════════════════
# Browser State
# ═══════════════════════════════════════════════════════════════════════════════

class BrowserState:
    """Persistent SeleniumBase browser with labeler cache."""

    def __init__(self, stealth=False):
        self.stealth = stealth
        self.sb = None
        self._ctx = None  # UC Mode context manager if stealth
        self._lock = threading.Lock()
        self._last_element_map = []  # cached SoM labels
        self._download_dir = "/tmp/sb_downloads"
        os.makedirs(self._download_dir, exist_ok=True)
        self._init_browser()

    def _init_browser(self):
        self._close_browser()
        try:
            if self.stealth:
                from seleniumbase import SB
                ctx = SB(uc=True, test=True, locale="en", xvfb=True)
                ctx.__enter__()
                self.sb = ctx.sb
                self._ctx = ctx
                self.sb.activate_cdp_mode("about:blank")
            else:
                from seleniumbase import sb_cdp
                self.sb = sb_cdp.Chrome("about:blank", xvfb=True)
                self._ctx = None

            # Configure CDP for download tracking
            self._setup_cdp_downloads()
        except Exception as e:
            print(f"[sb_server] browser init failed: {e}", file=sys.stderr)
            self.sb = None
            self._ctx = None

    def _setup_cdp_downloads(self):
        """Configure CDP to handle browser-triggered downloads."""
        try:
            downloads_path = str(Path(self._download_dir).absolute())
            self.sb.execute_cdp_cmd("Page.setDownloadBehavior", {
                "behavior": "allow",
                "downloadPath": downloads_path
            })
            print(f"[sb_server] download directory: {downloads_path}", file=sys.stderr)
        except Exception as e:
            print(f"[sb_server] CDP download setup failed (non-fatal): {e}", file=sys.stderr)

    def _close_browser(self):
        if self._ctx is not None:
            try:
                self._ctx.__exit__(None, None, None)
            except Exception:
                pass
            self._ctx = None
        if self.sb is not None:
            try:
                self.sb.driver.stop()
            except Exception:
                pass
            self.sb = None

    def reset(self):
        with self._lock:
            self._init_browser()
            self._last_element_map = []

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


# ═══════════════════════════════════════════════════════════════════════════════
# Helpers
# ═══════════════════════════════════════════════════════════════════════════════

def safe(fn, default=None):
    try:
        return fn()
    except Exception:
        return default


def ts():
    return str(int(time.time()))


def now():
    return time.strftime("%H:%M:%S")


def log(msg):
    print(f"[sb_server {now()}] {msg}", file=sys.stderr)


def screenshot_path():
    return f"{SCREENSHOT_DIR}/sb_screenshot_{ts()}.png"


def run_labeler(sb, state):
    """Inject SoM labeler JS, returns element_map list."""
    try:
        result = sb.execute_script(SOM_LABELER_JS)
        element_map = json.loads(result) if isinstance(result, str) else result
        state._last_element_map = element_map
        return element_map
    except Exception as e:
        log(f"labeler failed: {e}")
        return state._last_element_map or []


def run_page_stats(sb):
    """Extract page statistics via JS."""
    try:
        result = sb.execute_script(PAGE_STATS_JS)
        return json.loads(result) if isinstance(result, str) else result
    except Exception as e:
        return {"error": str(e)}


def extract_page_data(sb):
    """Extract page text, images, links from current browser state."""
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
                if len(images) >= MAX_IMAGES:
                    break
    except Exception:
        pass

    links = []
    try:
        for a in sb.select_all("a[href]", timeout=3):
            href = a.get_attribute("href") or ""
            if href and not href.startswith("#") and not href.startswith("javascript:"):
                link_text = (a.text or "").strip()[:100]
                links.append({"text": link_text, "url": href})
                if len(links) >= MAX_LINKS:
                    break
    except Exception:
        pass

    return {"title": title, "url": url, "text": text, "images": images, "links": links}


def extract_interactive(sb):
    """Extract interactive elements from current page."""
    elements = []
    try:
        nodes = sb.select_all(
            'a[href], button, input:not([type="hidden"]), select, textarea, '
            '[role="button"], [role="link"], [role="textbox"], [role="combobox"], '
            '[role="checkbox"], [role="radio"], [role="tab"], [role="menuitem"], '
            '[role="option"], [role="searchbox"]',
            timeout=3
        )
        for el in nodes[:MAX_ELEMENTS]:
            tag = el.tag_name.lower() if el.tag_name else ""
            text = ""
            if tag in ("input", "textarea"):
                text = el.get_attribute("placeholder") or el.get_attribute("name") or el.get_attribute("type") or ""
            elif tag == "select":
                text = el.get_attribute("name") or ""
            else:
                text = (el.text or "").strip()[:60]

            selector = ""
            el_id = el.get_attribute("id")
            if el_id:
                selector = f"#{el_id}"
            else:
                selector = tag

            elements.append({
                "type": "link" if tag == "a" else ("button" if tag == "button" else "input"),
                "tag": tag,
                "text": text,
                "selector": selector,
            })
    except Exception:
        pass
    return elements


def find_element_by_index(state, index):
    """Look up element selector from cached SoM label map."""
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
        if length == 0:
            return {}
        raw = self.rfile.read(length)
        return json.loads(raw)

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
        if data:
            resp.update(data)
        resp.update(extra)
        self._json_response(200, resp)

    def _err(self, msg, code=500):
        self._json_response(code, {"ok": False, "error": str(msg)})

    def _full_page_capture(self, sb, state):
        """Run labeler + screenshot + page stats + page data. Used after every page change."""
        element_map = run_labeler(sb, state)
        stats = run_page_stats(sb)
        page_data = extract_page_data(sb)

        spath = screenshot_path()
        safe(lambda: sb.save_screenshot(spath), None)

        return {
            "page_data": page_data,
            "element_map": element_map,
            "screenshot_path": spath,
            "page_stats": stats,
        }

    # ── GET ─────────────────────────────────────────────────────────────────

    def do_GET(self):
        if self.path == "/status":
            alive = self.state.sb is not None
            url = ""
            if alive:
                url = safe(lambda: self.state.sb.get_current_url() or "", "")
            self._ok({
                "alive": alive,
                "url": url,
                "stealth": self.state.stealth,
                "tabs": safe(lambda: len(self.state.sb.driver.window_handles), 0) if alive else 0,
            })
        else:
            self._err(f"Unknown GET endpoint: {self.path}", 404)

    # ── POST dispatch ───────────────────────────────────────────────────────

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

        # ── Reset ─────────────────────────────────────────────────────────

        if path == "/reset":
            self.state.reset()
            self._ok({"message": "browser reset"})

        # ── Navigate ──────────────────────────────────────────────────────

        elif path == "/navigate":
            url = body.get("url", "")
            if not url:
                return self._err("missing url", 400)
            if not self.state.ensure():
                return self._err("browser not available")
            sb = self.state.sb
            sb.get(url)
            sb.sleep(2)
            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        # ── Read (no navigation) ──────────────────────────────────────────

        elif path == "/read":
            if sb is None:
                return self._err("browser not available")
            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        # ── Search ────────────────────────────────────────────────────────

        elif path == "/search":
            query = body.get("query", "")
            if not query:
                return self._err("missing query", 400)
            if not self.state.ensure():
                return self._err("browser not available")
            sb = self.state.sb
            sb.get(f"https://duckduckgo.com/?q={query.replace(' ', '+')}&iax=images&ia=images")
            sb.sleep(2)
            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        # ── Go Back / Refresh ─────────────────────────────────────────────

        elif path == "/go_back":
            if sb is None:
                return self._err("browser not available")
            sb.go_back()
            sb.sleep(1)
            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        elif path == "/refresh":
            if sb is None:
                return self._err("browser not available")
            sb.refresh()
            sb.sleep(2)
            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        # ── Click ─────────────────────────────────────────────────────────

        elif path == "/click":
            if sb is None:
                return self._err("browser not available")

            selector = body.get("selector", "")
            index = body.get("index", 0)

            if index:
                selector = find_element_by_index(self.state, index)
                if not selector:
                    return self._err(f"element index {index} not found in label map — call /label first")
            if not selector:
                return self._err("missing selector or index", 400)

            try:
                sb.click(selector)
            except Exception as e:
                return self._err(f"click failed: {e}")

            sb.sleep(1)

            # Detect new tabs opened by click
            self._detect_new_tabs(sb)

            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        # ── Type ──────────────────────────────────────────────────────────

        elif path == "/type":
            if sb is None:
                return self._err("browser not available")

            selector = body.get("selector", "")
            index = body.get("index", 0)
            text = body.get("text", "")
            submit = body.get("submit", False)

            if index:
                selector = find_element_by_index(self.state, index)
                if not selector:
                    return self._err(f"element index {index} not found — call /label first")
            if not selector:
                return self._err("missing selector or index", 400)
            if not text:
                return self._err("missing text", 400)

            try:
                sb.type(selector, text)
            except Exception as e:
                return self._err(f"type failed: {e}")

            if submit:
                try:
                    sb.send_keys(selector, "\n")
                except Exception:
                    pass

            sb.sleep(1)
            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        # ── Scroll ────────────────────────────────────────────────────────

        elif path == "/scroll":
            if sb is None:
                return self._err("browser not available")

            direction = body.get("direction", "down")
            amount = body.get("amount", 0)  # pixels, 0 = one viewport

            if amount > 0:
                sb.execute_script(f"window.scrollBy(0, {amount if direction != 'up' else -amount})")
            elif direction == "down":
                sb.scroll_down()
            elif direction == "up":
                sb.scroll_up()
            else:
                sb.scroll_to(direction)

            sb.sleep(0.5)
            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        # ── Label ─────────────────────────────────────────────────────────

        elif path == "/label":
            if sb is None:
                return self._err("browser not available")

            element_map = run_labeler(sb, self.state)
            stats = run_page_stats(sb)
            spath = screenshot_path()
            safe(lambda: sb.save_screenshot(spath), None)

            self._ok({
                "element_map": element_map,
                "screenshot_path": spath,
                "page_stats": stats,
                "url": safe(lambda: sb.get_current_url() or "", ""),
            })

        # ── Interact ──────────────────────────────────────────────────────

        elif path == "/interact":
            if sb is None:
                return self._err("browser not available")

            page_data = extract_page_data(sb)
            elements = extract_interactive(sb)
            stats = run_page_stats(sb)
            spath = screenshot_path()
            safe(lambda: sb.save_screenshot(spath), None)

            self._ok({
                "page_data": page_data,
                "elements": elements,
                "screenshot_path": spath,
                "page_stats": stats,
            })

        # ── Extract Images ────────────────────────────────────────────────

        elif path == "/extract_images":
            if sb is None:
                return self._err("browser not available")

            images = []
            try:
                for img in sb.select_all("img[src]", timeout=5):
                    src = img.get_attribute("src") or ""
                    if src and not src.startswith("data:") and not src.startswith("blob:") and len(src) < 2000:
                        alt = img.get_attribute("alt") or ""
                        images.append({"src": src, "alt": alt[:100]})
                        if len(images) >= MAX_IMAGES:
                            break
            except Exception:
                pass
            self._ok({
                "images": images,
                "url": safe(lambda: sb.get_current_url() or "", ""),
            })

        # ── Screenshot ────────────────────────────────────────────────────

        elif path == "/screenshot":
            path_out = body.get("path", screenshot_path())
            if sb is None:
                return self._err("browser not available")
            sb.save_screenshot(path_out)
            self._ok({
                "screenshot_path": path_out,
                "url": safe(lambda: sb.get_current_url() or "", ""),
            })

        # ── Download ──────────────────────────────────────────────────────

        elif path == "/download":
            url = body.get("url", "")
            out_path = body.get("path", "")
            if not url or not out_path:
                return self._err("missing url or path", 400)
            import urllib.request
            try:
                urllib.request.urlretrieve(url, out_path)
                size = os.path.getsize(out_path)
                self._ok({"url": url, "path": out_path, "size": size})
            except Exception as e:
                self._err(f"download failed: {e}")

        # ── Find Text ─────────────────────────────────────────────────────

        elif path == "/find_text":
            text_query = body.get("text", "")
            if not text_query:
                return self._err("missing text", 400)
            if sb is None:
                return self._err("browser not available")

            try:
                # Scroll the page to find and highlight text
                escaped = text_query.replace('"', '\\"')
                sb.execute_script(f'var found = window.find("{escaped}"); return found;')
                sb.sleep(0.5)
            except Exception as e:
                return self._err(f"find_text failed: {e}")

            capture = self._full_page_capture(sb, self.state)
            self._ok(capture)

        # ── Evaluate JavaScript ───────────────────────────────────────────

        elif path == "/evaluate":
            code = body.get("code", "")
            if not code:
                return self._err("missing code", 400)
            if sb is None:
                return self._err("browser not available")

            try:
                result = sb.execute_script(code)
                self._ok({
                    "result": str(result)[:5000] if result is not None else None,
                    "type": type(result).__name__,
                })
            except Exception as e:
                self._err(f"evaluate failed: {e}")

        # ── Run Python (sb pre-loaded) ────────────────────────────────────

        elif path == "/run":
            code = body.get("code", "")
            if not code:
                return self._err("missing code", 400)
            if sb is None:
                return self._err("browser not available")

            namespace = {"sb": sb, "json": json, "os": os, "time": time}
            try:
                exec(code, namespace)
            except Exception as e:
                return self._err(f"exec error: {e}")

            if "result" in namespace and isinstance(namespace["result"], dict):
                self._ok(namespace["result"])
            else:
                capture = self._full_page_capture(sb, self.state)
                self._ok(capture)

        # ── Tabs ──────────────────────────────────────────────────────────

        elif path == "/tabs":
            if sb is None:
                return self._err("browser not available")

            tabs = []
            try:
                handles = sb.driver.window_handles
                current = sb.driver.current_window_handle
                for i, h in enumerate(handles):
                    sb.driver.switch_to.window(h)
                    tabs.append({
                        "index": i,
                        "url": safe(lambda: sb.get_current_url() or "", ""),
                        "title": safe(lambda: sb.get_title() or "", ""),
                        "active": h == current,
                    })
                sb.driver.switch_to.window(current)
            except Exception as e:
                return self._err(f"tab listing failed: {e}")
            self._ok({"tabs": tabs})

        elif path == "/new_tab":
            url = body.get("url", "about:blank")
            if sb is None:
                return self._err("browser not available")
            try:
                sb.driver.execute_script(f"window.open('{url}', '_blank');")
                handles = sb.driver.window_handles
                sb.driver.switch_to.window(handles[-1])
                sb.sleep(2)
                capture = self._full_page_capture(sb, self.state)
                self._ok(capture)
            except Exception as e:
                self._err(f"new tab failed: {e}")

        elif path == "/switch_tab":
            index = body.get("index", 0)
            if sb is None:
                return self._err("browser not available")
            try:
                handles = sb.driver.window_handles
                if 0 <= index < len(handles):
                    sb.driver.switch_to.window(handles[index])
                    sb.sleep(1)
                    capture = self._full_page_capture(sb, self.state)
                    self._ok(capture)
                else:
                    self._err(f"tab index {index} out of range (0-{len(handles)-1})")
            except Exception as e:
                self._err(f"switch tab failed: {e}")

        elif path == "/close_tab":
            if sb is None:
                return self._err("browser not available")
            try:
                handles = sb.driver.window_handles
                if len(handles) <= 1:
                    return self._err("can't close last tab")
                sb.driver.close()
                handles = sb.driver.window_handles
                sb.driver.switch_to.window(handles[-1])
                sb.sleep(1)
                capture = self._full_page_capture(sb, self.state)
                self._ok(capture)
            except Exception as e:
                self._err(f"close tab failed: {e}")

        # ── Check recent CDP downloads ────────────────────────────────────

        elif path == "/check_downloads":
            files = []
            try:
                ddir = self.state._download_dir
                for f in sorted(Path(ddir).iterdir(), key=lambda p: p.stat().st_mtime, reverse=True):
                    if f.is_file() and not f.name.endswith(".crdownload"):
                        files.append({
                            "filename": f.name,
                            "path": str(f),
                            "size": f.stat().st_size,
                            "modified": f.stat().st_mtime,
                        })
                self._ok({"downloads": files[:20], "download_dir": ddir})
            except Exception as e:
                self._err(f"check_downloads failed: {e}")

        else:
            self._err(f"Unknown endpoint: {path}", 404)

    def _detect_new_tabs(self, sb):
        """Detect if a click opened new tabs and don't auto-switch — just log."""
        try:
            handles = sb.driver.window_handles
            if len(handles) > 1:
                log(f"detected {len(handles)} open tabs after click")
        except Exception:
            pass


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
