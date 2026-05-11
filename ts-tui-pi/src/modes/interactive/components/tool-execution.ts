import { Container, getCapabilities, Image, Spacer, Text, type TUI } from "@mariozechner/pi-tui";
import type { ToolDefinition, ToolRenderContext } from "../../../core/extensions/types.js";
import { getTextOutput as getRenderedTextOutput } from "../../../core/tools/render-utils.js";
import { convertToPng } from "../../../utils/image-convert.js";
import { theme } from "../theme/theme.js";

export interface ToolExecutionOptions {
	showImages?: boolean;
}

export class ToolExecutionComponent extends Container {
	private contentText: Text;
	private imageComponents: Image[] = [];
	private imageSpacers: Spacer[] = [];
	private toolName: string;
	private toolCallId: string;
	private args: any;
	private expanded = false;
	private showImages: boolean;
	private isPartial = true;
	private ui: TUI;
	private cwd: string;
	private executionStarted = false;
	private argsComplete = false;
	private result?: {
		content: Array<{ type: string; text?: string; data?: string; mimeType?: string }>;
		isError: boolean;
		details?: any;
	};
	private convertedImages: Map<number, { data: string; mimeType: string }> = new Map();
	private hideComponent = false;
	private toolDefinition: ToolDefinition<any, any> | undefined;
	private renderState: any = {};
	private customComponent?: import("@mariozechner/pi-tui").Component;

	constructor(
		toolName: string,
		toolCallId: string,
		args: any,
		options: ToolExecutionOptions = {},
		toolDefinition: ToolDefinition<any, any> | undefined,
		ui: TUI,
		cwd: string = process.cwd(),
	) {
		super();
		this.toolName = toolName;
		this.toolCallId = toolCallId;
		this.args = args;
		this.showImages = options.showImages ?? true;
		this.toolDefinition = toolDefinition;
		this.ui = ui;
		this.cwd = cwd;

		this.contentText = new Text("", 1, 0, (text: string) => text);
		this.addChild(this.contentText);

		this.updateDisplay();
	}

	updateArgs(args: any): void {
		this.args = args;
		this.updateDisplay();
	}

	markExecutionStarted(): void {
		this.executionStarted = true;
		this.updateDisplay();
		this.ui.requestRender();
	}

	setArgsComplete(): void {
		this.argsComplete = true;
		this.updateDisplay();
		this.ui.requestRender();
	}

	updateResult(
		result: {
			content: Array<{ type: string; text?: string; data?: string; mimeType?: string }>;
			details?: any;
			isError: boolean;
		},
		isPartial = false,
	): void {
		this.result = result;
		this.isPartial = isPartial;
		try {
			this.updateDisplay();
		} catch (e) {
			// Prevent render errors from crashing the TUI
			// Fallback: show raw error as text
			this.result = { content: [{ type: "text", text: String(result ?? "") }], isError: true };
			this.updateDisplay();
		}
		this.maybeConvertImagesForKitty();
	}

	private maybeConvertImagesForKitty(): void {
		const caps = getCapabilities();
		if (caps.images !== "kitty") return;
		if (!this.result || !this.result.content) return;

		const imageBlocks = this.result.content.filter((c) => c.type === "image");
		for (let i = 0; i < imageBlocks.length; i++) {
			const img = imageBlocks[i];
			if (!img.data || !img.mimeType) continue;
			if (img.mimeType === "image/png") continue;
			if (this.convertedImages.has(i)) continue;

			const index = i;
			convertToPng(img.data, img.mimeType).then((converted) => {
				if (converted) {
					this.convertedImages.set(index, converted);
					this.updateDisplay();
					this.ui.requestRender();
				}
			});
		}
	}

	setExpanded(expanded: boolean): void {
		this.expanded = expanded;
		this.updateDisplay();
	}

	setShowImages(show: boolean): void {
		this.showImages = show;
		this.updateDisplay();
	}

	override invalidate(): void {
		super.invalidate();
		this.updateDisplay();
	}

	override render(width: number): string[] {
		if (this.hideComponent) {
			return [];
		}
		return super.render(width);
	}

	private createRenderContext(): ToolRenderContext {
		return {
			args: this.args,
			toolCallId: this.toolCallId,
			invalidate: () => { this.updateDisplay(); this.ui.requestRender(); },
			lastComponent: this.customComponent,
			state: this.renderState,
			cwd: this.cwd,
			executionStarted: this.executionStarted,
			argsComplete: this.argsComplete,
			isPartial: this.isPartial,
			expanded: this.expanded,
			showImages: this.showImages,
			isError: this.result?.isError ?? false,
		};
	}

	private removeCustomComponent(): void {
		if (this.customComponent) {
			this.removeChild(this.customComponent);
			this.customComponent = undefined;
		}
	}

	private updateDisplay(): void {
		let hasContent = false;
		this.hideComponent = false;

		// Try custom renderResult first (when we have a result)
		if (this.result && this.toolDefinition?.renderResult) {
			try {
				const component = this.toolDefinition.renderResult(
					{ content: this.result.content as any[], details: this.result.details, isError: this.result.isError },
					{ expanded: this.expanded, isPartial: this.isPartial },
					theme,
					this.createRenderContext(),
				);
				if (component) {
					this.removeCustomComponent();
					this.contentText.setText("");
					this.customComponent = component;
					this.addChild(component);
					hasContent = true;

					// Still handle images from result
					this.updateImageComponents();
					if (!hasContent && this.imageComponents.length === 0) {
						this.hideComponent = true;
					}
					return;
				}
			} catch {
				// Fall through to default rendering
			}
		}

		// Try custom renderCall (when no result yet, or result rendering fell through)
		if (!this.result && this.toolDefinition?.renderCall) {
			try {
				const component = this.toolDefinition.renderCall(
					this.args,
					theme,
					this.createRenderContext(),
				);
				if (component) {
					this.removeCustomComponent();
					this.contentText.setText("");
					this.customComponent = component;
					this.addChild(component);
					hasContent = true;

					this.updateImageComponents();
					if (!hasContent && this.imageComponents.length === 0) {
						this.hideComponent = true;
					}
					return;
				}
			} catch {
				// Fall through to default rendering
			}
		}

		// Default rendering
		this.removeCustomComponent();
		this.contentText.setText(this.formatToolExecution());
		hasContent = true;

		this.updateImageComponents();
		if (!hasContent && this.imageComponents.length === 0) {
			this.hideComponent = true;
		}
	}

	private updateImageComponents(): void {
		for (const img of this.imageComponents) {
			this.removeChild(img);
		}
		this.imageComponents = [];
		for (const spacer of this.imageSpacers) {
			this.removeChild(spacer);
		}
		this.imageSpacers = [];

		if (this.result && this.result.content) {
			const imageBlocks = this.result.content.filter((c) => c.type === "image");
			const caps = getCapabilities();
			for (let i = 0; i < imageBlocks.length; i++) {
				const img = imageBlocks[i];
				if (caps.images && this.showImages && img.data && img.mimeType) {
					const converted = this.convertedImages.get(i);
					const imageData = converted?.data ?? img.data;
					const imageMimeType = converted?.mimeType ?? img.mimeType;
					if (caps.images === "kitty" && imageMimeType !== "image/png") continue;

					const spacer = new Spacer(1);
					this.addChild(spacer);
					this.imageSpacers.push(spacer);
					const imageComponent = new Image(
						imageData,
						imageMimeType,
						{ fallbackColor: (s: string) => theme.fg("toolOutput", s) },
						{ maxWidthCells: 60 },
					);
					this.imageComponents.push(imageComponent);
					this.addChild(imageComponent);
				}
			}
		}
	}

	private getTextOutput(): string {
		return getRenderedTextOutput(this.result, this.showImages);
	}

	private formatToolExecution(): string {
		// Status dot: green=success, red=error, yellow=pending
		const dot = this.isPartial
			? theme.fg("warning", "●")
			: this.result?.isError
				? theme.fg("error", "●")
				: theme.fg("success", "●");

		const argsSummary = this.formatArgsSummary();
		let text = argsSummary ? `${this.toolName}: ${argsSummary}` : this.toolName;

		const output = this.getTextOutput();
		if (output) {
			text += `\n  ${output}`;
		}

		return `${dot} ${text}`;
	}

	/** Extract a one-line summary of the args — command, file path, pattern, etc. */
	private formatArgsSummary(): string {
		const a = this.args;
		if (!a || typeof a !== "object") return String(a ?? "");

		// bash/sh — show the command
		if (this.toolName === "bash" || this.toolName === "shell_exec") {
			const cmd = a.command || a.cmd || "";
			return cmd.length > 120 ? cmd.slice(0, 120) + "..." : cmd;
		}

		// file operations — show the path
		if (this.toolName === "file_read" || this.toolName === "read" ||
			this.toolName === "file_write" || this.toolName === "write" ||
			this.toolName === "file_edit" || this.toolName === "edit" ||
			this.toolName === "file_glob" || this.toolName === "find" ||
			this.toolName === "file_grep" || this.toolName === "grep") {
			return a.file_path || a.path || a.pattern || a.query || "";
		}

		// delegation — show role + task preview
		if (this.toolName === "delegate_to" || this.toolName === "delegate_async") {
			const role = a.instructions || a.step || a.agent_name || "agent";
			const task = a.task || "";
			const preview = task.length > 60 ? task.slice(0, 60) + "..." : task;
			return `${role}${preview ? `: ${preview}` : ""}`;
		}

		// memory — show key
		if (this.toolName === "memory" || this.toolName === "save_memory") {
			return a.key || a.action || "";
		}

		// generic — first string value under 80 chars
		for (const v of Object.values(a)) {
			if (typeof v === "string" && v.length > 0) {
				return v.length > 120 ? v.slice(0, 120) + "..." : v;
			}
		}
		return "";
	}
}
