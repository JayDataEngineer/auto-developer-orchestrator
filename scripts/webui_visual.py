#!/usr/bin/env python3
"""
WebUI Visual Testing Server — mirrors tui_visual.py for the web frontend.

Uses Playwright (sync API) to control Chromium, captures screenshots,
and serves them via HTTP so AI agents can see the WebUI.

Endpoints (same as tui_visual.py):
    GET  /screenshot  → PNG image of current page
    GET  /screen      → JSON with visible text content
    GET  /logs        → last N console messages
    GET  /observe     → combined: screenshot (base64) + text + logs + status
    POST /input       → type text into focused element
    POST /key         → press a key (body: {"key": "Enter"})
    POST /click       → click at coordinates or selector
    POST /goto        → navigate to URL
    GET  /health      → server status

Usage:
    uv pip install playwright Pillow
    playwright install chromium
    python scripts/webui_visual.py [--port 9878] [--url http://localhost:5174]
"""

import argparse
import base64
import collections
import io
import json
import os
import sys
import time
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from urllib.parse import parse_qs, urlparse

try:
    from playwright.sync_api import sync_playwright
    from PIL import Image
except ImportError:
    print("Install dependencies first:")
    print("  uv pip install playwright Pillow")
    print("  playwright install chromium")
    sys.exit(1)


# ── Console Log Capture ────────────────────────────────────────────

class ConsoleCapture:
    """Captures browser console messages."""

    def __init__(self, max_lines=500):
        self.max_lines = max_lines
        self.messages = collections.deque(maxlen=max_lines)
        self.lock = threading.Lock()

    def add(self, msg_type, text):
        with self.lock:
            self.messages.append(f"[{msg_type}] {text}")

    def get_lines(self, n=None):
        with self.lock:
            lines = list(self.messages)
            if n:
                return lines[-n:]
            return lines

    def clear(self):
        with self.lock:
            self.messages.clear()


# ── Browser Manager ────────────────────────────────────────────────

class BrowserManager:
    """Manages a Playwright browser instance."""

    def __init__(self, base_url="http://localhost:5174", viewport=None):
        self.base_url = base_url
        self.viewport = viewport or {"width": 1280, "height": 720}
        self.console = ConsoleCapture()
        self._pw = None
        self._browser = None
        self._context = None
        self.page = None

    def start(self):
        self._pw = sync_playwright().start()
        self._browser = self._pw.chromium.launch(headless=True)
        self._context = self._browser.new_context(
            viewport=self.viewport,
            device_scale_factor=1,
        )
        self.page = self._context.new_page()

        # Capture console messages
        self.page.on("console", lambda msg: self.console.add(msg.type, msg.text))
        self.page.on("pageerror", lambda err: self.console.add("error", str(err)))

        # Navigate to base URL
        self.page.goto(self.base_url, wait_until="networkidle", timeout=30000)
        self.console.add("info", f"Navigated to {self.base_url}")

    def stop(self):
        if self._context:
            self._context.close()
        if self._browser:
            self._browser.close()
        if self._pw:
            self._pw.stop()

    def screenshot_png(self) -> bytes:
        return self.page.screenshot(type="png", full_page=False)

    def screenshot_b64(self) -> str:
        return base64.b64encode(self.screenshot_png()).decode("ascii")

    def get_text(self) -> str:
        """Extract visible text content from the page."""
        return self.page.evaluate("""() => {
            const body = document.body;
            if (!body) return '';
            const walker = document.createTreeWalker(body, NodeFilter.SHOW_TEXT, {
                acceptNode: (node) => {
                    // Skip script, style, and hidden elements
                    const parent = node.parentElement;
                    if (!parent) return NodeFilter.FILTER_REJECT;
                    const tag = parent.tagName.toLowerCase();
                    if (['script', 'style', 'noscript'].includes(tag)) return NodeFilter.FILTER_REJECT;
                    const style = window.getComputedStyle(parent);
                    if (style.display === 'none' || style.visibility === 'hidden') return NodeFilter.FILTER_REJECT;
                    return NodeFilter.FILTER_ACCEPT;
                }
            });
            const texts = [];
            while (walker.nextNode()) {
                const t = walker.currentNode.textContent.trim();
                if (t) texts.push(t);
            }
            return texts.join('\\n');
        }""")

    def get_state(self) -> dict:
        """Get current page state: URL, title, viewport."""
        return {
            "url": self.page.url,
            "title": self.page.title(),
            "viewport": self.page.viewport_size,
        }


# ── HTTP Server ────────────────────────────────────────────────────

class WebUIVisualHandler(BaseHTTPRequestHandler):
    browser: BrowserManager = None

    def do_GET(self):
        if self.path == "/screenshot":
            self._serve_screenshot()
        elif self.path == "/screen":
            self._serve_screen()
        elif self.path == "/logs":
            self._serve_logs()
        elif self.path.startswith("/observe"):
            self._serve_observe()
        elif self.path == "/health":
            self._serve_health()
        else:
            self.send_error(404)

    def do_POST(self):
        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length) if content_length > 0 else b"{}"

        if self.path == "/input":
            self._handle_input(body)
        elif self.path == "/key":
            self._handle_key(body)
        elif self.path == "/click":
            self._handle_click(body)
        elif self.path == "/goto":
            self._handle_goto(body)
        else:
            self.send_error(404)

    def _serve_screenshot(self):
        png_data = self.browser.screenshot_png()
        self.send_response(200)
        self.send_header("Content-Type", "image/png")
        self.send_header("Content-Length", str(len(png_data)))
        self.end_headers()
        self.wfile.write(png_data)

    def _serve_screen(self):
        text = self.browser.get_text()
        state = self.browser.get_state()
        data = json.dumps({
            "screen": text,
            "url": state["url"],
            "title": state["title"],
            "viewport": state["viewport"],
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def _serve_logs(self):
        n = 50
        if "?" in self.path:
            qs = parse_qs(self.path.split("?", 1)[1])
            n = int(qs.get("n", ["50"])[0])
        logs = self.browser.console.get_lines(n)
        data = json.dumps({"logs": logs}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def _serve_observe(self):
        """Combined endpoint: screenshot (base64) + text + logs + state.

        Same response format as tui_visual.py /observe for DRY AI consumption.
        """
        wait = 0
        if "?" in self.path:
            qs = parse_qs(self.path.split("?", 1)[1])
            wait = float(qs.get("wait", ["0"])[0])
        if wait > 0:
            time.sleep(min(wait, 10))

        screenshot_b64 = self.browser.screenshot_b64()
        screen_text = self.browser.get_text()
        logs = self.browser.console.get_lines(30)
        state = self.browser.get_state()

        data = json.dumps({
            "screenshot_base64": screenshot_b64,
            "screen": screen_text,
            "logs": logs,
            "url": state["url"],
            "title": state["title"],
            "viewport": state["viewport"],
            "timestamp": time.time(),
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def _handle_input(self, body):
        try:
            req = json.loads(body)
            text = req.get("text", "")
            selector = req.get("selector", "body")
            if text:
                el = self.browser.page.locator(selector).first
                el.type(text, delay=50)
                wait = float(req.get("wait", 0.5))
                time.sleep(min(wait, 5))
            self._json_ok({"ok": True})
        except Exception as e:
            self.send_error(400, str(e))

    def _handle_key(self, body):
        try:
            req = json.loads(body)
            key = req.get("key", "Enter")
            self.browser.page.keyboard.press(key)
            wait = float(req.get("wait", 0.5))
            time.sleep(min(wait, 5))
            self._json_ok({"ok": True, "key": key})
        except Exception as e:
            self.send_error(400, str(e))

    def _handle_click(self, body):
        try:
            req = json.loads(body)
            selector = req.get("selector")
            x = req.get("x")
            y = req.get("y")
            if selector:
                self.browser.page.click(selector)
            elif x is not None and y is not None:
                self.browser.page.mouse.click(float(x), float(y))
            else:
                self.send_error(400, "Provide 'selector' or 'x'/'y'")
                return
            wait = float(req.get("wait", 0.5))
            time.sleep(min(wait, 5))
            self._json_ok({"ok": True})
        except Exception as e:
            self.send_error(400, str(e))

    def _handle_goto(self, body):
        try:
            req = json.loads(body)
            url = req.get("url", self.browser.base_url)
            self.browser.page.goto(url, wait_until="networkidle", timeout=30000)
            wait = float(req.get("wait", 0.5))
            time.sleep(min(wait, 5))
            state = self.browser.get_state()
            self._json_ok({"ok": True, **state})
        except Exception as e:
            self.send_error(400, str(e))

    def _serve_health(self):
        state = self.browser.get_state()
        data = json.dumps({
            "running": True,
            **state,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def _json_ok(self, obj):
        data = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, format, *args):
        pass


# ── Main ───────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="WebUI Visual Testing Server")
    parser.add_argument("--port", type=int, default=9878)
    parser.add_argument("--url", type=str, default="http://localhost:5174",
                        help="Base URL of the Vite frontend")
    parser.add_argument("--width", type=int, default=1280)
    parser.add_argument("--height", type=int, default=720)
    args = parser.parse_args()

    browser = BrowserManager(
        base_url=args.url,
        viewport={"width": args.width, "height": args.height},
    )

    print(f"Starting Chromium → {args.url}")
    browser.start()

    WebUIVisualHandler.browser = browser

    server = HTTPServer(("0.0.0.0", args.port), WebUIVisualHandler)
    print(f"\nWebUI Visual Testing Server on :{args.port}")
    print(f"  GET  /observe       → combined screenshot + text + logs (AI endpoint)")
    print(f"  GET  /screenshot    → PNG of current page")
    print(f"  GET  /screen        → visible text content")
    print(f"  GET  /logs?n=50     → last N console messages")
    print(f"  POST /input         → type text (body: {{\"text\": \"hello\", \"selector\": \"input\"}})")
    print(f"  POST /key           → press key (body: {{\"key\": \"Enter\"}})")
    print(f"  POST /click         → click (body: {{\"selector\": \"button\"}} or {{\"x\": 100, \"y\": 200}})")
    print(f"  POST /goto          → navigate (body: {{\"url\": \"http://...\"}})")
    print(f"  GET  /health        → server status")
    print(f"\nViewport: {args.width}x{args.height}")
    print()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down...")
        browser.stop()
        server.server_close()


if __name__ == "__main__":
    main()
