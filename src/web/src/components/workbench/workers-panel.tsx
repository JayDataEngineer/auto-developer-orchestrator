import { useState } from "react";
import {
	Users,
	RotateCcw,
	Crown,
	Pencil,
} from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfigPanel } from "./config-panel/config-panel";
import { PromptDetail } from "./prompt-panel";
import type { FieldConfig } from "./config-panel/types";

// ── Types ──

interface WorkerFormData {
	name: string;
	hint: string;
	persona: string;
	capabilities: string[];
	model: string;
	maxRounds: number | undefined;
	temperature: number | undefined;
	sandbox: string;
	delegatesTo: string[];
}

// ── Constants ──

const emptyForm: WorkerFormData = {
	name: "",
	hint: "",
	persona: "",
	capabilities: [],
	model: "",
	maxRounds: undefined,
	temperature: undefined,
	sandbox: "",
	delegatesTo: [],
};

// ── Field config ──

const workerFields: FieldConfig<WorkerFormData>[] = [
	{ key: "name", type: "text", label: "Name", placeholder: "data-collector", required: true },
	{ key: "hint", type: "text", label: "Hint", placeholder: "One-line CTO-facing description" },
	{ key: "persona", type: "textarea", label: "Persona", placeholder: "You are a data collection specialist...", rows: 3, required: true },
	{
		key: "capabilities", type: "multiselect", label: "Capabilities",
		options: [
			{ value: "browser", label: "Browser" },
			{ value: "code", label: "Code" },
			{ value: "desktop", label: "Desktop" },
			{ value: "research", label: "Research" },
			{ value: "shell", label: "Shell" },
			{ value: "vision", label: "Vision" },
		],
	},
	{ key: "model", type: "model", label: "Model" },
	{ key: "maxRounds", type: "number", label: "Max Rounds", min: 1, max: 100 },
	{ key: "temperature", type: "number", label: "Temperature", min: 0, max: 2 },
	{
		key: "sandbox", type: "select", label: "Sandbox",
		options: [
			{ value: "", label: "Default" },
			{ value: "isolated", label: "Isolated" },
			{ value: "bridged", label: "Bridged" },
			{ value: "native", label: "Native" },
		],
	},
	{ key: "delegatesTo", type: "workers", label: "Can Delegate To" },
];

// ── Helpers ──

function workerToForm(w: any): WorkerFormData {
	return {
		name: w.name || "",
		hint: w.hint || "",
		persona: w.persona || "",
		capabilities: w.capabilities || [],
		model: w.model || "",
		maxRounds: w.max_rounds || undefined,
		temperature: w.temperature || undefined,
		sandbox: w.sandbox || "",
		delegatesTo: w.delegates_to || [],
	};
}

function buildBody(form: WorkerFormData): Record<string, any> {
	const body: Record<string, any> = {
		name: form.name.trim(),
		persona: form.persona.trim(),
	};
	if (form.hint.trim()) body.hint = form.hint.trim();
	if (form.capabilities.length > 0) body.capabilities = form.capabilities;
	if (form.model) body.model = form.model;
	if (form.maxRounds) body.maxRounds = form.maxRounds;
	if (form.temperature != null) body.temperature = form.temperature;
	if (form.sandbox) body.sandbox = form.sandbox;
	if (form.delegatesTo.length > 0) body.delegatesTo = form.delegatesTo;
	return body;
}

// ── CTO Card ──

function CTOCard({ onClick }: { onClick: () => void }) {
	return (
		<Card className="px-3 py-2 cursor-pointer hover:bg-accent/30 transition-colors" onClick={onClick}>
			<div className="flex items-center gap-2">
				<div className="flex size-6 items-center justify-center rounded bg-amber-500/20">
					<Crown className="size-3.5 text-amber-500" />
				</div>
				<div className="min-w-0 flex-1">
					<div className="flex items-center gap-2">
						<span className="truncate text-sm font-medium">CTO (Pux)</span>
						<Badge variant="outline" className="text-[9px]">orchestrator</Badge>
					</div>
					<p className="mt-0.5 truncate text-[11px] text-muted-foreground">
						Edit system prompt sections
					</p>
				</div>
				<Button variant="ghost" size="icon" className="size-7 shrink-0" title="Edit prompt">
					<Pencil size={14} />
				</Button>
			</div>
		</Card>
	);
}

// ── Panel ──

export function WorkersPanel() {
	const [view, setView] = useState<"list" | "cto">("list");

	if (view === "cto") {
		return <PromptDetail onBack={() => setView("list")} />;
	}

	return (
		<div className="flex h-full flex-col">
			{/* CTO card at the top */}
			<div className="p-2 pb-0">
				<CTOCard onClick={() => setView("cto")} />
			</div>

			{/* Workers list below */}
			<div className="flex-1 overflow-hidden">
				<ConfigPanel<WorkerFormData>
					fetchUrl="/api/workers/"
					createUrl="/api/workers/"
					updateUrl={(name) => `/api/workers/${name}`}
					deleteUrl={(name) => `/api/workers/${name}`}
					fields={workerFields}
					emptyForm={emptyForm}
					formToBody={buildBody}
					responseToForm={workerToForm}
					itemDef={{
						id: (w: any) => w.name,
						label: (w: any) => w.name,
						description: (w: any) => w.hint || w.persona,
						badges: (w: any) => [
							...(w.isDefault ? [{ text: "default", variant: "outline" as const }] : []),
							...(w.capabilities || []).map((c: string) => ({
								text: c,
								variant: "secondary" as const,
							})),
						],
					}}
					title="Workers"
					emptyMessage="No workers configured"
					emptyIcon={<Users className="size-8 text-muted-foreground/50" />}
					extraActions={(w: any) =>
						w.isModified
							? [{ label: "Revert to default", icon: <RotateCcw size={14} />, url: `/api/workers/${w.name}/revert`, method: "POST" }]
							: []
					}
					askAITemplate={(form, isEdit, editItem) => {
						const parts: string[] = [isEdit ? `Update the worker "${(editItem as any)?.name}" for me.` : "Create a new worker for me."];
						if (form.name.trim()) parts.push(`Name: ${form.name.trim()}`);
						if (form.hint.trim()) parts.push(`Hint: ${form.hint.trim()}`);
						if (form.persona.trim()) parts.push(`Persona: ${form.persona.trim()}`);
						if (form.capabilities.length > 0) parts.push(`Capabilities: ${form.capabilities.join(", ")}`);
						if (form.model) parts.push(`Model: ${form.model}`);
						if (form.maxRounds) parts.push(`Max rounds: ${form.maxRounds}`);
						if (form.temperature != null) parts.push(`Temperature: ${form.temperature}`);
						if (form.delegatesTo.length > 0) parts.push(`Can delegate to: ${form.delegatesTo.join(", ")}`);
						return parts.join(" ");
					}}
				/>
			</div>
		</div>
	);
}
