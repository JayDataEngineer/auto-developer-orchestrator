/**
 * MarkdownText — simple markdown renderer for Ink terminals.
 *
 * Supports: **bold**, `code`, # headers, - bullets, > blockquotes,
 * ```code blocks```, and [links](url).
 *
 * Returns an array of <Text> elements, one per visual line.
 */

import React from "react";
import { Text } from "ink";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";

interface Segment {
	text: string;
	bold?: boolean;
	italic?: boolean;
	code?: boolean;
	color?: string;
}

/** Parse a single inline line into styled segments. */
function parseInline(line: string): Segment[] {
	const segments: Segment[] = [];
	// Patterns: **bold**, *italic*, `code`, [text](url)
	const re = /(\*\*(.+?)\*\*|\*(.+?)\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\))/g;
	let lastIdx = 0;
	let match: RegExpExecArray | null;

	while ((match = re.exec(line)) !== null) {
		// Text before this match
		if (match.index > lastIdx) {
			segments.push({ text: line.slice(lastIdx, match.index) });
		}
		const full = match[1];
		if (full.startsWith("**")) {
			segments.push({ text: match[2], bold: true });
		} else if (full.startsWith("*")) {
			segments.push({ text: match[3], italic: true });
		} else if (full.startsWith("`")) {
			segments.push({ text: match[4], code: true });
		} else if (full.startsWith("[")) {
			segments.push({ text: match[5], color: "cyan" });
		}
		lastIdx = match.index + full.length;
	}
	// Remaining text
	if (lastIdx < line.length) {
		segments.push({ text: line.slice(lastIdx) });
	}
	return segments.length > 0 ? segments : [{ text: line }];
}

/** Render segments into <Text> children. */
function renderSegments(segments: Segment[]): React.ReactNode[] {
	return segments.map((seg, i) => (
		<Text key={i} bold={seg.bold} italic={seg.italic} color={seg.code ? "cyan" : seg.color}>
			{seg.code ? seg.text : seg.text}
		</Text>
	));
}

interface MarkdownTextProps {
	text: string;
	dim?: boolean;
	color?: string;
}

/** Wrap text to a max width, prefixing continuation lines with indent. */
function wrapText(text: string, maxWidth: number, indent: string): string[] {
	if (text.length <= maxWidth) return [text];
	const lines: string[] = [];
	let remaining = text;
	while (remaining.length > 0) {
		if (remaining.length <= maxWidth) {
			lines.push(remaining);
			break;
		}
		// Find break point at last space before maxWidth
		let cut = remaining.lastIndexOf(" ", maxWidth);
		if (cut < maxWidth * 0.4) cut = maxWidth; // no good break point — hard cut
		lines.push(remaining.slice(0, cut));
		remaining = indent + remaining.slice(cut).trimStart();
	}
	return lines;
}

export function MarkdownText({ text, dim, color }: MarkdownTextProps) {
	const colors = useColors();
	const lines = text.split("\n");
	const elements: React.ReactNode[] = [];

	let inCodeBlock = false;
	let codeBlockLines: string[] = [];
	let codeLang = "";

	for (let i = 0; i < lines.length; i++) {
		const line = lines[i];

		// Code block fences
		if (line.trimStart().startsWith("```")) {
			if (inCodeBlock) {
				// End code block
				inCodeBlock = false;
				for (const cl of codeBlockLines) {
					elements.push(
						<Text key={`code-${i}-${cl.slice(0, 10)}`} color={colors.assistant}>
							{"  "}{BLOCKQUOTE_BAR} {cl}
						</Text>,
					);
				}
				codeBlockLines = [];
			} else {
				// Start code block
				inCodeBlock = true;
				codeLang = line.trimStart().slice(3).trim();
			}
			continue;
		}

		if (inCodeBlock) {
			codeBlockLines.push(line);
			continue;
		}

		// Empty line
		if (!line.trim()) {
			elements.push(<Text key={`blank-${i}`}> </Text>);
			continue;
		}

		// Headers
		if (line.startsWith("### ")) {
			elements.push(
				<Text key={i} bold color={colors.brand}>
					{"  "}{line.slice(4)}
				</Text>,
			);
			continue;
		}
		if (line.startsWith("## ")) {
			elements.push(
				<Text key={i} bold color={colors.brand}>
					{line.slice(3)}
				</Text>,
			);
			continue;
		}
		if (line.startsWith("# ")) {
			elements.push(
				<Text key={i} bold color={colors.brand}>
					{line.slice(2)}
				</Text>,
			);
			continue;
		}

		// Bullet points
		if (line.match(/^[-*]\s/)) {
			const content = line.replace(/^[-*]\s/, "");
			const prefix = "  " + BLOCKQUOTE_BAR + " ";
			const wrapWidth = (process.stdout.columns || 80) - prefix.length - 2;
			const wrapped = wrapText(content, wrapWidth, "    ");
			for (let wi = 0; wi < wrapped.length; wi++) {
				const lineContent = wi === 0 ? wrapped[wi] : wrapped[wi];
				elements.push(
					<Text key={`${i}-${wi}`} dimColor={dim}>
						{wi === 0
							? <><Text color="gray">{prefix}</Text>{renderSegments(parseInline(lineContent))}</>
							: <>{renderSegments(parseInline(lineContent))}</>}
					</Text>,
				);
			}
			continue;
		}

		// Numbered lists
		if (line.match(/^\d+\.\s/)) {
			const content = line.replace(/^\d+\.\s/, "");
			const num = line.match(/^\d+/)?.[0] || "1";
			const prefix = num + ". ";
			const wrapWidth = (process.stdout.columns || 80) - prefix.length - 2;
			const wrapped = wrapText(content, wrapWidth, "   ");
			for (let wi = 0; wi < wrapped.length; wi++) {
				elements.push(
					<Text key={`${i}-${wi}`} dimColor={dim}>
						{wi === 0
							? <><Text color="gray">{prefix}</Text>{renderSegments(parseInline(wrapped[wi]))}</>
							: <>{renderSegments(parseInline(wrapped[wi]))}</>}
					</Text>,
				);
			}
			continue;
		}

		// Blockquote
		if (line.startsWith("> ")) {
			const content = line.slice(2);
			const isError = content.startsWith("**Error:**");
			elements.push(
				<Text key={i} color={isError ? colors.error : "gray"} dimColor={!isError}>
					{isError ? symbols.toolError : BLOCKQUOTE_BAR} {isError ? content.replace(/^\*\*Error:\*\*\s*/, "") : content}
				</Text>,
			);
			continue;
		}

		// Horizontal rule
		if (line.match(/^---+$/)) {
			const width = process.stdout.columns || 80;
			elements.push(
				<Text key={i} color="gray">
					{"─".repeat(Math.min(width - 2, 80))}
				</Text>,
			);
			continue;
		}

		// Table rows — preserve column alignment, truncate to terminal width
		if (line.startsWith("|")) {
			const width = (process.stdout.columns || 80) - 2;
			const isSeparator = /^\|[\s\-:]+\|/.test(line);
			const cells = line.split("|").filter(Boolean).map((c) => c.trim());
			if (isSeparator) {
				// Render separator with ─ characters
				elements.push(
					<Text key={i} color="gray">
						{" " + cells.map(() => "────────").join("│").slice(0, width)}
					</Text>,
				);
			} else if (cells.length > 0) {
				// Render cells with │ separator
				elements.push(
					<Text key={i}>
						{" "}{cells.map((cell, ci) => (
							<React.Fragment key={ci}>
								{ci > 0 && <Text color="gray">│</Text>}
								{renderSegments(parseInline(cell))}
							</React.Fragment>
						))}
					</Text>,
				);
			}
			continue;
		}

		// Regular text with inline formatting
		elements.push(
			<Text key={i} dimColor={dim} color={color}>
				{renderSegments(parseInline(line))}
			</Text>,
		);
	}

	return <>{elements}</>;
}
