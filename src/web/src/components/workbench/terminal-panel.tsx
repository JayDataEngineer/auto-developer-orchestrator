import { useEffect, useRef, useCallback } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

interface TerminalPanelProps {
	cwd?: string;
}

export function TerminalPanel({ cwd }: TerminalPanelProps) {
	const containerRef = useRef<HTMLDivElement>(null);
	const termRef = useRef<Terminal | null>(null);
	const wsRef = useRef<WebSocket | null>(null);
	const fitRef = useRef<FitAddon | null>(null);
	const cwdRef = useRef(cwd);
	cwdRef.current = cwd;

	// Connect WebSocket to existing terminal
	const connectWS = useCallback(() => {
		// Close old ws if any
		if (wsRef.current) {
			wsRef.current.close();
		}

		const term = termRef.current;
		if (!term) return;

		const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
		let url = `${proto}//${window.location.host}/api/terminal/ws?shell=bash`;
		if (cwdRef.current) url += `&cwd=${encodeURIComponent(cwdRef.current)}`;

		const ws = new WebSocket(url);
		ws.binaryType = "arraybuffer";
		wsRef.current = ws;

		ws.onmessage = (e) => {
			const data = e.data instanceof ArrayBuffer
				? new TextDecoder().decode(e.data)
				: e.data;
			term.write(data);
		};

		ws.onclose = () => {
			term.write("\r\n\x1b[90m[disconnected — press Enter to reconnect]\x1b[0m");
		};
	}, []);

	// Initialize terminal once
	useEffect(() => {
		if (!containerRef.current) return;

		const term = new Terminal({
			cursorBlink: true,
			fontSize: 13,
			fontFamily: "Menlo, Monaco, 'Courier New', monospace",
			theme: {
				background: "#1a1a2e",
				foreground: "#e0e0e0",
				cursor: "#e0e0e0",
				selectionBackground: "#444466",
				black: "#1a1a2e",
				red: "#e74c3c",
				green: "#2ecc71",
				yellow: "#f1c40f",
				blue: "#3498db",
				magenta: "#9b59b6",
				cyan: "#1abc9c",
				white: "#ecf0f1",
				brightBlack: "#555577",
				brightRed: "#e74c3c",
				brightGreen: "#2ecc71",
				brightYellow: "#f1c40f",
				brightBlue: "#3498db",
				brightMagenta: "#9b59b6",
				brightCyan: "#1abc9c",
				brightWhite: "#ecf0f1",
			},
		});

		const fit = new FitAddon();
		term.loadAddon(fit);
		term.open(containerRef.current);
		fit.fit();
		term.focus();

		termRef.current = term;
		fitRef.current = fit;

		// Single data handler — send to ws or reconnect on Enter
		term.onData((data) => {
			const ws = wsRef.current;
			if (ws && ws.readyState === WebSocket.OPEN) {
				ws.send(new TextEncoder().encode(data));
			} else if (!ws || ws.readyState === WebSocket.CLOSED) {
				if (data === "\r") {
					term.clear();
					connectWS();
				}
			}
		});

		// Initial connection
		connectWS();

		// Resize handler
		const onResize = () => fit.fit();
		window.addEventListener("resize", onResize);

		return () => {
			window.removeEventListener("resize", onResize);
			wsRef.current?.close();
			term.dispose();
		};
	}, [connectWS]);

	// Fit on container resize
	useEffect(() => {
		if (!containerRef.current) return;
		const observer = new ResizeObserver(() => {
			fitRef.current?.fit();
		});
		observer.observe(containerRef.current);
		return () => observer.disconnect();
	}, []);

	return (
		<div
			ref={containerRef}
			className="h-full w-full bg-[#1a1a2e]"
			style={{ padding: "4px 0 0 4px" }}
		/>
	);
}
