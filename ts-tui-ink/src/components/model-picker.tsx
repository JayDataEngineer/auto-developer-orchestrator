/**
 * ModelPicker — interactive model selection overlay.
 *
 * Arrow keys navigate, Enter selects, Escape cancels.
 * Shows current model with ← marker.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useColors, symbols } from "../theme.js";

export function ModelPicker() {
	const show = usePuxStore((s) => s.showModelPicker);
	const models = usePuxStore((s) => s.modelList);
	const activeModel = usePuxStore((s) => s.activeModel);
	const setModel = usePuxStore((s) => s.setModel);
	const toggle = usePuxStore((s) => s.toggleModelPicker);
	const colors = useColors();
	const [idx, setIdx] = useState(0);

	useInput((_, key) => {
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
	});

	if (!show || models.length === 0) return null;

	return (
		<Box flexDirection="column" paddingY={1} paddingX={1}>
			<Box backgroundColor="magenta" paddingX={1}>
				<Text bold>Model</Text>
			</Box>
			<Box flexDirection="column" marginTop={1}>
				{models.map((m, i) => {
					const current = m.id === activeModel;
					const selected = i === idx;
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
							{current && <Text color={colors.brand}> ←</Text>}
						</Text>
					);
				})}
			</Box>
			<Box marginTop={1}>
				<Text dimColor color="gray">
					↑↓ navigate · Enter select · Esc cancel
				</Text>
			</Box>
		</Box>
	);
}
