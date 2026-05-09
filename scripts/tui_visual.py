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
    POST /input       → send keystrokes (body: {"text": "hello\n"})
    POST /restart     → restart the TUI process
    GET  /health      → server status
"""

import argparse
import base64
import fcntl
import json
import os
import pty
import select
import signal
import struct
import subprocess
import sys
import termios
import time
import tty
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from threading import Lock

# Lazy imports — installed in /tmp/tui-venv
try:
    import pyte
    from PIL import Image, ImageDraw, ImageFont
except ImportError:
    print("Install dependencies first:")
    print("  uv venv /tmp/tui-venv && source /tmp/tui-venv/bin/activate")
    print("  uv pip install pyte Pillow")
    sys.exit(1)


# ── Terminal Renderer ─────────────────────────────────────────────

class TerminalRenderer:
    """Emulates a terminal and renders the screen buffer as a PNG."""

    def __init__(self, cols=120, rows=40, font_path=None, font_size=14):
        self.cols = cols
        self.rows = rows
        self.screen = pyte.Screen(cols, rows)
        self.stream = pyte.Stream(self.screen)

        # Font setup
        if font_path and os.path.exists(font_path):
            self.font = ImageFont.truetype(font_path, font_size)
        else:
            # Try common monospace fonts
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

        # Measure character cell size
        bbox = self.font.getbbox("M")
        self.cell_w = bbox[2] - bbox[0] + 2  # +2 for spacing
        self.cell_h = bbox[3] - bbox[1] + 4   # +4 for line spacing

        # Lock for thread safety
        self.lock = Lock()

    def feed(self, data: bytes):
        """Feed raw terminal output through the emulator."""
        with self.lock:
            try:
                self.stream.feed(data.decode("utf-8", errors="replace"))
            except Exception:
                pass

    def render(self) -> Image.Image:
        """Render the current screen buffer as a PIL Image."""
        with self.lock:
            img_w = self.cols * self.cell_w
            img_h = self.rows * self.cell_h
            img = Image.new("RGB", (img_w, img_h), color=(30, 30, 30))
            draw = ImageDraw.Draw(img)

            for y, line in enumerate(self.screen.display):
                for x, char in enumerate(line):
                    if char == " " or char == "":
                        continue
                    # Get color from pyte Char
                    pyte_char = self.screen.buffer[y][x]
                    fg = self._pyte_color(pyte_char.fg, (212, 212, 212))
                    bg = self._pyte_color(pyte_char.bg, None)

                    px = x * self.cell_w
                    py = y * self.cell_h

                    # Draw background if set
                    if bg:
                        draw.rectangle(
                            [px, py, px + self.cell_w, py + self.cell_h],
                            fill=bg,
                        )

                    # Draw character
                    if char.strip():
                        draw.text((px + 1, py + 1), char, fill=fg, font=self.font)

            return img

    def render_bytes(self, fmt="PNG") -> bytes:
        """Render and return as bytes."""
        import io
        buf = io.BytesIO()
        self.render().save(buf, format=fmt)
        return buf.getvalue()

    def get_text(self) -> str:
        """Return the screen buffer as plain text."""
        with self.lock:
            lines = []
            for line in self.screen.display:
                lines.append(line.rstrip())
            # Trim trailing empty lines
            while lines and not lines[-1]:
                lines.pop()
            return "\n".join(lines)

    def resize(self, cols, rows):
        """Resize the terminal."""
        with self.lock:
            self.cols = cols
            self.rows = rows
            self.screen.resize(rows, cols)

    @staticmethod
    def _pyte_color(color, default):
        """Convert pyte color to RGB tuple."""
        if color is None or color == "default":
            return default
        if isinstance(color, str):
            named = {
                "black": (0, 0, 0), "red": (205, 49, 49),
                "green": (13, 188, 121), "yellow": (229, 229, 16),
                "blue": (36, 114, 200), "magenta": (188, 63, 188),
                "cyan": (17, 168, 205), "white": (229, 229, 229),
            }
            return named.get(color, default)
        if hasattr(color, "css"):
            # pyte.colors.Color has a css property like "rgb(1,2,3)"
            return default
        return default


# ── TUI Process Manager ───────────────────────────────────────────

class TUIProcess:
    """Manages a TUI process running in a pseudo-terminal."""

    def __init__(self, cmd, cwd, renderer: TerminalRenderer, env=None):
        self.cmd = cmd
        self.cwd = cwd
        self.renderer = renderer
        self.env = env or os.environ.copy()
        self.master_fd = None
        self.pid = None
        self.running = False

    def start(self):
        """Start the TUI process in a pty."""
        self.stop()

        pid, master_fd = pty.fork()
        if pid == 0:
            # Child process
            os.chdir(self.cwd)
            os.execvp(self.cmd[0], self.cmd)
        else:
            # Parent process
            self.pid = pid
            self.master_fd = master_fd
            self.running = True

            # Set terminal size
            winsize = struct.pack("HHHH", self.renderer.rows, self.renderer.cols, 0, 0)
            fcntl.ioctl(master_fd, termios.TIOCSWINSZ, winsize)

            # Set non-blocking
            flags = fcntl.fcntl(master_fd, fcntl.F_GETFL)
            fcntl.fcntl(master_fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)

            # Start reader thread
            import threading
            self._reader_thread = threading.Thread(target=self._read_loop, daemon=True)
            self._reader_thread.start()

    def stop(self):
        """Stop the TUI process."""
        if self.pid:
            try:
                os.kill(self.pid, signal.SIGTERM)
                os.waitpid(self.pid, os.WNOHANG)
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
        """Send input to the TUI process."""
        if self.master_fd is not None:
            try:
                os.write(self.master_fd, data.encode())
            except OSError:
                pass

    def _read_loop(self):
        """Read from the pty and feed to the terminal emulator."""
        while self.running and self.master_fd is not None:
            try:
                r, _, _ = select.select([self.master_fd], [], [], 0.1)
                if r:
                    data = os.read(self.master_fd, 65536)
                    if data:
                        self.renderer.feed(data)
                    else:
                        self.running = False
                        break
            except (OSError, ValueError):
                self.running = False
                break

    def restart(self):
        """Restart the TUI process."""
        self.start()


# ── HTTP Server ────────────────────────────────────────────────────

class TUIVisualHandler(BaseHTTPRequestHandler):
    """HTTP handler for the visual testing server."""

    tui: TUIProcess = None
    renderer: TerminalRenderer = None
    last_screenshot: bytes = b""
    last_screenshot_time: float = 0

    def do_GET(self):
        if self.path == "/screenshot":
            self._serve_screenshot()
        elif self.path == "/screen":
            self._serve_screen()
        elif self.path == "/health":
            self._serve_health()
        else:
            self.send_error(404)

    def do_POST(self):
        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length) if content_length > 0 else b"{}"

        if self.path == "/input":
            self._handle_input(body)
        elif self.path == "/restart":
            self._handle_restart()
        else:
            self.send_error(404)

    def _serve_screenshot(self):
        """Return the current TUI state as a PNG."""
        png_data = self.renderer.render_bytes()
        self.last_screenshot = png_data
        self.last_screenshot_time = time.time()
        self.send_response(200)
        self.send_header("Content-Type", "image/png")
        self.send_header("Content-Length", str(len(png_data)))
        self.end_headers()
        self.wfile.write(png_data)

    def _serve_screen(self):
        """Return the terminal buffer as text."""
        text = self.renderer.get_text()
        data = json.dumps({
            "screen": text,
            "cols": self.renderer.cols,
            "rows": self.renderer.rows,
            "process_running": self.tui.running if self.tui else False,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def _handle_input(self, body):
        """Send keystrokes to the TUI."""
        try:
            req = json.loads(body)
            text = req.get("text", "")
            if text:
                self.tui.write(text)
                time.sleep(0.1)  # Let the TUI process the input
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"ok": True}).encode())
        except Exception as e:
            self.send_error(400, str(e))

    def _handle_restart(self):
        """Restart the TUI process."""
        self.tui.restart()
        time.sleep(1)  # Let it initialize
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"ok": True, "running": self.tui.running}).encode())

    def _serve_health(self):
        """Health check."""
        data = json.dumps({
            "running": self.tui.running if self.tui else False,
            "pid": self.tui.pid,
            "cols": self.renderer.cols,
            "rows": self.renderer.rows,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, format, *args):
        """Suppress default request logging."""
        pass


# ── Main ───────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="TUI Visual Testing Server")
    parser.add_argument("--port", type=int, default=9877, help="HTTP port (default: 9877)")
    parser.add_argument("--cols", type=int, default=120, help="Terminal columns (default: 120)")
    parser.add_argument("--rows", type=int, default=40, help="Terminal rows (default: 40)")
    parser.add_argument("--font-size", type=int, default=14, help="Font size (default: 14)")
    parser.add_argument("--server", type=str, default="http://localhost:3847", help="Backend URL")
    parser.add_argument("--project", type=str, default="ts-tui-pi", help="Project name")
    args = parser.parse_args()

    # Find the TUI directory
    tui_dir = Path(__file__).parent.parent / "ts-tui-pi"
    if not tui_dir.exists():
        print(f"TUI directory not found: {tui_dir}")
        sys.exit(1)

    # Create renderer
    renderer = TerminalRenderer(cols=args.cols, rows=args.rows, font_size=args.font_size)

    # Create TUI process — run bun with the TUI
    cmd = ["bun", "run", "src/main.ts", "--project", args.project, "--server", args.server]
    tui = TUIProcess(cmd, str(tui_dir), renderer)

    # Wire handler
    TUIVisualHandler.tui = tui
    TUIVisualHandler.renderer = renderer

    # Start TUI
    print(f"Starting TUI: {' '.join(cmd)}")
    print(f"Working directory: {tui_dir}")
    tui.start()
    time.sleep(1)

    # Start HTTP server
    server = HTTPServer(("0.0.0.0", args.port), TUIVisualHandler)
    print(f"\nTUI Visual Testing Server running on port {args.port}")
    print(f"  GET  /screenshot  → PNG of current TUI state")
    print(f"  GET  /screen      → terminal buffer as text")
    print(f"  POST /input       → send keystrokes")
    print(f"  POST /restart     → restart TUI process")
    print(f"  GET  /health      → server status")
    print(f"\nProcess running: {tui.running} (pid={tui.pid})")
    print(f"Terminal: {args.cols}x{args.rows}, font size: {args.font_size}")
    print()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down...")
        tui.stop()
        server.server_close()


if __name__ == "__main__":
    main()
