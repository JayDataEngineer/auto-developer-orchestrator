// Terminal panel: xterm.js + WebSocket to /api/terminal/ws. The host BFF
// spawns a real PTY at the workspace root; this component is just the
// viewport + keyboard/mouse bridge.

import { useEffect, useRef, type FC } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

const CTRL = 0x01; // matches server/terminal.ts

export const TerminalPanel: FC = () => {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  useEffect(() => {
    if (!hostRef.current) return;
    const term = new Terminal({
      fontFamily:
        "'JetBrains Mono', 'Fira Code', 'SF Mono', Menlo, Consolas, monospace",
      fontSize: 12,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(hostRef.current);
    fit.fit();
    termRef.current = term;
    fitRef.current = fit;

    // Open WS. Use the same protocol/host as the page so vite dev proxy works.
    const wsUrl = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/terminal/ws?cols=${term.cols}&rows=${term.rows}`;
    const ws = new WebSocket(wsUrl);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      // Initial resize sync — fitAddon may have adjusted dims after URL build.
      sendResize();
    };

    ws.onmessage = (ev) => {
      const data = typeof ev.data === "string"
        ? ev.data
        : new TextDecoder().decode(ev.data as ArrayBuffer);
      term.write(data);
    };

    ws.onclose = () => {
      term.write("\r\n\x1b[31m[disconnected]\x1b[0m\r\n");
    };
    ws.onerror = () => {
      term.write("\r\n\x1b[31m[socket error]\x1b[0m\r\n");
    };

    // Keyboard → WS (text frames, server treats as PTY stdin)
    const disposable = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    });

    function sendResize() {
      if (ws.readyState !== WebSocket.OPEN) return;
      const ctrl = `\x01RESIZE:${term.rows}:${term.cols}`;
      ws.send(ctrl);
    }

    // ResizeObserver → fitAddon → push new dims to server.
    const ro = new ResizeObserver(() => {
      try {
        fit.fit();
        sendResize();
      } catch {
        // container torn down
      }
    });
    ro.observe(hostRef.current);

    return () => {
      disposable.dispose();
      ro.disconnect();
      try { ws.close(); } catch {}
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
      wsRef.current = null;
    };
  }, []);

  return (
    <div className="h-full bg-[#1e1e1e]">
      <div ref={hostRef} className="h-full w-full p-1" />
    </div>
  );
};
