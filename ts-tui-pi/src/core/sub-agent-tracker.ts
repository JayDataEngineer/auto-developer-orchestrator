/**
 * SubAgentTracker — buffers full per-agent conversations for the detail view.
 *
 * Absorbs and extends the old pux-subagents extension state tracking.
 * Stores tool events, text deltas, and thinking deltas per sub-agent
 * so the detail overlay can render the full conversation live.
 */

export interface SubAgentEntry {
	type: "thinking" | "text" | "tool_start" | "tool_end" | "tool_update";
	timestamp: number;
	text?: string;
	toolName?: string;
	toolArgs?: any;
	toolResult?: any;
	isError?: boolean;
}

export interface SubAgentState {
	agentName: string;
	task: string;
	status: "running" | "completed" | "failed";
	startedAt: number;
	endedAt?: number;
	toolCount: number;
	currentTool?: string;
	currentToolArgs?: string;
	recentTools: Array<{ tool: string; args: string }>;
	conversation: SubAgentEntry[];
	textPreview: string;
	thinkingPreview: string;
	error?: string;
}

const MAX_CONVERSATION = 500;
const PREVIEW_LEN = 200;

export class SubAgentTracker {
	private agents = new Map<string, SubAgentState>();
	private chain: string[] = [];
	private listeners = new Set<() => void>();

	onEvent(event: any): void {
		const agentName = event.agentName as string | undefined;

		switch (event.type) {
			case "subagent_start": {
				const name = event.agentName || "agent";
				const task = event.task || "";
				const state: SubAgentState = {
					agentName: name,
					task,
					status: "running",
					startedAt: Date.now(),
					toolCount: 0,
					recentTools: [],
					conversation: [],
					textPreview: "",
					thinkingPreview: "",
				};
				this.agents.set(name, state);
				if (!this.chain.includes(name)) this.chain.push(name);
				this.notify();
				break;
			}

			case "subagent_end": {
				const name = event.agentName || "agent";
				const s = this.agents.get(name);
				if (s) {
					s.status = event.status === "error" ? "failed" : "completed";
					s.endedAt = Date.now();
					s.currentTool = undefined;
					s.currentToolArgs = undefined;
					if (event.error) s.error = event.error;
					this.notify();
				}
				break;
			}

			case "subagent_text_delta": {
				if (!agentName) break;
				const s = this.agents.get(agentName);
				if (!s) break;
				const text = event.text || "";
				s.conversation.push({ type: "text", timestamp: Date.now(), text });
				s.textPreview = (s.textPreview + text).slice(-PREVIEW_LEN);
				this.trimConversation(s);
				this.notify();
				break;
			}

			case "subagent_thinking_delta": {
				if (!agentName) break;
				const s = this.agents.get(agentName);
				if (!s) break;
				const text = event.text || "";
				s.conversation.push({ type: "thinking", timestamp: Date.now(), text });
				s.thinkingPreview = (s.thinkingPreview + text).slice(-PREVIEW_LEN);
				this.trimConversation(s);
				this.notify();
				break;
			}

			case "tool_execution_start": {
				if (!agentName) break;
				const s = this.agents.get(agentName);
				if (!s) break;
				const toolName = event.toolName || "tool";
				const argsStr = this.truncArgs(event.args);
				s.toolCount++;
				s.currentTool = toolName;
				s.currentToolArgs = argsStr;
				s.recentTools.push({ tool: toolName, args: argsStr });
				s.conversation.push({
					type: "tool_start",
					timestamp: Date.now(),
					toolName,
					toolArgs: event.args,
				});
				this.trimConversation(s);
				this.notify();
				break;
			}

			case "tool_execution_end": {
				if (!agentName) break;
				const s = this.agents.get(agentName);
				if (!s) break;
				s.conversation.push({
					type: "tool_end",
					timestamp: Date.now(),
					toolName: event.toolName || "",
					toolResult: event.result,
					isError: event.isError,
				});
				s.currentTool = undefined;
				s.currentToolArgs = undefined;
				this.trimConversation(s);
				this.notify();
				break;
			}

			case "tool_update": {
				if (!agentName) break;
				const s = this.agents.get(agentName);
				if (!s) break;
				s.conversation.push({
					type: "tool_update",
					timestamp: Date.now(),
					toolName: event.toolName || "",
					text: event.text || "",
				});
				this.trimConversation(s);
				this.notify();
				break;
			}

			case "agent_start":
				// New prompt — don't reset, keep previous agents visible
				break;
		}
	}

	getAgent(name: string): SubAgentState | undefined {
		return this.agents.get(name);
	}

	getAllAgents(): SubAgentState[] {
		return this.chain.map(n => this.agents.get(n)).filter(Boolean) as SubAgentState[];
	}

	getRunningAgents(): SubAgentState[] {
		return this.getAllAgents().filter(a => a.status === "running");
	}

	onChange(fn: () => void): () => void {
		this.listeners.add(fn);
		return () => this.listeners.delete(fn);
	}

	reset(): void {
		this.agents.clear();
		this.chain = [];
		this.notify();
	}

	private notify(): void {
		for (const fn of this.listeners) fn();
	}

	private trimConversation(s: SubAgentState): void {
		if (s.conversation.length > MAX_CONVERSATION) {
			s.conversation = s.conversation.slice(-MAX_CONVERSATION);
		}
	}

	private truncArgs(args: any): string {
		if (!args) return "";
		try {
			const s = JSON.stringify(args);
			return s.length > 60 ? s.slice(0, 57) + "..." : s;
		} catch {
			return String(args).slice(0, 60);
		}
	}
}
