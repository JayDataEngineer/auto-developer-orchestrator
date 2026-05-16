/**
 * ProvidersOverlay — fullscreen provider/model browser with add-provider flow.
 *
 * 4-screen state machine:
 *   A: Provider list (descriptions, type badges, "add provider" option)
 *   B: Provider detail (expanded, models list)
 *   C: Add provider (catalog picker)
 *   D: Configure provider (form with ink-text-input)
 *
 * Visually distinct from chat: cyan header, double-line rules, no chat elements.
 */

import React, { useState, useMemo, useCallback } from "react";
import { Box, Text, useInput, useStdout } from "ink";
import TextInput from "ink-text-input";
import { usePuxStore, type ProvidersMap, type ModelInfo } from "@pux/shared";
import {
	PROVIDER_CATALOG,
	TYPE_COLORS,
	TYPE_LABELS,
	type CatalogEntry,
	type ProviderType,
} from "../provider-catalog.js";
import { useColors, symbols } from "../theme.js";

type Screen = "list" | "detail" | "add" | "config";

export function ProvidersOverlay() {
	// ALL hooks before conditional returns
	const show = usePuxStore((s) => s.showProvidersOverlay);
	const providers = usePuxStore((s) => s.providers);
	const activeModel = usePuxStore((s) => s.activeModel);
	const selectModel = usePuxStore((s) => s.selectModel);
	const closeProvidersOverlay = usePuxStore((s) => s.closeProvidersOverlay);
	const addProvider = usePuxStore((s) => s.addProvider);

	const [screen, setScreen] = useState<Screen>("list");
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [expandedProvider, setExpandedProvider] = useState<string | null>(null);

	// Config form state
	const [configProvider, setConfigProvider] = useState<string>("");
	const [configBaseUrl, setConfigBaseUrl] = useState("");
	const [configApiKey, setConfigApiKey] = useState("");
	const [configModelId, setConfigModelId] = useState("");
	const [configModelName, setConfigModelName] = useState("");
	const [configField, setConfigField] = useState(0);
	const [configError, setConfigError] = useState<string | null>(null);

	const { stdout } = useStdout();
	const colors = useColors();
	const rows = stdout?.rows ?? 24;
	const cols = stdout?.columns ?? 80;

	// Sorted provider names
	const providerNames = useMemo(() => Object.keys(providers).sort(), [providers]);

	// Catalog entries not already configured
	const availableCatalog = useMemo(() => {
		const configured = new Set(providerNames);
		return Object.entries(PROVIDER_CATALOG)
			.filter(([id]) => !configured.has(id))
			.map(([id, entry]) => ({ id, ...entry }));
	}, [providerNames]);

	// Build nav items for list screen (providers + "add provider")
	const listItems = useMemo(() => {
		const items: Array<{ type: "provider"; name: string } | { type: "add" }> = [];
		for (const name of providerNames) {
			items.push({ type: "provider", name });
		}
		items.push({ type: "add" });
		return items;
	}, [providerNames]);

	// Build nav items for detail screen (provider info + models)
	const detailItems = useMemo(() => {
		if (!expandedProvider || !providers[expandedProvider]) return [];
		type Item = { type: "header" } | { type: "model"; model: ModelInfo };
		const items: Item[] = [{ type: "header" }];
		for (const model of providers[expandedProvider].models) {
			items.push({ type: "model", model });
		}
		return items;
	}, [expandedProvider, providers]);

	// Config form fields count
	const configFields = configProvider ? ["baseUrl", "apiKey", "modelId", "modelName"] : [];
	const maxConfigField = configFields.length - 1;

	// Keyboard handler
	useInput(useCallback((_input: string, key: any) => {
		if (!show) return;

		if (key.escape) {
			if (screen === "config") { setScreen("add"); setConfigError(null); return; }
			if (screen === "add") { setScreen("list"); return; }
			if (screen === "detail") {
				const pIdx = listItems.findIndex((it) => it.type === "provider" && it.name === expandedProvider);
				setExpandedProvider(null);
				setScreen("list");
				setSelectedIdx(pIdx >= 0 ? pIdx : 0);
				return;
			}
			closeProvidersOverlay();
			return;
		}

		// Screen-specific handlers
		if (screen === "list") {
			if (key.upArrow) {
				setSelectedIdx((i) => Math.max(0, i - 1));
				return;
			}
			if (key.downArrow) {
				setSelectedIdx((i) => Math.min(listItems.length - 1, i + 1));
				return;
			}
			if (_input === "a" || _input === "A") {
				setScreen("add");
				setSelectedIdx(0);
				return;
			}
			if (key.return) {
				const item = listItems[Math.min(selectedIdx, listItems.length - 1)];
				if (!item) return;
				if (item.type === "add") {
					setScreen("add");
					setSelectedIdx(0);
				} else {
					setExpandedProvider(item.name);
					setScreen("detail");
					setSelectedIdx(1); // skip header, select first model
				}
			}
		}

		if (screen === "detail") {
			if (key.upArrow) {
				setSelectedIdx((i) => Math.max(0, i - 1));
				return;
			}
			if (key.downArrow) {
				setSelectedIdx((i) => Math.min(detailItems.length - 1, i + 1));
				return;
			}
			if (key.return) {
				const item = detailItems[Math.min(selectedIdx, detailItems.length - 1)];
				if (item && item.type === "model") {
					selectModel(expandedProvider!, item.model.id);
				}
			}
		}

		if (screen === "add") {
			if (key.upArrow) {
				setSelectedIdx((i) => Math.max(0, i - 1));
				return;
			}
			if (key.downArrow) {
				const max = availableCatalog.length + 1; // +1 for custom
				setSelectedIdx((i) => Math.min(max - 1, i + 1));
				return;
			}
			if (key.return) {
				if (selectedIdx < availableCatalog.length) {
					const entry = availableCatalog[selectedIdx];
					setConfigProvider(entry.id);
					setConfigBaseUrl(entry.defaultBaseUrl);
					setConfigApiKey("");
					setConfigModelId("");
					setConfigModelName("");
					setConfigField(0);
					setConfigError(null);
					setScreen("config");
				} else {
					// Custom
					setConfigProvider("custom");
					setConfigBaseUrl("");
					setConfigApiKey("");
					setConfigModelId("");
					setConfigModelName("");
					setConfigField(0);
					setConfigError(null);
					setScreen("config");
				}
			}
		}

		if (screen === "config") {
			if (key.tab) {
				setConfigField((f) => (f + 1) % configFields.length);
				return;
			}
			if (key.return) {
				// Validate
				if (!configBaseUrl) { setConfigError("Base URL required"); return; }
				if (!configModelId) { setConfigError("Model ID required"); return; }
				if (!configModelName) { setConfigError("Model name required"); return; }
				const providerId = configProvider === "custom"
					? configBaseUrl.replace(/https?:\/\//, "").split(".")[0].replace(/[^a-z0-9]/g, "")
					: configProvider;
				if (!providerId) { setConfigError("Invalid provider"); return; }
				addProvider({
					id: providerId,
					baseUrl: configBaseUrl,
					apiKey: configApiKey || "sk-no-key",
					models: [{
						id: configModelId,
						name: configModelName,
						contextWindow: 128000,
						maxTokens: 8192,
					}],
				}).then(() => {
					setScreen("list");
					setSelectedIdx(0);
				});
			}
		}
	}, [show, screen, selectedIdx, listItems, detailItems, expandedProvider, availableCatalog, configFields.length, closeProvidersOverlay, selectModel, addProvider, configBaseUrl, configApiKey, configModelId, configModelName, configProvider]));

	// NOW safe to return conditionally
	if (!show) return null;

	const maxVisible = rows - 8;

	// ── Screen A: Provider List ──
	if (screen === "list") {
		const scrollOffset = Math.max(0, selectedIdx - maxVisible + 3);
		const visible = listItems.slice(scrollOffset, scrollOffset + maxVisible);

		return (
			<Box flexDirection="column" flexGrow={1}>
				<Header cols={cols} label="Providers & Models" />
				<Box flexDirection="column" paddingX={2} flexGrow={1}>
					{visible.map((item, vi) => {
						const globalIdx = scrollOffset + vi;
						const isSelected = globalIdx === selectedIdx;
						if (item.type === "add") {
							return <AddProviderRow key="add" isSelected={isSelected} />;
						}
						return (
							<ProviderRow
								key={item.name}
								name={item.name}
								provider={providers[item.name]}
								isSelected={isSelected}
								activeModel={activeModel}
								cols={cols}
							/>
						);
					})}
				</Box>
				<Footer cols={cols} hint="↑↓ navigate · Enter expand · a add · Esc close" />
			</Box>
		);
	}

	// ── Screen B: Provider Detail ──
	if (screen === "detail" && expandedProvider) {
		const provider = providers[expandedProvider];
		const catalog = PROVIDER_CATALOG[expandedProvider];
		const scrollOffset = Math.max(0, selectedIdx - maxVisible + 3);
		const visible = detailItems.slice(scrollOffset, scrollOffset + maxVisible);

		return (
			<Box flexDirection="column" flexGrow={1}>
				<Header cols={cols} label="Providers & Models" />
				<Box flexDirection="column" paddingX={2} flexGrow={1}>
					{/* Provider header */}
					<ProviderDetailHeader name={expandedProvider} provider={provider} catalog={catalog} />
					<Box marginTop={1} flexDirection="column">
						{visible.map((item, vi) => {
							const globalIdx = scrollOffset + vi;
							const isSelected = globalIdx === selectedIdx;
							if (item.type === "header") return null;
							return (
								<ModelRow
									key={item.model.id}
									model={item.model}
									provider={expandedProvider}
									isSelected={isSelected}
									isActive={item.model.id === activeModel}
									cols={cols}
								/>
							);
						})}
					</Box>
				</Box>
				<Footer cols={cols} hint="↑↓ navigate · Enter select model · Esc back" />
			</Box>
		);
	}

	// ── Screen C: Add Provider ──
	if (screen === "add") {
		const totalItems = availableCatalog.length + 1; // +1 for custom
		const scrollOffset = Math.max(0, selectedIdx - maxVisible + 3);
		const visible = availableCatalog.slice(scrollOffset, scrollOffset + maxVisible - 1);
		const showCustom = scrollOffset + maxVisible - 1 > availableCatalog.length;

		return (
			<Box flexDirection="column" flexGrow={1}>
				<Header cols={cols} label="Add Provider" />
				<Box flexDirection="column" paddingX={2} flexGrow={1}>
					<Text dimColor color="gray">Select a provider to configure:</Text>
					<Box marginTop={1} flexDirection="column">
						{visible.map((entry, vi) => {
							const globalIdx = scrollOffset + vi;
							const isSelected = globalIdx === selectedIdx;
							return (
								<CatalogRow key={entry.id} id={entry.id} entry={entry} isSelected={isSelected} cols={cols} />
							);
						})}
						{(showCustom || selectedIdx === availableCatalog.length) && (
							<CatalogRow
								id="custom"
								entry={{ name: "Custom", description: "Any OpenAI-compatible endpoint", type: "local" as ProviderType }}
								isSelected={selectedIdx === availableCatalog.length}
								cols={cols}
							/>
						)}
					</Box>
				</Box>
				<Footer cols={cols} hint="↑↓ navigate · Enter select · Esc cancel" />
			</Box>
		);
	}

	// ── Screen D: Config Form ──
	if (screen === "config") {
		const catalog = PROVIDER_CATALOG[configProvider];
		const title = catalog ? catalog.name : "Custom Provider";
		const fieldLabels = ["Base URL", "API Key", "Model ID", "Model Name"];
		const fieldValues = [configBaseUrl, configApiKey, configModelId, configModelName];
		const fieldSetters = [setConfigBaseUrl, setConfigApiKey, setConfigModelId, setConfigModelName];

		return (
			<Box flexDirection="column" flexGrow={1}>
				<Header cols={cols} label={`Configure: ${title}`} />
				<Box flexDirection="column" paddingX={2} flexGrow={1}>
					{fieldLabels.map((label, i) => (
						<Box key={label} marginTop={1}>
							<Box width={12}>
								<Text bold={configField === i} color={configField === i ? colors.brand : undefined}>
									{configField === i ? symbols.arrow : " "} {label}:
								</Text>
							</Box>
							<TextInput
								value={fieldValues[i]}
								onChange={fieldSetters[i]}
								focus={configField === i}
								mask={label === "API Key" && fieldValues[i].length > 0 ? "•" : undefined}
								placeholder={label === "Base URL" ? (catalog?.defaultBaseUrl || "https://...") : label}
							/>
						</Box>
					))}
					{configError && (
						<Box marginTop={1}>
							<Text color={colors.error}>{symbols.toolError} {configError}</Text>
						</Box>
					)}
				</Box>
				<Footer cols={cols} hint="Tab next field · Enter confirm · Esc cancel" />
			</Box>
		);
	}

	return null;
}

// ── Sub-components ──

function Header({ cols, label }: { cols: number; label: string }) {
	return (
		<Box flexDirection="column">
			<Box paddingX={1}>
				<Text backgroundColor="cyan" bold> {label} </Text>
			</Box>
			<Text color="cyan">{"═".repeat(cols)}</Text>
		</Box>
	);
}

function Footer({ cols, hint }: { cols: number; hint: string }) {
	return (
		<Box flexDirection="column">
			<Text color="cyan">{"═".repeat(cols)}</Text>
			<Box paddingX={2}>
				<Text dimColor color="gray">{hint}</Text>
			</Box>
		</Box>
	);
}

function ProviderRow({ name, provider, isSelected, activeModel, cols }: {
	name: string;
	provider: { status: string; models: ModelInfo[] };
	isSelected: boolean;
	activeModel: string;
	cols: number;
}) {
	const colors = useColors();
	const catalog = PROVIDER_CATALOG[name];
	const hasActive = provider.models.some((m) => m.id === activeModel);
	const statusIcon = provider.status === "available" ? "●" : "○";
	const statusColor = provider.status === "available" ? "green" : "gray";
	const typeColor = catalog ? TYPE_COLORS[catalog.type] : "gray";
	const typeLabel = catalog ? TYPE_LABELS[catalog.type] : "";
	const desc = catalog?.description || name;
	// Fixed columns: 3 (arrow+spaces) + 2 (icon) + 14 (name) + 12 (type) = 31
	// Available = (cols - 4 paddingX) - 31 fixed - (← if active)
	const descMax = cols - 4 - 31 - (hasActive ? 3 : 0);

	return (
		<Text backgroundColor={isSelected ? "gray" : undefined}>
			{" "}{isSelected ? symbols.arrow : " "}{" "}
			<Text color={statusColor}>{statusIcon} </Text>
			<Text bold={isSelected}>{name.padEnd(14)}</Text>
			<Text color={typeColor}>{typeLabel.padEnd(12)}</Text>
			<Text dimColor>{clip(desc, descMax)}</Text>
			{hasActive && <Text color={colors.brand}> ←</Text>}
		</Text>
	);
}

function AddProviderRow({ isSelected }: { isSelected: boolean }) {
	const colors = useColors();
	return (
		<Text backgroundColor={isSelected ? "gray" : undefined}>
			{" "}{isSelected ? symbols.arrow : " "}{" "}
			<Text color={colors.brand} bold>+ Add provider...</Text>
		</Text>
	);
}

function ProviderDetailHeader({ name, provider, catalog }: {
	name: string;
	provider: { baseUrl: string; status: string };
	catalog?: CatalogEntry;
}) {
	const statusColor = provider.status === "available" ? "green" : "gray";
	const statusIcon = provider.status === "available" ? "●" : "○";
	const typeColor = catalog ? TYPE_COLORS[catalog.type] : "gray";
	const typeLabel = catalog ? TYPE_LABELS[catalog.type] : "";

	return (
		<Box flexDirection="column">
			<Box>
				<Text color={statusColor}>{statusIcon} </Text>
				<Text bold>{name}</Text>
				<Text color={typeColor}> {typeLabel}</Text>
				<Text dimColor color="gray"> {provider.status}</Text>
			</Box>
			{catalog && <Text dimColor color="gray">{catalog.description}</Text>}
			<Text dimColor color="gray">{provider.baseUrl}</Text>
		</Box>
	);
}

function ModelRow({ model, provider, isSelected, isActive, cols }: {
	model: ModelInfo;
	provider: string;
	isSelected: boolean;
	isActive: boolean;
	cols: number;
}) {
	const colors = useColors();
	const ctxLabel = formatTokenCount(model.contextWindow);
	const outLabel = formatTokenCount(model.maxTokens);
	const inputLabel = model.input.join("·");
	const isFree = model.cost.input === 0 && model.cost.output === 0;
	const costLabel = isFree ? "free" : `$${model.cost.input}/$${model.cost.output}`;

	return (
		<Text backgroundColor={isSelected ? "gray" : undefined}>
			{"    "}{isSelected ? symbols.arrow : " "}{" "}
			<Text color={isActive ? colors.brand : undefined} bold={isActive || isSelected}>
				{model.name}
			</Text>
			<Text dimColor color="gray"> {ctxLabel} ctx</Text>
			<Text dimColor color="gray"> {outLabel} out</Text>
			<Text dimColor color="gray"> {inputLabel}</Text>
			{model.reasoning && <Text color="yellow"> [R]</Text>}
			<Text dimColor color="gray"> {costLabel}</Text>
			{isActive && <Text color={colors.brand}> ←</Text>}
		</Text>
	);
}

function CatalogRow({ id, entry, isSelected, cols }: {
	id: string;
	entry: { name: string; description: string; type: ProviderType };
	isSelected: boolean;
	cols: number;
}) {
	const typeColor = TYPE_COLORS[entry.type] || "gray";
	// Fixed: 3 (arrow) + 16 (name) + 10 (type) = 29
	const descMax = cols - 4 - 29;
	return (
		<Text backgroundColor={isSelected ? "gray" : undefined}>
			{" "}{isSelected ? symbols.arrow : " "}{" "}
			<Text bold={isSelected}>{entry.name.padEnd(16)}</Text>
			<Text color={typeColor}>{TYPE_LABELS[entry.type].padEnd(10)}</Text>
			<Text dimColor>{clip(entry.description, descMax)}</Text>
		</Text>
	);
}

// ── Helpers ──

/** Clip a string to fit within `maxLen` visible characters, appending … if truncated. */
function clip(s: string, maxLen: number): string {
	if (maxLen <= 0) return "";
	if (s.length <= maxLen) return s;
	return maxLen <= 1 ? "…" : s.slice(0, maxLen - 1) + "…";
}

function formatTokenCount(n: number): string {
	if (n >= 1_000_000) {
		const m = n / 1_000_000;
		return m === Math.floor(m) ? `${m}M` : `${m.toFixed(1)}M`;
	}
	if (n >= 1_000) {
		const k = n / 1_000;
		if (k === Math.floor(k)) return `${k}K`;
		if (n >= 1024 && n % 1024 === 0) {
			const kb = n / 1024;
			return kb >= 1024 ? `${(kb / 1024).toFixed(kb % 1024 === 0 ? 0 : 1)}M` : `${kb}K`;
		}
		return `${k.toFixed(1)}K`;
	}
	return String(n);
}
