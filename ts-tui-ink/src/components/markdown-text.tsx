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
			elements.push(
				<Text key={i} dimColor={dim}>
					<Text color="gray">{"  "}{BLOCKQUOTE_BAR} </Text>
					{renderSegments(parseInline(content))}
				</Text>,
			);
			continue;
		}

		// Numbered lists
		if (line.match(/^\d+\.\s/)) {
			const content = line.replace(/^\d+\.\s/, "");
			const num = line.match(/^\d+/)?.[0] || "1";
			elements.push(
				<Text key={i} dimColor={dim}>
					<Text color="gray">{num}. </Text>
					{renderSegments(parseInline(content))}
				</Text>,
			);
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
			elements.push(
				<Text key={i} color="gray">
					{"─".repeat(40)}
				</Text>,
			);
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
