import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { usePuxStore } from "@/lib/pux-store";
import { X } from "lucide-react";
import type { FieldConfig } from "./types";

// ── Field wrapper (label + input) ──

function Field({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
	return (
		<label className="flex flex-col gap-1">
			<span className="text-[10px] uppercase tracking-wider text-muted-foreground">
				{label}
				{required && <span className="text-red-400"> *</span>}
			</span>
			{children}
		</label>
	);
}

// ── Multiselect (Badge pills + toggle) ──

function MultiSelect({
	options,
	selected,
	onChange,
}: {
	options: { value: string; label: string }[];
	selected: string[];
	onChange: (vals: string[]) => void;
}) {
	const toggle = (val: string) => {
		if (selected.includes(val)) {
			onChange(selected.filter((v) => v !== val));
		} else {
			onChange([...selected, val]);
		}
	};

	return (
		<div className="flex flex-wrap gap-1.5">
			{options.map((opt) => {
				const active = selected.includes(opt.value);
				return (
					<button
						key={opt.value}
						type="button"
						onClick={() => toggle(opt.value)}
						className={active ? undefined : undefined}
					>
						<Badge variant={active ? "default" : "outline"} className="cursor-pointer text-[10px]">
							{active && <X className="mr-0.5 size-2.5" />}
							{opt.label}
						</Badge>
					</button>
				);
			})}
		</div>
	);
}

// ── Field renderer ──

export function FieldRenderer<T>({
	field,
	value,
	onChange,
}: {
	field: FieldConfig<T>;
	value: any;
	onChange: (val: any) => void;
}) {
	switch (field.type) {
		case "text":
			return (
				<Field label={field.label} required={field.required}>
					<Input
						value={value ?? ""}
						onChange={(e) => onChange(e.target.value)}
						placeholder={field.placeholder}
						className="h-8 text-xs"
					/>
				</Field>
			);

		case "textarea":
			return (
				<Field label={field.label} required={field.required}>
					<Textarea
						value={value ?? ""}
						onChange={(e) => onChange(e.target.value)}
						placeholder={field.placeholder}
						rows={field.rows ?? 3}
						className="min-h-16 resize-none text-xs"
					/>
				</Field>
			);

		case "select":
			return (
				<Field label={field.label} required={field.required}>
					<Select value={value || "__none__"} onValueChange={(v) => onChange(v === "__none__" ? "" : v)}>
						<SelectTrigger className="h-8 text-xs">
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{field.options.map((opt) => (
								<SelectItem key={opt.value || "__none__"} value={opt.value || "__none__"} className="text-xs">
									{opt.label}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</Field>
			);

		case "multiselect":
			return (
				<Field label={field.label}>
					<MultiSelect
						options={field.options}
						selected={value ?? []}
						onChange={onChange}
					/>
				</Field>
			);

		case "number":
			return (
				<Field label={field.label}>
					<Input
						type="number"
						value={value ?? ""}
						onChange={(e) => onChange(e.target.value ? Number(e.target.value) : undefined)}
						placeholder={field.placeholder}
						min={field.min}
						max={field.max}
						className="h-8 text-xs"
					/>
				</Field>
			);

		case "model": {
			const models = usePuxStore((s) => s.modelList);
			return (
				<Field label={field.label}>
					<Select value={value || "__default__"} onValueChange={(v) => onChange(v === "__default__" ? "" : v)}>
						<SelectTrigger className="h-8 text-xs">
							<SelectValue placeholder="Default" />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="__default__" className="text-xs">Default</SelectItem>
							{models.map((m) => (
								<SelectItem key={m.id} value={m.id} className="text-xs">
									{m.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</Field>
			);
		}

		default:
			return null;
	}
}
