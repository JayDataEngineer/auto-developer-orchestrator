#!/usr/bin/env python3
"""
Second message test — verifies the composer still works after sending.
Types text, presses Enter, then types again and presses Enter again.
Checks that both messages appear and the composer is usable throughout.
"""

import fcntl
import os
import pty
import re
import select
import signal
import struct
import sys
import termios
import time
import threading


PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
TUI_ENTRY = os.path.join(PROJECT_ROOT, "ts-tui-ink", "src", "main.tsx")
BUN = "/home/ubuntu/.bun/bin/bun"

MAGIC1 = "FIRSTMSG"
MAGIC2 = "SECONDMSG"


def strip_ansi(text):
    return re.sub(r'\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b\[[\?0-9;]*[a-zA-Z]|\x1b\([A-Z0-9]', '', text)


def main():
    print("=== Second Message Test ===\n")

    pid, master_fd = pty.fork()
    if pid == 0:
        os.chdir(PROJECT_ROOT)
        env = os.environ.copy()
        env["TERM"] = "xterm-256color"
        env["PROJECT_ROOT"] = PROJECT_ROOT
        os.execvp(BUN, [BUN, "run", TUI_ENTRY, "--project", "auto-developer-orchestrator"])

    COLS, ROWS = 120, 40
    winsize = struct.pack("HHHH", ROWS, COLS, 0, 0)
    fcntl.ioctl(master_fd, termios.TIOCSWINSZ, winsize)
    flags = fcntl.fcntl(master_fd, fcntl.F_GETFL)
    fcntl.fcntl(master_fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)

    chunks = []
    capture_done = threading.Event()
    start_time = time.time()

    def read_loop():
        while not capture_done.is_set():
            try:
                r, _, _ = select.select([master_fd], [], [], 0.01)
                if r:
                    data = os.read(master_fd, 65536)
                    if data:
                        ts = time.time() - start_time
                        chunks.append((ts, data.decode("utf-8", errors="replace")))
                    else:
                        break
            except (OSError, ValueError):
                break

    reader = threading.Thread(target=read_loop, daemon=True)
    reader.start()

    try:
        # Wait for TUI
        print("Waiting for TUI...")
        time.sleep(3)

        # === Message 1 ===
        print(f"\n--- Message 1: {MAGIC1} ---")
        for ch in MAGIC1:
            os.write(master_fd, ch.encode())
            time.sleep(0.02)
        time.sleep(0.3)

        screen = strip_ansi("".join(c[1] for c in chunks))
        if MAGIC1 not in screen:
            print(f"FAIL: {MAGIC1} not on screen after typing")
            sys.exit(1)
        print(f"  {MAGIC1} confirmed on screen")

        chunks.clear()
        start_time = time.time()
        os.write(master_fd, b"\r")
        time.sleep(2.0)

        screen = strip_ansi("".join(c[1] for c in chunks))
        if MAGIC1 not in screen:
            print(f"FAIL: {MAGIC1} not in output after Enter")
            sys.exit(1)
        print(f"  {MAGIC1} sent and visible in thread")

        # === Message 2 ===
        print(f"\n--- Message 2: {MAGIC2} ---")
        chunks.clear()
        start_time = time.time()

        for ch in MAGIC2:
            os.write(master_fd, ch.encode())
            time.sleep(0.02)
        time.sleep(0.5)

        screen = strip_ansi("".join(c[1] for c in chunks))
        if MAGIC2 not in screen:
            print(f"FAIL: {MAGIC2} not on screen after typing")
            print(f"  Last screen content:")
            for line in screen.split('\n')[-10:]:
                if line.strip():
                    print(f"    | {line[:100]}")
            sys.exit(1)
        print(f"  {MAGIC2} confirmed on screen — composer accepts input!")

        chunks.clear()
        start_time = time.time()
        os.write(master_fd, b"\r")
        time.sleep(2.0)

        screen = strip_ansi("".join(c[1] for c in chunks))
        if MAGIC2 not in screen:
            print(f"FAIL: {MAGIC2} not in output after Enter")
            sys.exit(1)
        print(f"  {MAGIC2} sent and visible in thread")

        print(f"\n*** PASS: Both messages sent successfully ***")
        sys.exit(0)

    finally:
        capture_done.set()
        try:
            os.kill(pid, signal.SIGTERM)
            for _ in range(10):
                p, status = os.waitpid(pid, os.WNOHANG)
                if p != 0:
                    break
                time.sleep(0.1)
            else:
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
        except (ProcessLookupError, ChildProcessError):
            pass
        try:
            os.close(master_fd)
        except OSError:
            pass


if __name__ == "__main__":
    main()
