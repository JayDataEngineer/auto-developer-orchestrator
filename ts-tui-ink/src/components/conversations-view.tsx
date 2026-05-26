/**
 * ConversationsView — thread list and conversation management.
 *
 * Uses ThreadListPrimitive and ThreadListItemPrimitive from assistant-ui
 * for conversation switching. Shows conversations loaded from the backend
 * with actions (open, delete).
 *
 * Falls back to reading from Zustand store since we use useLocalRuntime
 * (not useRemoteThreadListRuntime). In future, switch to the remote
 * runtime for full ThreadListPrimitive integration.
 */

import React, { useState, useEffect, useCallback } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useColors, symbols } from "../theme.js";

export function ConversationsView() {
	const conversations = usePuxStore((s) => s.conversations);
	const activeAgentId = usePuxStore((s) => s.activeAgentId);
	const activeProject = usePuxStore((s) => s.activeProject);
	const setConversation = usePuxStore((s) => s.setConversation);
	const deleteConversation = usePuxStore((s) => s.deleteConversation);
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
	const colors = useColors();

	// Keyboard navigation
	useInput(useCallback((input: string, key: any) => {
		if (conversations.length === 0) return;

		if (confirmDelete) {
			if (input === "y" || input === "Y") {
				const item = conversations.find((c) => c.agentId === confirmDelete);
				if (item) {
					deleteConversation(item.project, item.agentId);
				}
				setConfirmDelete(null);
			} else {
				setConfirmDelete(null);
			}
			return;
		}

		if (key.escape) {
			usePuxStore.getState().setTuiView("chat");
			return;
		}

		if (key.upArrow) {
			setSelectedIdx(Math.max(0, selectedIdx - 1));
			return;
		}
		if (key.downArrow) {
			setSelectedIdx(Math.min(conversations.length - 1, selectedIdx + 1));
			return;
		}
		if (key.return) {
			const conv = conversations[selectedIdx];
			if (conv) {
				setConversation(conv.project, conv.agentId);
			}
			return;
		}
		if (input === "d" || input === "D") {
			const conv = conversations[selectedIdx];
			if (conv) {
				setConfirmDelete(conv.agentId);
			}
			return;
		}
	}, [conversations, selectedIdx, confirmDelete, setConversation, deleteConversation]));

	// Reload conversations on mount
	useEffect(() => {
		usePuxStore.getState().loadConversations();
	}, []);

	if (conversations.length === 0) {
		return (
			<Box flexDirection="column" paddingX={2} paddingY={1}>
				<Text bold color={colors.brand}>Conversations</Text>
				<Box marginTop={1}>
					<Text dimColor>No conversations yet. Start chatting to create one.</Text>
				</Box>
			</Box>
		);
	}

	return (
		<Box flexDirection="column" paddingX={1}>
			{/* Header */}
			<Box marginBottom={1}>
				<Text bold color={colors.brand}>Conversations</Text>
				<Text color="gray"> {symbols.dot} {conversations.length} total</Text>
			</Box>

			{/* Conversation list */}
			{conversations.slice(0, 20).map((conv, i) => {
				const isActive = conv.agentId === activeAgentId && conv.project === activeProject;
				const isSelected = i === selectedIdx;
				const isDeleting = confirmDelete === conv.agentId;

				return (
					<Box key={`${conv.project}:${conv.agentId}`} flexDirection="column" marginBottom={0}>
						<Box>
							<Text color={isActive ? colors.brand : isSelected ? colors.running : "gray"}>
								{"   "}
							</Text>
							<Text bold={isSelected} color={isActive ? colors.brand : undefined}>
								{(conv.title && !conv.title.startsWith("REFLECT:") && !conv.title.startsWith("[SYSTEM:"))
									? conv.title
									: conv.lastMessage
										? conv.lastMessage.slice(0, 50)
										: "(untitled)"}
							</Text>
							<Text color="gray">
								{" "}
								{conv.messageCount} msgs {symbols.dot} {conv.agentId.slice(0, 8)}
							</Text>
						</Box>
						{conv.lastMessage && (
							<Text dimColor color="gray">
								{"    "}{conv.lastMessage.slice(0, 70)}
							</Text>
						)}
						{isDeleting && (
							<Text color={colors.warning}>
								{"    "}Delete this conversation? (y/N)
							</Text>
						)}
					</Box>
				);
			})}

			{/* Controls hint */}
			<Box marginTop={1}>
				<Text dimColor>
					<Text bold>Up/Down</Text> navigate <Text bold>Enter</Text> open <Text bold>D</Text> delete <Text bold>Esc</Text> back
				</Text>
			</Box>
		</Box>
	);
}
