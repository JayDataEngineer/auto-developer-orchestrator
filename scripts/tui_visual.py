#!/usr/bin/env python3
"""
TUI Visual Testing Server — autonomous visual feedback loop.

Runs a terminal UI in a pseudo-terminal, captures the screen buffer,
renders it as a PNG, and serves it via HTTP so AI agents can see it.

Usage:
    source /tmp/tui-venv/bin/activate
    python scripts/tui_visual.py [--port 9877] [--cols 120] [--rows 40]

Endpoints:
    GET  /screenshot  → PNG image of current TUI state
    GET  /screen      → JSON with terminal buffer as text
    GET  /logs        → last N lines of stderr/log output
    GET  /observe     → combined: screenshot (base64) + screen text + logs + status
    POST /input       → send keystrokes (body: {"text": "hello\n"})
    POST /key         → send special keys (body: {"key": "escape"})
    POST /restart     → restart the TUI process
    GET  /health      → server status
"""

import argparse
import collections
import fcntl
import io
import json
import os
import pty
import select
import signal
import struct
import sys
import termios
import time
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path

try:
    import pyte
    from PIL import Image, ImageDraw, ImageFont
except ImportError:
    print("Install dependencies first:")
    print("  uv venv /tmp/tui-venv && source /tmp/tui-venv/bin/activate")
    print("  uv pip install pyte Pillow")
    sys.exit(1)


# ── Special key sequences ──────────────────────────────────────────

SPECIAL_KEYS = {
    "escape":      "\x1b",
    "enter":       "\r",
    "tab":         "\t",
    "backspace":   "\x7f",
    "delete":      "\x1b[3~",
    "up":          "\x1b[A",
    "down":        "\x1b[B",
    "right":       "\x1b[C",
    "left":        "\x1b[D",
    "home":        "\x1b[H",
    "end":         "\x1b[F",
    "pageup":      "\x1b[5~",
    "pagedown":    "\x1b[6~",
    "ctrl+c":      "\x03",
    "ctrl+d":      "\x04",
    "ctrl+z":      "\x1a",
    "ctrl+l":      "\x0c",
    "ctrl+k":      "\x0b",
    "ctrl+p":      "\x10",
    "ctrl+o":      "\x0f",
    "ctrl+t":      "\x14",
    "ctrl+g":      "\x07",
    "ctrl+u":      "\x15",
    "ctrl+w":      "\x17",
    "ctrl+a":      "\x01",
    "ctrl+e":      "\x05",
    "ctrl+n":      "\x0e",
    "shift+tab":   "\x1b[Z",
    "shift+ctrl+p": "\x1b[20;5~",
    "alt+enter":   "\x1b\r",
    "alt+up":      "\x1b\x1b[A",
}


# ── Ring Buffer for Logs ───────────────────────────────────────────

class RingBuffer:
    """Thread-safe ring buffer for log capture."""

    def __init__(self, max_lines=500):
        self.max_lines = max_lines
        self.buffer = collections.deque(maxlen=max_lines)
        self.lock = threading.Lock()

    def append(self, line: str):
        with self.lock:
            self.buffer.append(line)

    def get_lines(self, n=None):
        with self.lock:
            lines = list(self.buffer)
            if n:
                return lines[-n:]
            return lines

    def clear(self):
        with self.lock:
            self.buffer.clear()


# ── Terminal Renderer ─────────────────────────────────────────────

class TerminalRenderer:
    """Emulates a terminal and renders the screen buffer as a PNG."""

    def __init__(self, cols=120, rows=40, font_path=None, font_size=14):
        self.cols = cols
        self.rows = rows
        self.screen = pyte.Screen(cols, rows)
        self.stream = pyte.Stream(self.screen)

        if font_path and os.path.exists(font_path):
            self.font = ImageFont.truetype(font_path, font_size)
        else:
            for candidate in [
                "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
                "/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
                "/usr/share/fonts/truetype/msttcorefonts/cour.ttf",
            ]:
                if os.path.exists(candidate):
                    self.font = ImageFont.truetype(candidate, font_size)
                    break
            else:
                self.font = ImageFont.load_default()

        bbox = self.font.getbbox("M")
        self.cell_w = bbox[2] - bbox[0] + 2
        self.cell_h = bbox[3] - bbox[1] + 4
        self.lock = threading.Lock()

    def feed(self, data: bytes):
        with self.lock:
            try:
                self.stream.feed(data.decode("utf-8", errors="replace"))
            except Exception:
                pass

    def render(self) -> Image.Image:
        with self.lock:
            img_w = self.cols * self.cell_w
            img_h = self.rows * self.cell_h
            img = Image.new("RGB", (img_w, img_h), color=(30, 30, 30))
            draw = ImageDraw.Draw(img)

            for y, line in enumerate(self.screen.display):
                for x, char in enumerate(line):
                    if char == " " or char == "":
                        continue
                    pyte_char = self.screen.buffer[y][x]
                    fg = self._pyte_color(pyte_char.fg, (212, 212, 212))
                    bg = self._pyte_color(pyte_char.bg, None)

                    px = x * self.cell_w
                    py = y * self.cell_h

                    if bg:
                        draw.rectangle(
                            [px, py, px + self.cell_w, py + self.cell_h],
                            fill=bg,
                        )
                    if char.strip():
                        draw.text((px + 1, py + 1), char, fill=fg, font=self.font)

            return img

    def render_bytes(self, fmt="PNG") -> bytes:
        buf = io.BytesIO()
        self.render().save(buf, format=fmt)
        return buf.getvalue()

    def render_base64(self) -> str:
        import base64
        return base64.b64encode(self.render_bytes()).decode("ascii")

    def get_text(self) -> str:
        with self.lock:
            lines = []
            for line in self.screen.display:
                lines.append(line.rstrip())
            while lines and not lines[-1]:
                lines.pop()
            return "\n".join(lines)

    def resize(self, cols, rows):
        with self.lock:
            self.cols = cols
            self.rows = rows
            self.screen.resize(rows, cols)

    @staticmethod
    def _pyte_color(color, default):
        if color is None or color == "default":
            return default
        if isinstance(color, str):
            # Named ANSI colors
            named = {
                "black": (0, 0, 0), "red": (205, 49, 49),
                "green": (13, 188, 121), "yellow": (229, 229, 16),
                "blue": (36, 114, 200), "magenta": (188, 63, 188),
                "cyan": (17, 168, 205), "white": (229, 229, 229),
            }
            if color in named:
                return named[color]
            # Hex color from pyte (e.g. "ff0000" for 256-color or truecolor)
            if len(color) == 6:
                try:
                    r = int(color[0:2], 16)
                    g = int(color[2:4], 16)
                    b = int(color[4:6], 16)
                    return (r, g, b)
                except ValueError:
                    pass
        return default


# ── TUI Process Manager ───────────────────────────────────────────

class TUIProcess:
    """Manages a TUI process running in a pseudo-terminal."""

    def __init__(self, cmd, cwd, renderer: TerminalRenderer, log_buffer: RingBuffer, env=None):
        self.cmd = cmd
        self.cwd = cwd
        self.renderer = renderer
        self.log_buffer = log_buffer
        self.env = env or os.environ.copy()
        self.master_fd = None
        self.pid = None
        self.running = False
        self.exit_code = None
        self.raw_output = RingBuffer(200)  # last 200 chunks of raw output

    def start(self):
        self.stop()

        pid, master_fd = pty.fork()
        if pid == 0:
            os.chdir(self.cwd)
            os.execvp(self.cmd[0], self.cmd)
        else:
            self.pid = pid
            self.master_fd = master_fd
            self.running = True
            self.exit_code = None

            winsize = struct.pack("HHHH", self.renderer.rows, self.renderer.cols, 0, 0)
            fcntl.ioctl(master_fd, termios.TIOCSWINSZ, winsize)

            flags = fcntl.fcntl(master_fd, fcntl.F_GETFL)
            fcntl.fcntl(master_fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)

            self._reader_thread = threading.Thread(target=self._read_loop, daemon=True)
            self._reader_thread.start()

            self.log_buffer.append(f"[tui-visual] started pid={pid} cmd={' '.join(self.cmd)}")

    def stop(self):
        if self.pid:
            try:
                os.kill(self.pid, signal.SIGTERM)
                for _ in range(10):
                    pid, status = os.waitpid(self.pid, os.WNOHANG)
                    if pid != 0:
                        self.exit_code = os.WEXITSTATUS(status) if os.WIFEXITED(status) else -1
                        break
                    time.sleep(0.1)
                else:
                    os.kill(self.pid, signal.SIGKILL)
                    os.waitpid(self.pid, 0)
                    self.exit_code = -9
            except (ProcessLookupError, ChildProcessError):
                pass
            self.pid = None
        if self.master_fd:
            try:
                os.close(self.master_fd)
            except OSError:
                pass
            self.master_fd = None
        self.running = False

    def write(self, data: str):
        if self.master_fd is not None:
            try:
                os.write(self.master_fd, data.encode())
            except OSError:
                pass

    def _read_loop(self):
        while self.running and self.master_fd is not None:
            try:
                r, _, _ = select.select([self.master_fd], [], [], 0.1)
                if r:
                    data = os.read(self.master_fd, 65536)
                    if data:
                        self.renderer.feed(data)
                        # Capture raw output for logs (strip ANSI for readability)
                        text = data.decode("utf-8", errors="replace")
                        self.raw_output.append(text)
                    else:
                        self.running = False
                        self.log_buffer.append("[tui-visual] process exited (EOF)")
                        break
            except (OSError, ValueError):
                self.running = False
                self.log_buffer.append("[tui-visual] process exited (error)")
                break

    def restart(self):
        self.log_buffer.append("[tui-visual] restarting...")
        self.raw_output.clear()
        self.start()


# ── HTTP Server ────────────────────────────────────────────────────

class TUIVisualHandler(BaseHTTPRequestHandler):
    tui: TUIProcess = None
    renderer: TerminalRenderer = None
    log_buffer: RingBuffer = None

    def do_GET(self):
        if self.path == "/screenshot":
            self._serve_screenshot()
        elif self.path == "/screen":
            self._serve_screen()
        elif self.path == "/logs":
            self._serve_logs()
        elif self.path == "/observe":
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
        elif self.path == "/restart":
            self._handle_restart()
        else:
            self.send_error(404)

    def _serve_screenshot(self):
        png_data = self.renderer.render_bytes()
        self.send_response(200)
        self.send_header("Content-Type", "image/png")
        self.send_header("Content-Length", str(len(png_data)))
        self.end_headers()
        self.wfile.write(png_data)

    def _serve_screen(self):
        text = self.renderer.get_text()
        data = json.dumps({
            "screen": text,
            "cols": self.renderer.cols,
            "rows": self.renderer.rows,
            "running": self.tui.running if self.tui else False,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def _serve_logs(self):
        n = 50
        if "?" in self.path:
            from urllib.parse import parse_qs
            qs = parse_qs(self.path.split("?", 1)[1])
            n = int(qs.get("n", ["50"])[0])
        logs = self.log_buffer.get_lines(n)
        raw = self.tui.raw_output.get_lines(n) if self.tui else []
        data = json.dumps({
            "logs": logs,
            "raw_output_last": raw[-5:] if raw else [],
            "running": self.tui.running if self.tui else False,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def _serve_observe(self):
        """Combined endpoint: screenshot (base64) + screen text + logs + status.

        This is the endpoint an AI agent calls after sending input.
        Returns everything needed to understand the current state.
        """
        # Optional wait parameter — wait N seconds before capturing
        wait = 0
        if "?" in self.path:
            from urllib.parse import parse_qs
            qs = parse_qs(self.path.split("?", 1)[1])
            wait = float(qs.get("wait", ["0"])[0])
        if wait > 0:
            time.sleep(min(wait, 10))  # cap at 10s

        screenshot_b64 = self.renderer.render_base64()
        screen_text = self.renderer.get_text()
        logs = self.log_buffer.get_lines(30)
        raw = self.tui.raw_output.get_lines(10) if self.tui else []

        data = json.dumps({
            "screenshot_base64": screenshot_b64,
            "screen": screen_text,
            "logs": logs,
            "raw_output_last": raw,
            "running": self.tui.running if self.tui else False,
            "exit_code": self.tui.exit_code,
            "pid": self.tui.pid,
            "cols": self.renderer.cols,
            "rows": self.renderer.rows,
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
            if text:
                self.tui.write(text)
                self.log_buffer.append(f"[input] {repr(text)}")
                wait = float(req.get("wait", 0.3))
                time.sleep(min(wait, 5))
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"ok": True}).encode())
        except Exception as e:
            self.send_error(400, str(e))

    def _handle_key(self, body):
        """Send a special key by name."""
        try:
            req = json.loads(body)
            key = req.get("key", "").lower()
            if key in SPECIAL_KEYS:
                self.tui.write(SPECIAL_KEYS[key])
                self.log_buffer.append(f"[key] {key}")
                wait = float(req.get("wait", 0.3))
                time.sleep(min(wait, 5))
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps({"ok": True, "key": key}).encode())
            else:
                available = ", ".join(sorted(SPECIAL_KEYS.keys()))
                self.send_error(400, f"Unknown key '{key}'. Available: {available}")
        except Exception as e:
            self.send_error(400, str(e))

    def _handle_restart(self):
        self.tui.restart()
        time.sleep(1)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"ok": True, "running": self.tui.running}).encode())

    def _serve_health(self):
        data = json.dumps({
            "running": self.tui.running if self.tui else False,
            "exit_code": self.tui.exit_code if self.tui else None,
            "pid": self.tui.pid if self.tui else None,
            "cols": self.renderer.cols,
            "rows": self.renderer.rows,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, format, *args):
        pass


# ── Main ───────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="TUI Visual Testing Server")
    parser.add_argument("--port", type=int, default=9877)
    parser.add_argument("--cols", type=int, default=120)
    parser.add_argument("--rows", type=int, default=40)
    parser.add_argument("--font-size", type=int, default=14)
    parser.add_argument("--server", type=str, default="http://localhost:3847")
    parser.add_argument("--project", type=str, default="ts-tui-pi")
    args = parser.parse_args()

    tui_dir = Path(__file__).parent.parent / "ts-tui-pi"
    if not tui_dir.exists():
        print(f"TUI directory not found: {tui_dir}")
        sys.exit(1)

    renderer = TerminalRenderer(cols=args.cols, rows=args.rows, font_size=args.font_size)
    log_buffer = RingBuffer(500)
    cmd = ["bun", "run", "src/main.ts", "--project", args.project, "--server", args.server]
    tui = TUIProcess(cmd, str(tui_dir), renderer, log_buffer)

    TUIVisualHandler.tui = tui
    TUIVisualHandler.renderer = renderer
    TUIVisualHandler.log_buffer = log_buffer

    print(f"Starting TUI: {' '.join(cmd)}")
    tui.start()
    time.sleep(1)

    server = HTTPServer(("0.0.0.0", args.port), TUIVisualHandler)
    print(f"\nTUI Visual Testing Server on :{args.port}")
    print(f"  GET  /observe       → combined screenshot + screen + logs (AI agent endpoint)")
    print(f"  GET  /screenshot    → PNG of current TUI state")
    print(f"  GET  /screen        → terminal buffer as text")
    print(f"  GET  /logs?n=50     → last N log lines + raw output")
    print(f"  POST /input         → type text (body: {{\"text\": \"hello\\n\", \"wait\": 0.5}})")
    print(f"  POST /key           → special key (body: {{\"key\": \"escape\", \"wait\": 0.3}})")
    print(f"  POST /restart       → restart TUI process")
    print(f"  GET  /health        → server status")
    print(f"\nSpecial keys: {', '.join(sorted(SPECIAL_KEYS.keys()))}")
    print(f"\nProcess: {tui.running} (pid={tui.pid}), terminal: {args.cols}x{args.rows}")
    print()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down...")
        tui.stop()
        server.server_close()


if __name__ == "__main__":
    main()
