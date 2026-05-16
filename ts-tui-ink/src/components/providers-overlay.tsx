/**
 * ProvidersOverlay — fullscreen model/provider browser.
 *
 * Takes over the entire content area when active. Two-level navigation:
 *   1. Provider list (↑↓ navigate, Enter expand, Escape close)
 *   2. Expanded provider models (↑↓ navigate, Enter select model, Escape collapse)
 *
 * Visually distinct from chat: cyan header, double-line rule, indented model rows.
 */

import React, { useState, useMemo, useCallback } from "react";
import { Box, Text, useInput, useStdout } from "ink";
import { usePuxStore, type ProvidersMap, type ModelInfo } from "@pux/shared";
import { colors, symbols } from "../theme.js";

export function ProvidersOverlay() {
	// ALL hooks before any conditional returns
	const show = usePuxStore((s) => s.showProvidersOverlay);
	const providers = usePuxStore((s) => s.providers);
	const activeModel = usePuxStore((s) => s.activeModel);
	const selectModel = usePuxStore((s) => s.selectModel);
	const closeProvidersOverlay = usePuxStore((s) => s.closeProvidersOverlay);
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [expandedProvider, setExpandedProvider] = useState<string | null>(null);
	const { stdout } = useStdout();
	const rows = stdout?.rows ?? 24;
	const cols = stdout?.columns ?? 80;

	// Build sorted provider list
	const providerNames = useMemo(() => Object.keys(providers).sort(), [providers]);

	// Build flat navigation items: providers + expanded models
	type NavItem =
		| { type: "provider"; name: string }
		| { type: "model"; provider: string; model: ModelInfo };

	const navItems = useMemo(() => {
		const items: NavItem[] = [];
		for (const name of providerNames) {
			items.push({ type: "provider", name });
			if (expandedProvider === name) {
				for (const model of providers[name].models) {
					items.push({ type: "model", provider: name, model });
				}
			}
		}
		return items;
	}, [providerNames, expandedProvider, providers]);

	// Clamp selection
	const clampedIdx = Math.min(selectedIdx, Math.max(0, navItems.length - 1));

	useInput(useCallback((_input: string, key: any) => {
		if (!show || navItems.length === 0) return;

		if (key.upArrow) {
			setSelectedIdx((i) => Math.max(0, i - 1));
			return;
		}
		if (key.downArrow) {
			setSelectedIdx((i) => Math.min(navItems.length - 1, i + 1));
			return;
		}
		if (key.escape) {
			if (expandedProvider) {
				// Collapse — reset selection to the provider row
				const providerRowIdx = navItems.findIndex(
					(item) => item.type === "provider" && item.name === expandedProvider,
				);
				setExpandedProvider(null);
				setSelectedIdx(providerRowIdx >= 0 ? providerRowIdx : 0);
			} else {
				closeProvidersOverlay();
			}
			return;
		}
		if (key.return) {
			const item = navItems[clampedIdx];
			if (!item) return;

			if (item.type === "provider") {
				if (expandedProvider === item.name) {
					// Collapse
					setExpandedProvider(null);
				} else {
					// Expand — selection stays on provider row, but now models appear below
					setExpandedProvider(item.name);
					setSelectedIdx(clampedIdx); // keep position
				}
			} else {
				// Select model
				selectModel(item.provider, item.model.id);
			}
		}
	}, [show, navItems, clampedIdx, expandedProvider, closeProvidersOverlay, selectModel]));

	// NOW safe to return conditionally
	if (!show) return null;

	// Visible viewport (leave room for header + footer + padding)
	const maxVisible = rows - 8;
	const scrollOffset = Math.max(0, clampedIdx - maxVisible + 3);
	const visibleItems = navItems.slice(scrollOffset, scrollOffset + maxVisible);

	return (
		<Box flexDirection="column" flexGrow={1}>
			{/* Header — distinct cyan color */}
			<Box paddingX={1}>
				<Text backgroundColor="cyan" bold> Providers & Models </Text>
			</Box>
			<Text color="cyan">{"═".repeat(cols)}</Text>

			{/* Content */}
			<Box flexDirection="column" paddingX={2} flexGrow={1}>
				{visibleItems.map((item, vi) => {
					const globalIdx = scrollOffset + vi;
					const isSelected = globalIdx === clampedIdx;

					if (item.type === "provider") {
						return (
							<ProviderRow
								key={`p-${item.name}`}
								name={item.name}
								provider={providers[item.name]}
								isSelected={isSelected}
								isExpanded={expandedProvider === item.name}
								activeModel={activeModel}
							/>
						);
					}

					return (
						<ModelRow
							key={`m-${item.provider}-${item.model.id}`}
							model={item.model}
							provider={item.provider}
							isSelected={isSelected}
							isActive={item.model.id === activeModel}
						/>
					);
				})}
			</Box>

			{/* Footer — controls hint */}
			<Text color="cyan">{"═".repeat(cols)}</Text>
			<Box paddingX={2}>
				<Text dimColor color="gray">
					↑↓ navigate{symbols.dot}Enter {expandedProvider ? "select model" : "expand"}{symbols.dot}Esc {expandedProvider ? "back" : "close"}
				</Text>
			</Box>
		</Box>
	);
}

// ── Provider Row ──

function ProviderRow({
	name,
	provider,
	isSelected,
	isExpanded,
	activeModel,
}: {
	name: string;
	provider: { status: string; models: ModelInfo[] };
	isSelected: boolean;
	isExpanded: boolean;
	activeModel: string;
}) {
	const hasActive = provider.models.some((m) => m.id === activeModel);
	const statusIcon = provider.status === "available" ? "●" : "○";
	const statusColor = provider.status === "available" ? "green" : "gray";

	return (
		<Box>
			<Text backgroundColor={isSelected ? "gray" : undefined} bold={isSelected}>
				{" "}
				{isSelected ? symbols.arrow : " "}{" "}
			</Text>
			<Text color={statusColor}>{statusIcon} </Text>
			<Text bold={isSelected}>{name}</Text>
			<Text dimColor color="gray"> ({provider.models.length} model{provider.models.length !== 1 ? "s" : ""})</Text>
			<Text color={statusColor}> {provider.status}</Text>
			{hasActive && <Text color={colors.brand}> ←</Text>}
			{isExpanded && <Text dimColor> ▼</Text>}
		</Box>
	);
}

// ── Model Row ──

function ModelRow({
	model,
	provider,
	isSelected,
	isActive,
}: {
	model: ModelInfo;
	provider: string;
	isSelected: boolean;
	isActive: boolean;
}) {
	const ctxLabel = formatTokenCount(model.contextWindow);
	const outLabel = formatTokenCount(model.maxTokens);
	const inputLabel = model.input.join("·");
	const isFree = model.cost.input === 0 && model.cost.output === 0;
	const costLabel = isFree ? "free" : `$${model.cost.input}/$${model.cost.output}`;

	return (
		<Box>
			<Text backgroundColor={isSelected ? "gray" : undefined}>
				{"    "}
				{isSelected ? symbols.arrow : " "}
				{" "}
			</Text>
			<Text
				color={isActive ? colors.brand : undefined}
				bold={isActive || isSelected}
			>
				{model.name}
			</Text>
			<Text dimColor color="gray"> {ctxLabel} ctx</Text>
			<Text dimColor color="gray"> {outLabel} out</Text>
			<Text dimColor color="gray"> {inputLabel}</Text>
			{model.reasoning && <Text color="yellow"> [R]</Text>}
			<Text dimColor color="gray"> {costLabel}</Text>
			{isActive && <Text color={colors.brand}> ←</Text>}
		</Box>
	);
}

// ── Helpers ──

function formatTokenCount(n: number): string {
	if (n >= 1_000_000) {
		const m = n / 1_000_000;
		return m === Math.floor(m) ? `${m}M` : `${m.toFixed(1)}M`;
	}
	if (n >= 1_000) {
		const k = n / 1_000;
		if (k === Math.floor(k)) return `${k}K`;
		// For common power-of-2 values, use those
		if (n >= 1024 && n % 1024 === 0) {
			const kb = n / 1024;
			return kb >= 1024 ? `${(kb / 1024).toFixed(kb % 1024 === 0 ? 0 : 1)}M` : `${kb}K`;
		}
		return `${k.toFixed(1)}K`;
	}
	return String(n);
}
