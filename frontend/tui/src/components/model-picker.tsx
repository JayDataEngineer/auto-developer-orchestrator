/**
 * ModelPicker — interactive model selection overlay.
 *
 * Arrow keys navigate, Enter selects, Escape cancels.
 * Shows current model with ← marker.
 */

import React, { useEffect, useState } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useColors, symbols } from "../theme.js";

export function ModelPicker() {
	const show = usePuxStore((s) => s.showModelPicker);
	const models = usePuxStore((s) => s.modelList);
	const activeModel = usePuxStore((s) => s.activeModel);
	const setModel = usePuxStore((s) => s.setModel);
	const defaultLogic = usePuxStore((s) => s.defaultLogic);
	const defaultWorker = usePuxStore((s) => s.defaultWorker);
	const setDefaults = usePuxStore((s) => s.setDefaults);
	const loadDefaults = usePuxStore((s) => s.loadDefaults);
	const toggle = usePuxStore((s) => s.toggleModelPicker);
	const colors = useColors();
	const [idx, setIdx] = useState(0);

	useEffect(() => { loadDefaults(); }, []);

	useInput((input, key) => {
		if (!show || models.length === 0) return;
		if (key.escape) {
			toggle();
			return;
		}
		if (key.upArrow) {
			setIdx((i) => (i <= 0 ? models.length - 1 : i - 1));
			return;
		}
		if (key.downArrow) {
			setIdx((i) => (i + 1) % models.length);
			return;
		}
		if (key.return) {
			setModel(models[idx].id);
			return;
		}
		// Set defaults
		if (input === "l" || input === "L") {
			setDefaults(models[idx].id, defaultWorker);
			return;
		}
		if (input === "w" || input === "W") {
			setDefaults(defaultLogic, models[idx].id);
			return;
		}
	});

	if (!show || models.length === 0) return null;

	const logicName = models.find((m) => m.id === defaultLogic)?.name || defaultLogic;
	const workerName = models.find((m) => m.id === defaultWorker)?.name || defaultWorker;

	return (
		<Box flexDirection="column" paddingY={1} paddingX={1}>
			<Box backgroundColor="magenta" paddingX={1}>
				<Text bold>Model</Text>
			</Box>
			{defaultLogic || defaultWorker ? (
				<Box flexDirection="column" marginTop={1} marginBottom={1}>
					{defaultLogic && (
						<Text dimColor>
							<Text color="cyan">L</Text> Logic: {logicName}
						</Text>
					)}
					{defaultWorker && (
						<Text dimColor>
							<Text color="yellow">W</Text> Worker: {workerName}
						</Text>
					)}
				</Box>
			) : null}
			<Box flexDirection="column">
				{models.map((m, i) => {
					const current = m.id === activeModel;
					const selected = i === idx;
					const isLogic = m.id === defaultLogic;
					const isWorker = m.id === defaultWorker;
					const badges: string[] = [];
					if (isLogic) badges.push("L");
					if (isWorker) badges.push("W");
					const badgeStr = badges.length > 0 ? ` [${badges.join("")}]` : "";
					return (
						<Text key={m.id} backgroundColor={selected ? "gray" : undefined}>
							{"  "}
							<Text
								color={current ? colors.brand : undefined}
								bold={current || selected}
							>
								{m.name}
							</Text>
							<Text dimColor color="gray"> ({m.provider})</Text>
							{badgeStr && (
								<Text color={colors.brand}>{badgeStr}</Text>
							)}
							{current && <Text color={colors.brand}> ←</Text>}
						</Text>
					);
				})}
			</Box>
			<Box marginTop={1}>
				<Text dimColor color="gray">
					↑↓ navigate · Enter select · l logic · w worker · Esc cancel
				</Text>
			</Box>
		</Box>
	);
}
