#!/usr/bin/env python3
"""
Flash detection test v5 — FULL DUMP of every PTY chunk after Enter.
Prints the complete content of each chunk so we can see EXACTLY
what the terminal renders at each point in time.

Exit 1 = flash found, exit 0 = clean.
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

# Different magic each run to avoid false matches from previous runs
MAGIC = f"FLASHPROBE{os.getpid() % 10000:04d}"


def strip_ansi(text):
    return re.sub(r'\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b\[[\?0-9;]*[a-zA-Z]|\x1b\([A-Z0-9]', '', text)


def main():
    print(f"=== Flash Detection v5 (magic={MAGIC}) ===\n")

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
                r, _, _ = select.select([master_fd], [], [], 0.001)  # 1ms granularity
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
        print("Waiting for TUI...")
        time.sleep(4)

        # Type the magic text
        print(f"Typing: {MAGIC}")
        for ch in MAGIC:
            os.write(master_fd, ch.encode())
            time.sleep(0.015)
        time.sleep(0.6)

        # Verify text is on screen
        screen = strip_ansi("".join(c[1] for c in chunks))
        if MAGIC not in screen:
            print(f"ERROR: {MAGIC} not found after typing!")
            print(f"Screen snippet (last 500 chars):")
            print(strip_ansi("".join(c[1] for c in chunks[-5:]))[:500])
            sys.exit(2)
        print(f"  Confirmed: {MAGIC} on screen\n")

        # Clear and reset for Enter
        chunks.clear()
        start_time = time.time()

        print(f">>> Pressing Enter <<<\n")
        os.write(master_fd, b"\r")

        # Capture 3 seconds of output
        time.sleep(3.0)
        capture_done.set()

        # Print FULL content of every chunk
        print(f"Captured {len(chunks)} chunks:\n")

        flash_count = 0
        for i, (ts, text) in enumerate(chunks):
            clean = strip_ansi(text)

            # Print every chunk's content (up to 300 chars)
            print(f"--- Chunk {i:3d} t={ts:.4f}s ({len(clean)} chars) ---")
            for line in clean.split('\n'):
                if line.strip():
                    print(f"  {line[:120]}")
            print()

            # Check if magic appears in the COMPOSER area specifically.
            # The composer is between the last two separator lines (───).
            # A flash = magic in both the thread AND the composer in the same chunk.
            lines = clean.split('\n')
            sep_lines = [j for j, l in enumerate(lines) if l.count('─') > 10]
            if len(sep_lines) >= 2:
                # Composer area = between last two ── separators
                composer_lines = lines[sep_lines[-2]:sep_lines[-1]+1]
                composer_text = '\n'.join(composer_lines)
                if MAGIC in composer_text:
                    # Check if thread also has magic (not just thinking echo)
                    above_composer = '\n'.join(lines[:sep_lines[-2]])
                    if MAGIC in above_composer:
                        flash_count += 1
                        print(f"  ^^^ FLASH: magic in COMPOSER area while also in thread ^^^\n")

        print(f"{'=' * 60}")
        print(f"Flash count: {flash_count}")
        if flash_count > 0:
            print(f"\n*** FLASH DETECTED ***")
            sys.exit(1)
        else:
            print(f"\nNo flash detected.")
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
