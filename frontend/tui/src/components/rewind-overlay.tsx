import React, { useState, useEffect, useCallback } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore, getFetch, apiUrl } from "@pux/shared";
import { useColors, symbols } from "../theme.js";

interface Checkpoint {
	id: string;
	timestamp: string;
	preview: string;
}

function relativeTime(iso: string): string {
	const diff = Date.now() - new Date(iso).getTime();
	const mins = Math.floor(diff / 60000);
	if (mins < 1) return "just now";
	if (mins < 60) return `${mins}m ago`;
	const hrs = Math.floor(mins / 60);
	if (hrs < 24) return `${hrs}h ago`;
	const days = Math.floor(hrs / 24);
	if (days < 30) return `${days}d ago`;
	return new Date(iso).toLocaleDateString();
}

export function RewindOverlay() {
	const show = usePuxStore((s) => s.showRewindOverlay);
	const activeProject = usePuxStore((s) => s.activeProject);
	const activeAgentId = usePuxStore((s) => s.activeAgentId);
	const setConversation = usePuxStore((s) => s.setConversation);
	const close = usePuxStore((s) => s.closeRewindOverlay);
	const colors = useColors();

	const [checkpoints, setCheckpoints] = useState<Checkpoint[]>([]);
	const [idx, setIdx] = useState(0);
	const [phase, setPhase] = useState<"select" | "action">("select");
	const [loading, setLoading] = useState(false);

	// Load checkpoints when overlay opens
	useEffect(() => {
		if (!show) return;
		setIdx(0);
		setPhase("select");
		setLoading(true);

		const fetch = getFetch();
		const params = new URLSearchParams({ project: activeProject, agentId: activeAgentId });
		fetch(apiUrl(`/api/pux/rewind?${params}`))
			.then((r) => r.json())
			.then((data: Checkpoint[]) => {
				setCheckpoints(data);
				setIdx(Math.max(0, data.length - 1));
			})
			.catch(() => setCheckpoints([]))
			.finally(() => setLoading(false));
	}, [show]);

	const doRewind = useCallback(async () => {
		const cp = checkpoints[idx];
		if (!cp) return;

		const fetch = getFetch();
		const resp = await fetch(apiUrl("/api/pux/rewind"), {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				project: activeProject,
				agentId: activeAgentId,
				nodeId: cp.id,
			}),
		});

		if (resp.ok) {
			// Re-key the runtime to reload history from the rewound session
			setConversation(activeProject, activeAgentId);
		}
		close();
	}, [checkpoints, idx, activeProject, activeAgentId, setConversation, close]);

	useInput(
		useCallback(
			(input: string, key: any) => {
				if (!show) return;

				if (key.escape) {
					if (phase === "action") {
						setPhase("select");
					} else {
						close();
					}
					return;
				}

				if (phase === "select") {
					if (key.upArrow) {
						setIdx((prev) => Math.max(0, prev - 1));
						return;
					}
					if (key.downArrow) {
						setIdx((prev) => Math.min(checkpoints.length - 1, prev + 1));
						return;
					}
					if (key.return && checkpoints.length > 0) {
						setPhase("action");
						return;
					}
				}

				if (phase === "action") {
					if (input === "1") {
						doRewind();
						return;
					}
				}
			},
			[show, phase, checkpoints, idx, close, doRewind],
		),
		{ isActive: show },
	);

	if (!show) return null;

	if (loading) {
		return (
			<Box flexDirection="column" padding={1}>
				<Text color={colors.muted}>Loading checkpoints...</Text>
			</Box>
		);
	}

	if (checkpoints.length === 0) {
		return (
			<Box flexDirection="column" padding={1}>
				<Text color={colors.accent}>{symbols.pointer} Rewind</Text>
				<Box marginTop={1}>
					<Text color={colors.muted}>No checkpoints found in this conversation.</Text>
				</Box>
				<Box marginTop={1}>
					<Text color={colors.muted}>Press Escape to close</Text>
				</Box>
			</Box>
		);
	}

	return (
		<Box flexDirection="column" padding={1}>
			<Text color={colors.accent}>
				{symbols.pointer} {phase === "select" ? "Rewind — select a checkpoint" : "Rewind — choose action"}
			</Text>
			<Box marginTop={1} flexDirection="column">
				{phase === "select" ? (
					// Checkpoint list
					checkpoints.map((cp, i) => (
						<Box key={cp.id}>
							<Text color={i === idx ? colors.accent : colors.muted}>
								{i === idx ? `${symbols.pointer} ` : "  "}
							</Text>
							<Text color={i === idx ? colors.text : colors.muted}>
								{relativeTime(cp.timestamp)}
							</Text>
							<Text color={colors.muted}> {symbols.pipe} </Text>
							<Text color={i === idx ? colors.text : colors.muted}>
								{cp.preview.length > 80 ? cp.preview.slice(0, 77) + "..." : cp.preview}
							</Text>
						</Box>
					))
				) : (
					// Action menu
					<Box flexDirection="column">
						<Box marginBottom={1}>
							<Text color={colors.text}>
								Rewind to: {checkpoints[idx]?.preview.slice(0, 60)}
								{checkpoints[idx]?.preview.length > 60 ? "..." : ""}
							</Text>
						</Box>
						<Box>
							<Text color={colors.accent}>1</Text>
							<Text color={colors.text}> Restore conversation</Text>
						</Box>
						<Box>
							<Text color={colors.muted}>2</Text>
							<Text color={colors.muted}> Restore code (coming soon)</Text>
						</Box>
						<Box marginTop={1}>
							<Text color={colors.muted}>Escape to go back</Text>
						</Box>
					</Box>
				)}
			</Box>
		</Box>
	);
}
