import {
	Users,
	RotateCcw,
} from "lucide-react";
import { ConfigPanel } from "./config-panel/config-panel";
import type { FieldConfig } from "./config-panel/types";

// ── Types ──

interface WorkerFormData {
	name: string;
	persona: string;
	capabilities: string[];
	model: string;
	maxRounds: number | undefined;
	temperature: number | undefined;
	sandbox: string;
}

// ── Constants ──

const emptyForm: WorkerFormData = {
	name: "",
	persona: "",
	capabilities: [],
	model: "",
	maxRounds: undefined,
	temperature: undefined,
	sandbox: "",
};

// ── Field config ──

const workerFields: FieldConfig<WorkerFormData>[] = [
	{ key: "name", type: "text", label: "Name", placeholder: "data-collector", required: true },
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
];

// ── Helpers ──

function workerToForm(w: any): WorkerFormData {
	return {
		name: w.name || "",
		persona: w.persona || "",
		capabilities: w.capabilities || [],
		model: w.model || "",
		maxRounds: w.max_rounds || undefined,
		temperature: w.temperature || undefined,
		sandbox: w.sandbox || "",
	};
}

function buildBody(form: WorkerFormData): Record<string, any> {
	const body: Record<string, any> = {
		name: form.name.trim(),
		persona: form.persona.trim(),
	};
	if (form.capabilities.length > 0) body.capabilities = form.capabilities;
	if (form.model) body.model = form.model;
	if (form.maxRounds) body.maxRounds = form.maxRounds;
	if (form.temperature != null) body.temperature = form.temperature;
	if (form.sandbox) body.sandbox = form.sandbox;
	return body;
}

// ── Panel ──

export function WorkersPanel() {
	return (
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
				description: (w: any) => w.persona,
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
				if (form.persona.trim()) parts.push(`Persona: ${form.persona.trim()}`);
				if (form.capabilities.length > 0) parts.push(`Capabilities: ${form.capabilities.join(", ")}`);
				if (form.model) parts.push(`Model: ${form.model}`);
				if (form.maxRounds) parts.push(`Max rounds: ${form.maxRounds}`);
				if (form.temperature != null) parts.push(`Temperature: ${form.temperature}`);
				return parts.join(" ");
			}}
		/>
	);
}
