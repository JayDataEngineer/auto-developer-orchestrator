import { useEffect, useRef, useCallback, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

interface TerminalPanelProps {
	cwd?: string;
}

// Parse ssh://user@host[:port]/path → { user, host, port, path }
function parseSshUrl(url: string): { user: string; host: string; port: string; path: string } | null {
	const m = url.match(/^ssh:\/\/([^@]+)@([^:/]+)(?::(\d+))?(\/.*)?$/);
	if (!m) return null;
	return { user: m[1], host: m[2], port: m[3] || "22", path: m[4] || "/" };
}

export function TerminalPanel({ cwd }: TerminalPanelProps) {
	const containerRef = useRef<HTMLDivElement>(null);
	const termRef = useRef<Terminal | null>(null);
	const wsRef = useRef<WebSocket | null>(null);
	const fitRef = useRef<FitAddon | null>(null);
	const cwdRef = useRef(cwd);
	const prevCwdRef = useRef(cwd);
	const sshSessionRef = useRef<string | null>(null);
	const [sshStatus, setSshStatus] = useState<string | null>(null);
	cwdRef.current = cwd;

	const isSsh = cwd?.startsWith("ssh://");

	// Connect WebSocket to existing terminal
	const connectWS = useCallback(() => {
		// Close old ws if any
		if (wsRef.current) {
			wsRef.current.close();
		}

		const term = termRef.current;
		if (!term) return;

		// SSH terminal
		if (cwdRef.current?.startsWith("ssh://")) {
			const ssh = parseSshUrl(cwdRef.current);
			if (!ssh) {
				term.write("\r\n\x1b[31mInvalid SSH URL\x1b[0m");
				return;
			}

			// Step 1: Establish SSH session
			setSshStatus("Connecting...");
			term.write(`\r\n\x1b[90mConnecting to ${ssh.user}@${ssh.host}...\x1b[0m`);

			fetch("/api/pux/ssh/connect", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ user: ssh.user, host: ssh.host, port: ssh.port, password: "", keyData: "" }),
			})
				.then(r => r.json())
				.then(data => {
					if (!data.sessionKey) {
						term.write(`\r\n\x1b[31mSSH failed: ${data.message || "unknown error"}\x1b[0m`);
						setSshStatus(null);
						return;
					}
					sshSessionRef.current = data.sessionKey;

					// Step 2: Connect WebSocket to SSH terminal
					const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
					const termObj = termRef.current;
					const rows = termObj?.rows || 24;
					const cols = termObj?.cols || 80;
					const wsUrl = `${proto}//${window.location.host}/api/terminal/ssh/ws?sessionKey=${data.sessionKey}&rows=${rows}&cols=${cols}&cwd=${encodeURIComponent(ssh.path)}`;

					const ws = new WebSocket(wsUrl);
					ws.binaryType = "arraybuffer";
					wsRef.current = ws;

					ws.onopen = () => {
						setSshStatus(`${ssh.user}@${ssh.host}`);
						term.write(`\r\n\x1b[32mConnected to ${ssh.host}\x1b[0m\r\n`);
					};

					ws.onmessage = (e) => {
						const data = e.data instanceof ArrayBuffer
							? new TextDecoder().decode(e.data)
							: e.data;
						term.write(data);
					};

					ws.onclose = () => {
						setSshStatus(null);
						term.write("\r\n\x1b[90m[disconnected — press Enter to reconnect]\x1b[0m");
					};
				})
				.catch(err => {
					term.write(`\r\n\x1b[31mConnection error: ${err}\x1b[0m`);
					setSshStatus(null);
				});
			return;
		}

		// Local terminal
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
		const onResize = () => {
			fit.fit();
			// Send resize message for SSH terminals
			const ws = wsRef.current;
			if (ws && ws.readyState === WebSocket.OPEN && cwdRef.current?.startsWith("ssh://")) {
				const msg = `\x01RESIZE:${term.rows}:${term.cols}`;
				ws.send(new TextEncoder().encode(msg));
			}
		};
		window.addEventListener("resize", onResize);

		return () => {
			window.removeEventListener("resize", onResize);
			wsRef.current?.close();
			term.dispose();
		};
	}, [connectWS]);

	// Auto-cd when project changes while terminal is open
	useEffect(() => {
		if (!cwd || cwd === prevCwdRef.current) return;
		// SSH terminals handle cwd at connect time — reconnect instead of cd
		if (cwd.startsWith("ssh://")) {
			prevCwdRef.current = cwd;
			// Reconnect to new SSH host
			connectWS();
			return;
		}
		const ws = wsRef.current;
		if (!ws || ws.readyState !== WebSocket.OPEN) {
			prevCwdRef.current = cwd;
			return;
		}
		// Send cd command to switch to the new project directory
		const cdCmd = `cd '${cwd.replace(/'/g, "'\\''")}'\n`;
		ws.send(new TextEncoder().encode(cdCmd));
		prevCwdRef.current = cwd;
	}, [cwd]);

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
		<div className="relative h-full w-full">
			{sshStatus && (
				<div className="absolute top-1 right-2 z-10 flex items-center gap-1.5 rounded bg-black/60 px-2 py-0.5 text-[10px] text-emerald-400 font-mono">
					<span className="inline-block size-1.5 rounded-full bg-emerald-400 animate-pulse" />
					{sshStatus}
				</div>
			)}
			<div
				ref={containerRef}
				className="h-full w-full bg-[#1a1a2e]"
				style={{ padding: "4px 0 0 4px" }}
			/>
		</div>
	);
}
