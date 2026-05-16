import type { ReactNode } from "react";

// ── Field config (discriminated union) ──

export type FieldConfig<T> =
	| { key: keyof T & string; type: "text"; label: string; placeholder?: string; required?: boolean }
	| { key: keyof T & string; type: "textarea"; label: string; placeholder?: string; rows?: number; required?: boolean }
	| { key: keyof T & string; type: "select"; label: string; options: { value: string; label: string }[]; required?: boolean }
	| { key: keyof T & string; type: "multiselect"; label: string; options: { value: string; label: string }[] }
	| { key: keyof T & string; type: "number"; label: string; placeholder?: string; min?: number; max?: number }
	| { key: keyof T & string; type: "model"; label: string }
	| { key: keyof T & string; type: "toggle"; label: string };

// ── Item display definition ──

export interface ItemDef {
	id: (item: any) => string;
	label: (item: any) => string;
	description?: (item: any) => string;
	badges?: (item: any) => { text: string; variant?: "default" | "secondary" | "destructive" | "outline" }[];
	status?: (item: any) => { icon: ReactNode; color: string; spin?: boolean };
}

// ── Extra action button ──

export interface ActionButton {
	label: string;
	icon: ReactNode;
	url: string;
	method: string;
	disabled?: boolean;
}

// ── ConfigPanel props ──

export interface ConfigPanelProps<T> {
	// API endpoints
	fetchUrl: string;
	createUrl: string;
	updateUrl: (id: string) => string;
	deleteUrl: (id: string) => string;

	// Form schema
	fields: FieldConfig<T>[];
	emptyForm: T;
	formToBody: (form: T) => Record<string, any>;
	responseToForm: (data: any) => T;

	// Display
	itemDef: ItemDef;
	title: string;
	emptyMessage: string;
	emptyIcon: ReactNode;

	// Optional features
	enableToggle?: { url: (id: string) => string; key: string };
	extraActions?: (item: any) => ActionButton[];
	askAITemplate: (form: T, isEdit: boolean, editItem?: any) => string;
	visible?: (key: string, form: T) => boolean;
}
